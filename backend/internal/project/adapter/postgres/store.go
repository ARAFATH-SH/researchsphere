// Package postgres implements the project domain's Store with the queries that sqlc
// generates from queries.sql, and authz.ProjectRoleLookup (SRS 2.3).
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/authz"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// wordSimilarityThreshold is the pg_trgm word similarity a title needs to match the search
// text when no title contains it, the same as researcher search's (profile/adapter/postgres).
const wordSimilarityThreshold = 0.4

// Match ranks of the search queries (queries.sql), best first.
const (
	rankSkill       = 4
	rankDescription = 5
)

// Store keeps projects in PostgreSQL. Every method but Search runs on the transaction the
// context carries, if any.
type Store struct {
	pool     *pgxpool.Pool
	searchTx func(ctx context.Context, fn func(pgx.Tx) error) error
}

var (
	_ domain.Store            = (*Store)(nil)
	_ authz.ProjectRoleLookup = (*Store)(nil)
)

// NewStore returns a Store that uses pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, searchTx: func(ctx context.Context, fn func(pgx.Tx) error) error {
		return pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, fn)
	}}
}

func (s *Store) q(ctx context.Context) *sqlcgen.Queries {
	return sqlcgen.New(postgres.Conn(ctx, s.pool))
}

// Create stores the project and its owner's member row. Call it inside a transaction: the
// owner check runs at commit.
func (s *Store) Create(ctx context.Context, p domain.NewProject) (string, error) {
	owner, ok := parseUUID(p.OwnerID)
	if !ok {
		return "", fmt.Errorf("create project: owner %q is not a UUID", p.OwnerID)
	}
	q := s.q(ctx)
	id, err := q.InsertProject(ctx, sqlcgen.InsertProjectParams{Title: p.Title, Description: p.Description, Abstract: text(p.Abstract)})
	if err != nil {
		return "", fmt.Errorf("create project: %w", err)
	}
	if err := q.InsertOwner(ctx, sqlcgen.InsertOwnerParams{ProjectID: id, UserID: owner}); err != nil {
		return "", fmt.Errorf("create project owner: %w", err)
	}
	return id.String(), nil
}

// SetSkills makes names the project's skills: tags that do not exist are created, links
// not named are removed, the missing ones added. The skill limit is checked at commit.
func (s *Store) SetSkills(ctx context.Context, projectID string, names []string) error {
	id, ok := parseUUID(projectID)
	if !ok {
		return domain.ErrNotFound
	}
	if names == nil {
		names = []string{}
	}
	q := s.q(ctx)
	if len(names) > 0 {
		if err := q.InsertSkillNames(ctx, names); err != nil {
			return fmt.Errorf("create skills: %w", err)
		}
	}
	if err := q.RemoveSkillsExcept(ctx, sqlcgen.RemoveSkillsExceptParams{ProjectID: id, Names: names}); err != nil {
		return fmt.Errorf("remove project skills: %w", err)
	}
	if len(names) > 0 {
		if err := q.AddSkills(ctx, sqlcgen.AddSkillsParams{ProjectID: id, Names: names}); err != nil {
			return fmt.Errorf("add project skills: %w", err)
		}
	}
	return nil
}

