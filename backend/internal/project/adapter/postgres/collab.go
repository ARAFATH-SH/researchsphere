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

// Real-time editing (slice 22): what the sync server reads and writes through the API.

var _ domain.CollabStore = (*Store)(nil)

// CollabFile reads a text file for the sync server; shared holds the row until the
// transaction ends.
func (s *Store) CollabFile(ctx context.Context, fileID string, shared bool) (domain.CollabFile, error) {
	fid, ok := parseUUID(fileID)
	if !ok {
		return domain.CollabFile{}, domain.ErrFileNotFound
	}
	var (
		r   sqlcgen.CollabFileRow
		err error
	)
	if shared {
		var row sqlcgen.CollabFileSharedRow
		row, err = s.q(ctx).CollabFileShared(ctx, fid)
		r = sqlcgen.CollabFileRow(row)
	} else {
		r, err = s.q(ctx).CollabFile(ctx, fid)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CollabFile{}, domain.ErrFileNotFound
	}
	if err != nil {
		return domain.CollabFile{}, fmt.Errorf("read collab file: %w", err)
	}
	f := domain.CollabFile{ProjectID: r.ProjectID.String(), Content: r.Content.String, Version: int(r.Version), Epoch: uuidPtr(r.YdocEpoch)}
	if r.YdocState != nil {
		f.State = r.YdocState
	}
	return f, nil
}

// StoreCollab writes the sync server's state when the file is still at st.BaseVersion.
func (s *Store) StoreCollab(ctx context.Context, fileID string, st domain.CollabState) (int, error) {
	fid, ok1 := parseUUID(fileID)
	epoch, ok2 := parseUUID(st.Epoch)
	if !ok1 {
		return 0, domain.ErrFileNotFound
	}
	if !ok2 {
		return 0, &domain.FieldError{Field: "ydocEpoch"}
	}
	var by pgtype.UUID
	if st.UpdatedBy != nil {
		by = viewerUUID(*st.UpdatedBy)
	}
	q := s.q(ctx)
	v, err := q.StoreCollabState(ctx, sqlcgen.StoreCollabStateParams{
		YdocState: st.State, YdocEpoch: epoch, Content: st.Content, UpdatedBy: by,
		FileID: fid, BaseVersion: int32(min(st.BaseVersion, 1<<31-1)), //nolint:gosec // capped above
	})
	if errors.Is(err, pgx.ErrNoRows) {
		if _, err := q.CollabFile(ctx, fid); errors.Is(err, pgx.ErrNoRows) {
			return 0, domain.ErrFileNotFound
		} else if err != nil {
			return 0, fmt.Errorf("read collab file: %w", err)
		}
		return 0, domain.ErrStaleVersion
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" &&
		(pgErr.ConstraintName == "project_files_ydoc_size" || pgErr.ConstraintName == "project_files_text") {
		return 0, domain.ErrFileTooLarge
	}
	if err != nil {
		return 0, fmt.Errorf("store collab state: %w", err)
	}
	return int(v), nil
}

// CollabRoles answers each check in order.
func (s *Store) CollabRoles(ctx context.Context, checks []domain.AccessCheck) ([]domain.Role, error) {
	users := make([]pgtype.UUID, len(checks))
	files := make([]pgtype.UUID, len(checks))
	for i, c := range checks {
		// A malformed ID is NULL, which matches nothing: none.
		users[i], files[i] = viewerUUID(c.UserID), viewerUUID(c.FileID)
	}
	rows, err := s.q(ctx).CollabRoles(ctx, sqlcgen.CollabRolesParams{UserIds: users, FileIds: files})
	if err != nil {
		return nil, fmt.Errorf("read collab roles: %w", err)
	}
	if len(rows) != len(checks) {
		return nil, fmt.Errorf("read collab roles: %d answers for %d checks", len(rows), len(checks))
	}
	roles := make([]domain.Role, len(rows))
	for i, r := range rows {
		if r.Role != "none" {
			roles[i] = domain.Role(r.Role)
		}
	}
	return roles, nil
}

// TextFileIDsUnder returns the IDs of the text files at path and under it.
func (s *Store) TextFileIDsUnder(ctx context.Context, projectID, path string) ([]string, error) {
	pid, ok := parseUUID(projectID)
	if !ok {
		return nil, domain.ErrNotFound
	}
	ids, err := s.q(ctx).TextFileIDsUnder(ctx, sqlcgen.TextFileIDsUnderParams{ProjectID: pid, Path: path})
	if err != nil {
		return nil, fmt.Errorf("read text file ids: %w", err)
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out, nil
}
