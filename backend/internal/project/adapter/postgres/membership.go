package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Project membership (FR-11, FR-10.5): domain.MemberStore on the same Store.

var _ domain.MemberStore = (*Store)(nil)

// isUnique reports whether err is a unique violation of constraint.
func isUnique(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

// Relation reads the user's relation to the project; Exists is false for IDs that are not
// UUIDs.
func (s *Store) Relation(ctx context.Context, projectID, userID string) (domain.Relation, error) {
	pid, ok1 := parseUUID(projectID)
	uid, ok2 := parseUUID(userID)
	if !ok1 || !ok2 {
		return domain.Relation{}, nil
	}
	q := s.q(ctx)
	row, err := q.MembershipRelation(ctx, sqlcgen.MembershipRelationParams{ProjectID: pid, UserID: uid})
	if err != nil {
		return domain.Relation{}, fmt.Errorf("read membership relation: %w", err)
	}
	rel := domain.Relation{Exists: row.UserExists, Available: row.Available, Blocked: row.Blocked, Role: domain.Role(row.Role)}
	p, err := q.PendingJoinRequest(ctx, sqlcgen.PendingJoinRequestParams{ProjectID: pid, UserID: uid})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return domain.Relation{}, fmt.Errorf("read pending join request: %w", err)
	default:
		r := joinRequest(sqlcgen.LockJoinRequestRow(p))
		rel.Pending = &r
	}
	return rel, nil
}

func joinRequest(r sqlcgen.LockJoinRequestRow) domain.JoinRequest {
	return domain.JoinRequest{
		ID: r.ID.String(), ProjectID: r.ProjectID.String(), Kind: domain.RequestKind(r.Kind),
		Status: domain.RequestStatus(r.Status), Message: textPtr(r.Message), Role: domain.Role(r.Role),
		UserID: r.UserID.String(), CreatedAt: r.CreatedAt.Time, DecidedAt: timePtr(r.DecidedAt),
	}
}

// CreateRequest stores a pending request or invitation.
func (s *Store) CreateRequest(ctx context.Context, n domain.NewJoinRequest) (domain.JoinRequest, error) {
	pid, ok1 := parseUUID(n.ProjectID)
	uid, ok2 := parseUUID(n.UserID)
	if !ok1 || !ok2 {
		return domain.JoinRequest{}, domain.ErrNotFound
	}
	p := sqlcgen.InsertJoinRequestParams{ProjectID: pid, UserID: uid, Kind: string(n.Kind), Message: text(n.Message)}
	if n.Role != domain.RoleNone {
		p.Role = pgtype.Text{String: string(n.Role), Valid: true}
	}
	row, err := s.q(ctx).InsertJoinRequest(ctx, p)
	if isUnique(err, "project_join_requests_pending_uq") {
		return domain.JoinRequest{}, domain.ErrRequestPending
	}
	if err != nil {
		return domain.JoinRequest{}, fmt.Errorf("create join request: %w", err)
	}
	return joinRequest(sqlcgen.LockJoinRequestRow(row)), nil
}

// LockRequest reads and locks one row of kind in the project.
func (s *Store) LockRequest(ctx context.Context, projectID, id string, kind domain.RequestKind) (domain.JoinRequest, error) {
	pid, ok1 := parseUUID(projectID)
	rid, ok2 := parseUUID(id)
	if !ok1 || !ok2 {
		return domain.JoinRequest{}, domain.ErrRequestNotFound
	}
	row, err := s.q(ctx).LockJoinRequest(ctx, sqlcgen.LockJoinRequestParams{ID: rid, ProjectID: pid, Kind: string(kind)})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.JoinRequest{}, domain.ErrRequestNotFound
	}
	if err != nil {
		return domain.JoinRequest{}, fmt.Errorf("lock join request: %w", err)
	}
	return joinRequest(row), nil
}