// Get reads the project as viewerID sees it.
func (s *Store) Get(ctx context.Context, projectID, viewerID string) (domain.Project, error) {
	id, ok := parseUUID(projectID)
	if !ok {
		return domain.Project{}, domain.ErrNotFound
	}
	q := s.q(ctx)
	row, err := q.GetProject(ctx, sqlcgen.GetProjectParams{ID: id, ViewerID: viewerUUID(viewerID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Project{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Project{}, fmt.Errorf("read project: %w", err)
	}
	skills, err := s.skills(ctx, q, id)
	if err != nil {
		return domain.Project{}, err
	}
	owner := row.OwnerID.String()
	return domain.Project{
		ID: row.ID.String(), Title: row.Title, Description: row.Description, Abstract: textPtr(row.Abstract),
		Status: domain.Status(row.Status), Skills: skills,
		Owner:  domain.Person{UserID: owner, FullName: row.OwnerName, Headline: textPtr(row.OwnerHeadline), PhotoFileID: uuidPtr(row.OwnerPhoto)},
		MyRole: domain.Role(row.MyRole), MemberCount: int(row.MemberCount),
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
		Access: domain.Access{
			Title: row.Title, Status: domain.Status(row.Status), OwnerID: owner,
			OwnerAvailable: row.OwnerAvailable, BlockedByOwner: row.BlockedByOwner, Role: domain.Role(row.MyRole),
		},
	}, nil
}

func (s *Store) skills(ctx context.Context, q *sqlcgen.Queries, id pgtype.UUID) ([]domain.Skill, error) {
	rows, err := q.ProjectSkills(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read project skills: %w", err)
	}
	out := make([]domain.Skill, len(rows))
	for i, r := range rows {
		out[i] = domain.Skill{ID: r.ID.String(), Name: r.Name}
	}
	return out, nil
}

// Lock reads the access fields and locks the project row.
func (s *Store) Lock(ctx context.Context, projectID, viewerID string) (domain.Access, error) {
	id, ok := parseUUID(projectID)
	if !ok {
		return domain.Access{}, domain.ErrNotFound
	}
	row, err := s.q(ctx).LockProject(ctx, sqlcgen.LockProjectParams{ID: id, ViewerID: viewerUUID(viewerID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Access{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Access{}, fmt.Errorf("lock project: %w", err)
	}
	return domain.Access{
		Title: row.Title, Status: domain.Status(row.Status), OwnerID: row.OwnerID.String(),
		OwnerAvailable: row.OwnerAvailable, BlockedByOwner: row.BlockedByOwner, Role: domain.Role(row.MyRole),
	}, nil
}

// Update writes the changed fields; skills are set with SetSkills.
func (s *Store) Update(ctx context.Context, projectID string, c domain.Changes) error {
	id, ok := parseUUID(projectID)
	if !ok {
		return domain.ErrNotFound
	}
	p := sqlcgen.UpdateProjectParams{ID: id, Title: text(c.Title), Description: text(c.Description), SetAbstract: c.SetAbstract, Abstract: text(c.Abstract)}
	if c.Status != nil {
		p.Status = pgtype.Text{String: string(*c.Status), Valid: true}
	}
	if err := s.q(ctx).UpdateProject(ctx, p); err != nil {
		return fmt.Errorf("update project: %w", err)
	}
	return nil
}

// Delete deletes the project; the schema's cascades and triggers remove the rest.
func (s *Store) Delete(ctx context.Context, projectID string) error {
	id, ok := parseUUID(projectID)
	if !ok {
		return domain.ErrNotFound
	}
	if err := s.q(ctx).DeleteProject(ctx, id); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

// Role is the user's role in the project, RoleNone without a member row or for an ID that
// is not a UUID.
func (s *Store) Role(ctx context.Context, projectID, userID string) (domain.Role, error) {
	pid, ok1 := parseUUID(projectID)
	uid, ok2 := parseUUID(userID)
	if !ok1 || !ok2 {
		return domain.RoleNone, nil
	}
	role, err := s.q(ctx).MemberRole(ctx, sqlcgen.MemberRoleParams{ProjectID: pid, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RoleNone, nil
	}
	if err != nil {
		return domain.RoleNone, fmt.Errorf("read project role: %w", err)
	}
	return domain.Role(role), nil
}

// ProjectRole implements authz.ProjectRoleLookup.
func (s *Store) ProjectRole(ctx context.Context, projectID, userID string) (authz.ProjectRole, error) {
	r, err := s.Role(ctx, projectID, userID)
	if err != nil {
		return authz.ProjectNonMember, err
	}
	if r == domain.RoleNone {
		return authz.ProjectNonMember, nil
	}
	return authz.ProjectRole(r), nil
}

// ListMine lists the user's projects, most recently changed first.
func (s *Store) ListMine(ctx context.Context, userID string, limit, offset int) ([]domain.Summary, int, error) {
	uid, ok := parseUUID(userID)
	if !ok {
		return []domain.Summary{}, 0, nil
	}
	lim, off, err := limits(limit, offset)
	if err != nil {
		return nil, 0, err
	}
	q := s.q(ctx)
	total, err := q.CountMyProjects(ctx, uid)
	if err != nil {
		return nil, 0, fmt.Errorf("count projects: %w", err)
	}
	rows, err := q.MyProjects(ctx, sqlcgen.MyProjectsParams{UserID: uid, Lim: lim, Off: off})
	if err != nil {
		return nil, 0, fmt.Errorf("list projects: %w", err)
	}
	out := make([]domain.Summary, len(rows))
	ids := make([]pgtype.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
		out[i] = domain.Summary{
			ID: r.ID.String(), Title: r.Title, Description: r.Description, Status: domain.Status(r.Status),
			Owner:  domain.Person{UserID: r.OwnerID.String(), FullName: r.OwnerName, Headline: textPtr(r.OwnerHeadline), PhotoFileID: uuidPtr(r.OwnerPhoto)},
			MyRole: domain.Role(r.MyRole), CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time,
		}
	}
	if err := addSkillNames(ctx, q, ids, out); err != nil {
		return nil, 0, err
	}
	return out, int(total), nil
}

// addSkillNames fills each summary's first domain.SummarySkills skill names.
func addSkillNames(ctx context.Context, q *sqlcgen.Queries, ids []pgtype.UUID, items []domain.Summary) error {
	for i := range items {
		items[i].Skills = []string{}
	}
	if len(ids) == 0 {
		return nil
	}
	at := make(map[string]int, len(items))
	for i, it := range items {
		at[it.ID] = i
	}
	rows, err := q.SkillsOfProjects(ctx, ids)
	if err != nil {
		return fmt.Errorf("read project skills: %w", err)
	}
	for _, r := range rows {
		i := at[r.ProjectID.String()]
		if len(items[i].Skills) < domain.SummarySkills {
			items[i].Skills = append(items[i].Skills, r.Name)
		}
	}
	return nil
}

// Search runs the project search in one read-only snapshot, so the page and total agree
// (FR-10.3).
func (s *Store) Search(ctx context.Context, sq domain.SearchQuery) (domain.SearchResult, error) {
	var res domain.SearchResult
	err := s.searchTx(ctx, func(t pgx.Tx) error {
		r, err := search(ctx, sqlcgen.New(t), sq)
		res = r
		return err
	})
	if err != nil {
		return domain.SearchResult{}, fmt.Errorf("search projects: %w", err)
	}
	return res, nil
}

func search(ctx context.Context, q *sqlcgen.Queries, sq domain.SearchQuery) (domain.SearchResult, error) {
	lim, off, err := limits(sq.Limit, sq.Offset)
	if err != nil {
		return domain.SearchResult{}, err
	}
	if err := q.SetWordSimilarityThreshold(ctx, strconv.FormatFloat(wordSimilarityThreshold, 'f', -1, 64)); err != nil {
		return domain.SearchResult{}, fmt.Errorf("set word similarity threshold: %w", err)
	}
	pattern := containsPattern(sq.Q)
	viewer := viewerUUID(sq.ViewerID)
	literal, err := q.AnyVisibleTitleContains(ctx, sqlcgen.AnyVisibleTitleContainsParams{Pattern: pattern, ViewerID: viewer})
	if err != nil {
		return domain.SearchResult{}, fmt.Errorf("check title matches: %w", err)
	}
	sum, err := q.SearchProjectsSummary(ctx, sqlcgen.SearchProjectsSummaryParams{ViewerID: viewer, Q: sq.Q, Fuzzy: !literal, Pattern: pattern})
	if err != nil {
		return domain.SearchResult{}, fmt.Errorf("count results: %w", err)
	}
	res := domain.SearchResult{Total: int(sum.Total), Items: []domain.Summary{}}
	if sum.CorrectedQuery != "" {
		res.Fuzzy = true
		res.CorrectedQuery = &sum.CorrectedQuery
	}
	rows, err := q.SearchProjectsPage(ctx, sqlcgen.SearchProjectsPageParams{
		ViewerID: viewer, Q: sq.Q, Fuzzy: !literal, Pattern: pattern, Lim: lim, Off: off,
	})
	if err != nil {
		return domain.SearchResult{}, fmt.Errorf("read results: %w", err)
	}
	res.Items = make([]domain.Summary, len(rows))
	ids := make([]pgtype.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
		res.Items[i] = domain.Summary{
			ID: r.ID.String(), Title: r.Title, Description: r.Description, Status: domain.Status(r.Status),
			Owner:  domain.Person{UserID: r.OwnerID.String(), FullName: r.OwnerName, Headline: textPtr(r.OwnerHeadline), PhotoFileID: uuidPtr(r.OwnerPhoto)},
			MyRole: domain.Role(r.MyRole), MatchedOn: matchedOn(r.Rank), CreatedAt: r.CreatedAt.Time, UpdatedAt: r.UpdatedAt.Time,
		}
	}
	if err := addSkillNames(ctx, q, ids, res.Items); err != nil {
		return domain.SearchResult{}, err
	}
	return res, nil
}

func matchedOn(rank int32) string {
	switch rank {
	case rankSkill:
		return "skill"
	case rankDescription:
		return "description"
	default: // ranks 0 to 3: the title
		return "title"
	}
}

// likeEscaper escapes the characters ILIKE treats specially (backslash is its default
// escape character), so the search text matches only itself.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// containsPattern is the ILIKE pattern of text anywhere in a value. It is passed as a
// query parameter, never joined into SQL.
func containsPattern(text string) string {
	return "%" + likeEscaper.Replace(text) + "%"
}

// CreateReport stores a report; a second open one by the same user is ErrAlreadyReported.
func (s *Store) CreateReport(ctx context.Context, r domain.NewReport) (domain.ReportReceipt, error) {
	pid, ok1 := parseUUID(r.ProjectID)
	uid, ok2 := parseUUID(r.ReporterID)
	if !ok1 || !ok2 {
		return domain.ReportReceipt{}, domain.ErrNotFound
	}
	row, err := s.q(ctx).InsertProjectReport(ctx, sqlcgen.InsertProjectReportParams{
		ProjectID: pid, ReporterID: uid, Reason: r.Reason, Details: text(r.Details),
	})
	if pgErr := (*pgconn.PgError)(nil); errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "project_reports_open_uq" {
		return domain.ReportReceipt{}, domain.ErrAlreadyReported
	}
	if err != nil {
		return domain.ReportReceipt{}, fmt.Errorf("create project report: %w", err)
	}
	return domain.ReportReceipt{ID: row.ID.String(), CreatedAt: row.CreatedAt.Time}, nil
}

// reportRow is the shape ListProjectReports and GetProjectReport share.
type reportRow = sqlcgen.ListProjectReportsRow

func reportOf(r reportRow) domain.Report {
	return domain.Report{
		ID: r.ID.String(), ProjectID: r.ProjectID.String(), ProjectTitle: r.ProjectTitle, Reason: r.Reason, Details: textPtr(r.Details),
		Reporter: domain.ReportPerson{
			Person: domain.Person{UserID: r.ReporterID.String(), FullName: r.ReporterName, Headline: textPtr(r.ReporterHeadline), PhotoFileID: uuidPtr(r.ReporterPhoto)},
			Role:   r.ReporterRole,
		},
		Owner: domain.ReportPerson{
			Person: domain.Person{UserID: r.OwnerID.String(), FullName: r.OwnerName, Headline: textPtr(r.OwnerHeadline), PhotoFileID: uuidPtr(r.OwnerPhoto)},
			Role:   r.OwnerRole,
		},
		CreatedAt: r.CreatedAt.Time, Outcome: textPtr(r.Outcome), Note: textPtr(r.ResolutionNote), ResolvedAt: timePtr(r.ResolvedAt),
	}
}

// ListReports lists open or resolved reports, newest first.
func (s *Store) ListReports(ctx context.Context, open bool, limit, offset int) ([]domain.Report, int, error) {
	lim, off, err := limits(limit, offset)
	if err != nil {
		return nil, 0, err
	}
	q := s.q(ctx)
	total, err := q.CountProjectReports(ctx, open)
	if err != nil {
		return nil, 0, fmt.Errorf("count project reports: %w", err)
	}
	rows, err := q.ListProjectReports(ctx, sqlcgen.ListProjectReportsParams{Open: open, Lim: lim, Off: off})
	if err != nil {
		return nil, 0, fmt.Errorf("list project reports: %w", err)
	}
	out := make([]domain.Report, len(rows))
	for i, r := range rows {
		out[i] = reportOf(r)
	}
	return out, int(total), nil
}

// GetReport reads a report and the reported project.
func (s *Store) GetReport(ctx context.Context, reportID string) (domain.Report, domain.ReportedProject, error) {
	id, ok := parseUUID(reportID)
	if !ok {
		return domain.Report{}, domain.ReportedProject{}, domain.ErrReportNotFound
	}
	q := s.q(ctx)
	rep, err := s.report(ctx, q, id)
	if err != nil {
		return domain.Report{}, domain.ReportedProject{}, err
	}
	pid, _ := parseUUID(rep.ProjectID)
	p, err := q.ReportedProject(ctx, pid)
	if err != nil {
		return domain.Report{}, domain.ReportedProject{}, fmt.Errorf("read reported project: %w", err)
	}
	skills, err := s.skills(ctx, q, pid)
	if err != nil {
		return domain.Report{}, domain.ReportedProject{}, err
	}
	return rep, domain.ReportedProject{
		ID: p.ID.String(), Title: p.Title, Description: p.Description, Abstract: textPtr(p.Abstract), Status: domain.Status(p.Status),
		Skills: skills, MemberCount: int(p.MemberCount), CreatedAt: p.CreatedAt.Time, UpdatedAt: p.UpdatedAt.Time,
	}, nil
}

func (s *Store) report(ctx context.Context, q *sqlcgen.Queries, id pgtype.UUID) (domain.Report, error) {
	row, err := q.GetProjectReport(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Report{}, domain.ErrReportNotFound
	}
	if err != nil {
		return domain.Report{}, fmt.Errorf("read project report: %w", err)
	}
	return reportOf(reportRow(row)), nil
}

// ResolveReport closes an open report.
func (s *Store) ResolveReport(ctx context.Context, reportID, adminID, outcome string, note *string) (domain.Report, error) {
	id, ok := parseUUID(reportID)
	if !ok {
		return domain.Report{}, domain.ErrReportNotFound
	}
	q := s.q(ctx)
	n, err := q.ResolveProjectReport(ctx, sqlcgen.ResolveProjectReportParams{
		ID: id, AdminID: viewerUUID(adminID), Outcome: pgtype.Text{String: outcome, Valid: true}, Note: text(note),
	})
	if err != nil {
		return domain.Report{}, fmt.Errorf("resolve project report: %w", err)
	}
	if n == 0 {
		exists, err := q.ProjectReportExists(ctx, id)
		if err != nil {
			return domain.Report{}, fmt.Errorf("read project report: %w", err)
		}
		if !exists {
			return domain.Report{}, domain.ErrReportNotFound
		}
		return domain.Report{}, domain.ErrAlreadyResolved
	}
	return s.report(ctx, q, id)
}

func parseUUID(s string) (pgtype.UUID, bool) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, false
	}
	return u, true
}

// viewerUUID is the viewer's ID, NULL when it is not a UUID: NULL matches no member row and
// no block.
func viewerUUID(s string) pgtype.UUID {
	u, _ := parseUUID(s)
	return u
}

func text(s *string) pgtype.Text {
	if s == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *s, Valid: true}
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

func uuidPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := u.String()
	return &s
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

func limits(limit, offset int) (int32, int32, error) {
	if limit < 0 || limit > 1<<31-1 || offset < 0 || offset > 1<<31-1 {
		return 0, 0, fmt.Errorf("limit %d or offset %d out of range", limit, offset)
	}
	return int32(limit), int32(offset), nil
}