// DecideRequest sets a pending row's status and decision time.
func (s *Store) DecideRequest(ctx context.Context, id string, status domain.RequestStatus) error {
	rid, ok := parseUUID(id)
	if !ok {
		return domain.ErrRequestNotFound
	}
	if err := s.q(ctx).DecideJoinRequest(ctx, sqlcgen.DecideJoinRequestParams{ID: rid, Status: string(status)}); err != nil {
		return fmt.Errorf("decide join request: %w", err)
	}
	return nil
}

// DeleteRequest deletes a row.
func (s *Store) DeleteRequest(ctx context.Context, id string) error {
	rid, ok := parseUUID(id)
	if !ok {
		return domain.ErrRequestNotFound
	}
	if err := s.q(ctx).DeleteJoinRequest(ctx, rid); err != nil {
		return fmt.Errorf("delete join request: %w", err)
	}
	return nil
}

// AddMember adds a member; an existing one is ErrAlreadyMember.
func (s *Store) AddMember(ctx context.Context, projectID, userID string, role domain.Role) (domain.Member, error) {
	pid, ok1 := parseUUID(projectID)
	uid, ok2 := parseUUID(userID)
	if !ok1 || !ok2 {
		return domain.Member{}, domain.ErrNotFound
	}
	q := s.q(ctx)
	_, err := q.InsertMember(ctx, sqlcgen.InsertMemberParams{ProjectID: pid, UserID: uid, Role: string(role)})
	if isUnique(err, "project_members_pkey") {
		return domain.Member{}, domain.ErrAlreadyMember
	}
	if err != nil {
		return domain.Member{}, fmt.Errorf("add member: %w", err)
	}
	return s.Member(ctx, projectID, userID)
}

// Person reads a user as a project shows them.
func (s *Store) Person(ctx context.Context, userID string) (domain.Person, error) {
	uid, ok := parseUUID(userID)
	if !ok {
		return domain.Person{}, domain.ErrUserNotFound
	}
	row, err := s.q(ctx).ProjectPerson(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Person{}, domain.ErrUserNotFound
	}
	if err != nil {
		return domain.Person{}, fmt.Errorf("read person: %w", err)
	}
	return domain.Person{UserID: row.ID.String(), FullName: row.FullName, Headline: textPtr(row.Headline), PhotoFileID: uuidPtr(row.PhotoFileID)}, nil
}

// Member reads one member.
func (s *Store) Member(ctx context.Context, projectID, userID string) (domain.Member, error) {
	pid, ok1 := parseUUID(projectID)
	uid, ok2 := parseUUID(userID)
	if !ok1 || !ok2 {
		return domain.Member{}, domain.ErrMemberNotFound
	}
	row, err := s.q(ctx).ProjectMember(ctx, sqlcgen.ProjectMemberParams{ProjectID: pid, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Member{}, domain.ErrMemberNotFound
	}
	if err != nil {
		return domain.Member{}, fmt.Errorf("read member: %w", err)
	}
	return member(sqlcgen.ProjectMembersRow(row)), nil
}

func member(r sqlcgen.ProjectMembersRow) domain.Member {
	return domain.Member{
		Person: domain.Person{UserID: r.UserID.String(), FullName: r.FullName, Headline: textPtr(r.Headline), PhotoFileID: uuidPtr(r.PhotoFileID)},
		Role:   domain.Role(r.Role), JoinedAt: r.JoinedAt.Time,
	}
}

// SetRole changes a member's role.
func (s *Store) SetRole(ctx context.Context, projectID, userID string, role domain.Role) error {
	pid, ok1 := parseUUID(projectID)
	uid, ok2 := parseUUID(userID)
	if !ok1 || !ok2 {
		return domain.ErrMemberNotFound
	}
	if err := s.q(ctx).SetMemberRole(ctx, sqlcgen.SetMemberRoleParams{ProjectID: pid, UserID: uid, Role: string(role)}); err != nil {
		return fmt.Errorf("set member role: %w", err)
	}
	return nil
}

// RemoveMember deletes a member row.
func (s *Store) RemoveMember(ctx context.Context, projectID, userID string) error {
	pid, ok1 := parseUUID(projectID)
	uid, ok2 := parseUUID(userID)
	if !ok1 || !ok2 {
		return domain.ErrMemberNotFound
	}
	if err := s.q(ctx).DeleteMember(ctx, sqlcgen.DeleteMemberParams{ProjectID: pid, UserID: uid}); err != nil {
		return fmt.Errorf("remove member: %w", err)
	}
	return nil
}

// Members lists the project's members, the owner first.
func (s *Store) Members(ctx context.Context, projectID string, limit, offset int) ([]domain.Member, int, error) {
	pid, ok := parseUUID(projectID)
	if !ok {
		return []domain.Member{}, 0, nil
	}
	lim, off, err := limits(limit, offset)
	if err != nil {
		return nil, 0, err
	}
	q := s.q(ctx)
	total, err := q.CountProjectMembers(ctx, pid)
	if err != nil {
		return nil, 0, fmt.Errorf("count members: %w", err)
	}
	rows, err := q.ProjectMembers(ctx, sqlcgen.ProjectMembersParams{ProjectID: pid, Lim: lim, Off: off})
	if err != nil {
		return nil, 0, fmt.Errorf("list members: %w", err)
	}
	out := make([]domain.Member, len(rows))
	for i, r := range rows {
		out[i] = member(r)
	}
	return out, int(total), nil
}

// ProjectRequests lists one kind of the project's rows, pending first.
func (s *Store) ProjectRequests(ctx context.Context, projectID string, kind domain.RequestKind, limit, offset int) ([]domain.RequestItem, int, error) {
	pid, ok := parseUUID(projectID)
	if !ok {
		return []domain.RequestItem{}, 0, nil
	}
	lim, off, err := limits(limit, offset)
	if err != nil {
		return nil, 0, err
	}
	q := s.q(ctx)
	total, err := q.CountProjectJoinRequests(ctx, sqlcgen.CountProjectJoinRequestsParams{ProjectID: pid, Kind: string(kind)})
	if err != nil {
		return nil, 0, fmt.Errorf("count join requests: %w", err)
	}
	rows, err := q.ProjectJoinRequests(ctx, sqlcgen.ProjectJoinRequestsParams{ProjectID: pid, Kind: string(kind), Lim: lim, Off: off})
	if err != nil {
		return nil, 0, fmt.Errorf("list join requests: %w", err)
	}
	out := make([]domain.RequestItem, len(rows))
	for i, r := range rows {
		out[i] = domain.RequestItem{
			JoinRequest: joinRequest(sqlcgen.LockJoinRequestRow{
				ID: r.ID, ProjectID: r.ProjectID, UserID: r.UserID, Kind: r.Kind, Status: r.Status,
				Message: r.Message, Role: r.Role, CreatedAt: r.CreatedAt, DecidedAt: r.DecidedAt,
			}),
			User: domain.Person{UserID: r.UserID.String(), FullName: r.FullName, Headline: textPtr(r.Headline), PhotoFileID: uuidPtr(r.PhotoFileID)},
		}
	}
	return out, int(total), nil
}

// MyRequests lists the user's join requests, newest first.
func (s *Store) MyRequests(ctx context.Context, userID string, limit, offset int) ([]domain.MyRequestItem, int, error) {
	uid, ok := parseUUID(userID)
	if !ok {
		return []domain.MyRequestItem{}, 0, nil
	}
	lim, off, err := limits(limit, offset)
	if err != nil {
		return nil, 0, err
	}
	q := s.q(ctx)
	total, err := q.CountMyJoinRequests(ctx, uid)
	if err != nil {
		return nil, 0, fmt.Errorf("count my join requests: %w", err)
	}
	rows, err := q.MyJoinRequests(ctx, sqlcgen.MyJoinRequestsParams{UserID: uid, Lim: lim, Off: off})
	if err != nil {
		return nil, 0, fmt.Errorf("list my join requests: %w", err)
	}
	out := make([]domain.MyRequestItem, len(rows))
	for i, r := range rows {
		out[i] = myItem(sqlcgen.MyInvitationsRow(r))
	}
	return out, int(total), nil
}

// MyInvitations lists the user's pending invitations whose owner is reachable.
func (s *Store) MyInvitations(ctx context.Context, userID string, limit, offset int) ([]domain.MyRequestItem, int, error) {
	uid, ok := parseUUID(userID)
	if !ok {
		return []domain.MyRequestItem{}, 0, nil
	}
	lim, off, err := limits(limit, offset)
	if err != nil {
		return nil, 0, err
	}
	q := s.q(ctx)
	total, err := q.CountMyInvitations(ctx, uid)
	if err != nil {
		return nil, 0, fmt.Errorf("count my invitations: %w", err)
	}
	rows, err := q.MyInvitations(ctx, sqlcgen.MyInvitationsParams{UserID: uid, Lim: lim, Off: off})
	if err != nil {
		return nil, 0, fmt.Errorf("list my invitations: %w", err)
	}
	out := make([]domain.MyRequestItem, len(rows))
	for i, r := range rows {
		out[i] = myItem(r)
	}
	return out, int(total), nil
}

func myItem(r sqlcgen.MyInvitationsRow) domain.MyRequestItem {
	return domain.MyRequestItem{
		JoinRequest: joinRequest(sqlcgen.LockJoinRequestRow{
			ID: r.ID, ProjectID: r.ProjectID, UserID: r.UserID, Kind: r.Kind, Status: r.Status,
			Message: r.Message, Role: r.Role, CreatedAt: r.CreatedAt, DecidedAt: r.DecidedAt,
		}),
		Project: domain.MembershipProject{
			ID: r.ProjectID.String(), Title: r.Title, Status: domain.Status(r.ProjectStatus),
			Owner: domain.Person{UserID: r.OwnerID.String(), FullName: r.OwnerName, Headline: textPtr(r.OwnerHeadline), PhotoFileID: uuidPtr(r.OwnerPhoto)},
		},
	}
}

// WithdrawBetween deletes the pending rows between a and b.
func (s *Store) WithdrawBetween(ctx context.Context, a, b string) ([]domain.Withdrawn, error) {
	au, ok1 := parseUUID(a)
	bu, ok2 := parseUUID(b)
	if !ok1 || !ok2 {
		return nil, nil
	}
	rows, err := s.q(ctx).WithdrawJoinRequestsBetween(ctx, sqlcgen.WithdrawJoinRequestsBetweenParams{A: au, B: bu})
	if err != nil {
		return nil, fmt.Errorf("withdraw join requests: %w", err)
	}
	out := make([]domain.Withdrawn, len(rows))
	for i, r := range rows {
		out[i] = domain.Withdrawn{ProjectID: r.ProjectID.String(), UserID: r.UserID.String()}
	}
	return out, nil
}

// VisibleTitles returns the titles of the projects the viewer may see; IDs that are not
// UUIDs are skipped.
func (s *Store) VisibleTitles(ctx context.Context, viewerID string, ids []string) (map[string]string, error) {
	uuids := make([]pgtype.UUID, 0, len(ids))
	for _, id := range ids {
		if u, ok := parseUUID(id); ok {
			uuids = append(uuids, u)
		}
	}
	out := make(map[string]string, len(uuids))
	if len(uuids) == 0 {
		return out, nil
	}
	rows, err := s.q(ctx).VisibleProjectTitles(ctx, sqlcgen.VisibleProjectTitlesParams{Ids: uuids, ViewerID: viewerUUID(viewerID)})
	if err != nil {
		return nil, fmt.Errorf("read project titles: %w", err)
	}
	for _, r := range rows {
		out[r.ID.String()] = r.Title
	}
	return out, nil
}
