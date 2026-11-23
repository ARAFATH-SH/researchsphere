package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

var _ domain.FileStore = (*Store)(nil)

// fileError maps the database's file rules (migration 000035) to the domain's errors.
func fileError(err error, what string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch {
		case pgErr.Code == "23505" && pgErr.ConstraintName == "project_files_path_uq":
			return domain.ErrPathTaken
		case pgErr.Code == "23514" && pgErr.ConstraintName == "project_files_max":
			return domain.ErrFileLimit
		case pgErr.Code == "23514" && pgErr.ConstraintName == "project_files_parent":
			return domain.ErrParentNotFolder
		}
	}
	return fmt.Errorf("%s: %w", what, err)
}

// Tree lists the project's files.
func (s *Store) Tree(ctx context.Context, projectID string) (domain.FileTree, error) {
	id, ok := parseUUID(projectID)
	if !ok {
		return domain.FileTree{}, domain.ErrNotFound
	}
	q := s.q(ctx)
	main, err := q.MainFilePath(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FileTree{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.FileTree{}, fmt.Errorf("read main file path: %w", err)
	}
	rows, err := q.ProjectFiles(ctx, id)
	if err != nil {
		return domain.FileTree{}, fmt.Errorf("list project files: %w", err)
	}
	files := make([]domain.File, len(rows))
	for i, r := range rows {
		files[i] = domain.File{
			ID: r.ID.String(), ProjectID: projectID, Path: r.Path, Kind: domain.FileKind(r.Kind),
			Size: r.SizeBytes, Version: int(r.Version), ContentType: textPtr(r.ContentType),
			ScanStatus: scanStatus(r.ScanStatus), UpdatedAt: r.UpdatedAt.Time,
			UpdatedBy: updater(r.UpdatedBy, r.UpdaterName, r.UpdaterHeadline, r.UpdaterPhoto),
		}
	}
	return domain.FileTree{MainFilePath: main, Files: files}, nil
}

// File reads one file of the project.
func (s *Store) File(ctx context.Context, projectID, fileID string) (domain.StoredFile, error) {
	pid, ok1 := parseUUID(projectID)
	fid, ok2 := parseUUID(fileID)
	if !ok1 || !ok2 {
		return domain.StoredFile{}, domain.ErrFileNotFound
	}
	r, err := s.q(ctx).ProjectFile(ctx, sqlcgen.ProjectFileParams{ProjectID: pid, ID: fid})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StoredFile{}, domain.ErrFileNotFound
	}
	if err != nil {
		return domain.StoredFile{}, fmt.Errorf("read project file: %w", err)
	}
	return domain.StoredFile{
		File: domain.File{
			ID: r.ID.String(), ProjectID: projectID, Path: r.Path, Kind: domain.FileKind(r.Kind),
			Size: r.SizeBytes, Version: int(r.Version), ContentType: textPtr(r.ContentType),
			ScanStatus: scanStatus(r.ScanStatus), UpdatedAt: r.UpdatedAt.Time,
			UpdatedBy: updater(r.UpdatedBy, r.UpdaterName, r.UpdaterHeadline, r.UpdaterPhoto),
		},
		Content: textPtr(r.Content), ObjectKey: textPtr(r.ObjectKey),
	}, nil
}

// LockFile reads one file of the project and locks its row.
func (s *Store) LockFile(ctx context.Context, projectID, fileID string) (domain.StoredFile, error) {
	pid, ok1 := parseUUID(projectID)
	fid, ok2 := parseUUID(fileID)
	if !ok1 || !ok2 {
		return domain.StoredFile{}, domain.ErrFileNotFound
	}
	r, err := s.q(ctx).LockProjectFile(ctx, sqlcgen.LockProjectFileParams{ProjectID: pid, ID: fid})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StoredFile{}, domain.ErrFileNotFound
	}
	if err != nil {
		return domain.StoredFile{}, fmt.Errorf("lock project file: %w", err)
	}
	return domain.StoredFile{
		File: domain.File{
			ID: r.ID.String(), ProjectID: projectID, Path: r.Path, Kind: domain.FileKind(r.Kind),
			Size: r.SizeBytes, Version: int(r.Version), ContentType: textPtr(r.ContentType), UpdatedAt: r.UpdatedAt.Time,
		},
		Content: textPtr(r.Content),
	}, nil
}

// FileAt finds the entry at path, ignoring letter case.
func (s *Store) FileAt(ctx context.Context, projectID, path string) (domain.File, bool, error) {
	pid, ok := parseUUID(projectID)
	if !ok {
		return domain.File{}, false, domain.ErrNotFound
	}
	r, err := s.q(ctx).ProjectFileAt(ctx, sqlcgen.ProjectFileAtParams{ProjectID: pid, Path: path})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.File{}, false, nil
	}
	if err != nil {
		return domain.File{}, false, fmt.Errorf("find project file: %w", err)
	}
	return domain.File{ID: r.ID.String(), ProjectID: projectID, Kind: domain.FileKind(r.Kind), Path: r.Path}, true, nil
}

// InsertFolder adds a folder.
func (s *Store) InsertFolder(ctx context.Context, projectID, path, by string) (string, error) {
	pid, ok := parseUUID(projectID)
	if !ok {
		return "", domain.ErrNotFound
	}
	id, err := s.q(ctx).InsertProjectFolder(ctx, sqlcgen.InsertProjectFolderParams{ProjectID: pid, Path: path, UpdatedBy: viewerUUID(by)})
	if err != nil {
		return "", fileError(err, "create project folder")
	}
	return id.String(), nil
}

// InsertText adds a text file.
func (s *Store) InsertText(ctx context.Context, projectID, path, content, by string) (string, error) {
	pid, ok := parseUUID(projectID)
	if !ok {
		return "", domain.ErrNotFound
	}
	id, err := s.q(ctx).InsertProjectText(ctx, sqlcgen.InsertProjectTextParams{ProjectID: pid, Path: path, Content: content, UpdatedBy: viewerUUID(by)})
	if err != nil {
		return "", fileError(err, "create project file")
	}
	return id.String(), nil
}

// InsertFigure adds the figure's stored file, owned by the project, and its project file.
func (s *Store) InsertFigure(ctx context.Context, f domain.NewFigure) (string, error) {
	pid, ok1 := parseUUID(f.ProjectID)
	sid, ok2 := parseUUID(f.StoredFileID)
	if !ok1 || !ok2 {
		return "", fmt.Errorf("create figure: project %q or file %q is not a UUID", f.ProjectID, f.StoredFileID)
	}
	q := s.q(ctx)
	p := sqlcgen.InsertFigureObjectParams{
		ID: sid, ProjectID: pid, UploadedBy: viewerUUID(f.UploadedBy), ObjectKey: f.ObjectKey,
		ContentType: f.ContentType, SizeBytes: f.Size,
	}
	if f.Width > 0 && f.Height > 0 {
		p.Width = pgtype.Int4{Int32: int32(f.Width), Valid: true}   //nolint:gosec // at most 2^31-1 (media's figure check)
		p.Height = pgtype.Int4{Int32: int32(f.Height), Valid: true} //nolint:gosec // at most 2^31-1 (media's figure check)
	}
	if err := q.InsertFigureObject(ctx, p); err != nil {
		return "", fmt.Errorf("record figure object: %w", err)
	}
	id, err := q.InsertProjectFigure(ctx, sqlcgen.InsertProjectFigureParams{
		ProjectID: pid, Path: f.Path, StoredFileID: sid, SizeBytes: f.Size, UpdatedBy: viewerUUID(f.UploadedBy),
	})
	if err != nil {
		return "", fileError(err, "create figure")
	}
	return id.String(), nil
}

// SaveContent stores a text file's content and returns its new version.
func (s *Store) SaveContent(ctx context.Context, fileID, content, by string) (int, error) {
	fid, ok := parseUUID(fileID)
	if !ok {
		return 0, domain.ErrFileNotFound
	}
	v, err := s.q(ctx).SaveProjectFileContent(ctx, sqlcgen.SaveProjectFileContentParams{ID: fid, Content: content, UpdatedBy: viewerUUID(by)})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrNotTextFile
	}
	if err != nil {
		return 0, fmt.Errorf("save project file: %w", err)
	}
	return int(v), nil
}

// Move moves the entry at from, and everything under it, to to; the main file follows.
func (s *Store) Move(ctx context.Context, projectID, from, to, by string) error {
	pid, ok := parseUUID(projectID)
	if !ok {
		return domain.ErrNotFound
	}
	q := s.q(ctx)
	n, err := q.MoveProjectFiles(ctx, sqlcgen.MoveProjectFilesParams{ProjectID: pid, OldPath: from, NewPath: to, UpdatedBy: viewerUUID(by)})
	if err != nil {
		return fileError(err, "move project files")
	}
	if n == 0 {
		return domain.ErrFileNotFound
	}
	if err := q.MoveMainFilePath(ctx, sqlcgen.MoveMainFilePathParams{ID: pid, OldPath: from, NewPath: to}); err != nil {
		return fmt.Errorf("move main file path: %w", err)
	}
	return nil
}

// ObjectKeysUnder returns the object keys of the figures at path and under it.
func (s *Store) ObjectKeysUnder(ctx context.Context, projectID, path string) ([]string, error) {
	pid, ok := parseUUID(projectID)
	if !ok {
		return nil, domain.ErrNotFound
	}
	keys, err := s.q(ctx).FigureKeysUnder(ctx, sqlcgen.FigureKeysUnderParams{ProjectID: pid, Path: path})
	if err != nil {
		return nil, fmt.Errorf("read figure keys: %w", err)
	}
	return keys, nil
}

// DeleteTree deletes the entry at path and everything under it.
func (s *Store) DeleteTree(ctx context.Context, projectID, path string) error {
	pid, ok := parseUUID(projectID)
	if !ok {
		return domain.ErrNotFound
	}
	if err := s.q(ctx).DeleteProjectFiles(ctx, sqlcgen.DeleteProjectFilesParams{ProjectID: pid, Path: path}); err != nil {
		return fileError(err, "delete project files")
	}
	return nil
}

// ObjectKeys returns the object keys of all the project's figures.
func (s *Store) ObjectKeys(ctx context.Context, projectID string) ([]string, error) {
	pid, ok := parseUUID(projectID)
	if !ok {
		return nil, domain.ErrNotFound
	}
	keys, err := s.q(ctx).ProjectFigureKeys(ctx, pid)
	if err != nil {
		return nil, fmt.Errorf("read project figure keys: %w", err)
	}
	return keys, nil
}

// FileCount counts the project's entries.
func (s *Store) FileCount(ctx context.Context, projectID string) (int, error) {
	pid, ok := parseUUID(projectID)
	if !ok {
		return 0, domain.ErrNotFound
	}
	n, err := s.q(ctx).CountProjectFiles(ctx, pid)
	if err != nil {
		return 0, fmt.Errorf("count project files: %w", err)
	}
	return int(n), nil
}

// FullName is the user's profile name, "" when there is none.
func (s *Store) FullName(ctx context.Context, userID string) (string, error) {
	uid, ok := parseUUID(userID)
	if !ok {
		return "", nil
	}
	name, err := s.q(ctx).ProfileFullName(ctx, uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read profile name: %w", err)
	}
	return name, nil
}

// ProjectsWithoutFiles returns the IDs of up to limit projects with no file, after after.
func (s *Store) ProjectsWithoutFiles(ctx context.Context, after string, limit int) ([]string, error) {
	ids, err := s.q(ctx).ProjectsWithoutFiles(ctx, sqlcgen.ProjectsWithoutFilesParams{After: viewerUUID(after), MaxRows: int32(min(limit, 1000))}) //nolint:gosec // bounded
	if err != nil {
		return nil, fmt.Errorf("list projects without files: %w", err)
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out, nil
}

// LockShared is the user's role with the project row locked shared.
func (s *Store) LockShared(ctx context.Context, projectID, userID string) (domain.Role, error) {
	pid, ok := parseUUID(projectID)
	if !ok {
		return domain.RoleNone, domain.ErrNotFound
	}
	role, err := s.q(ctx).ShareLockProjectRole(ctx, sqlcgen.ShareLockProjectRoleParams{ID: pid, UserID: viewerUUID(userID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RoleNone, domain.ErrNotFound
	}
	if err != nil {
		return domain.RoleNone, fmt.Errorf("lock project shared: %w", err)
	}
	return domain.Role(role), nil
}

// StorageUsed is the bytes of the project's stored files, except those removed as malware.
func (s *Store) StorageUsed(ctx context.Context, projectID string) (int64, error) {
	pid, ok := parseUUID(projectID)
	if !ok {
		return 0, domain.ErrNotFound
	}
	n, err := s.q(ctx).ProjectStorageUsed(ctx, pid)
	if err != nil {
		return 0, fmt.Errorf("project storage used: %w", err)
	}
	return n, nil
}

// PendingUploadBytes is the bytes of the project's uploads started after since.
func (s *Store) PendingUploadBytes(ctx context.Context, projectID string, since time.Time) (int64, error) {
	pid, ok := parseUUID(projectID)
	if !ok {
		return 0, domain.ErrNotFound
	}
	n, err := s.q(ctx).PendingUploadBytes(ctx, sqlcgen.PendingUploadBytesParams{
		ProjectID: pid, Since: pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("pending upload bytes: %w", err)
	}
	return n, nil
}

// InsertUpload records an upload.
func (s *Store) InsertUpload(ctx context.Context, u domain.Upload) (string, error) {
	pid, ok1 := parseUUID(u.ProjectID)
	uid, ok2 := parseUUID(u.UserID)
	if !ok1 || !ok2 {
		return "", fmt.Errorf("record upload: project %q or user %q is not a UUID", u.ProjectID, u.UserID)
	}
	id, err := s.q(ctx).InsertFigureUpload(ctx, sqlcgen.InsertFigureUploadParams{
		ProjectID: pid, UserID: uid, ObjectKey: u.ObjectKey, Path: u.Path, SizeBytes: u.Size,
		ExpiresAt: pgtype.Timestamptz{Time: u.ExpiresAt, Valid: true},
	})
	if err != nil {
		return "", fmt.Errorf("record upload: %w", err)
	}
	return id.String(), nil
}

// Upload reads the user's upload in the project while it has not expired.
func (s *Store) Upload(ctx context.Context, projectID, uploadID, userID string) (domain.Upload, error) {
	pid, ok1 := parseUUID(projectID)
	id, ok2 := parseUUID(uploadID)
	uid, ok3 := parseUUID(userID)
	if !ok1 || !ok2 || !ok3 {
		return domain.Upload{}, domain.ErrUploadNotFound
	}
	r, err := s.q(ctx).FigureUpload(ctx, sqlcgen.FigureUploadParams{ID: id, ProjectID: pid, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Upload{}, domain.ErrUploadNotFound
	}
	if err != nil {
		return domain.Upload{}, fmt.Errorf("read upload: %w", err)
	}
	return domain.Upload{
		ID: r.ID.String(), ProjectID: projectID, UserID: userID, ObjectKey: r.ObjectKey,
		Path: r.Path, Size: r.SizeBytes, ExpiresAt: r.ExpiresAt.Time,
	}, nil
}

// ClaimUpload deletes the upload if it is still there and not expired.
func (s *Store) ClaimUpload(ctx context.Context, uploadID string) (bool, error) {
	id, ok := parseUUID(uploadID)
	if !ok {
		return false, nil
	}
	n, err := s.q(ctx).ClaimFigureUpload(ctx, id)
	if err != nil {
		return false, fmt.Errorf("claim upload: %w", err)
	}
	return n == 1, nil
}

// DeleteExpiredUploads deletes up to limit expired uploads.
func (s *Store) DeleteExpiredUploads(ctx context.Context, limit int) (int, error) {
	n, err := s.q(ctx).DeleteExpiredFigureUploads(ctx, int32(min(limit, 10000))) //nolint:gosec // bounded above
	if err != nil {
		return 0, fmt.Errorf("delete expired uploads: %w", err)
	}
	return int(n), nil
}

func scanStatus(t pgtype.Text) *domain.ScanStatus {
	if !t.Valid {
		return nil
	}
	st := domain.ScanStatus(t.String)
	return &st
}

func updater(id pgtype.UUID, name string, headline pgtype.Text, photo pgtype.UUID) *domain.Person {
	if !id.Valid {
		return nil
	}
	return &domain.Person{UserID: id.String(), FullName: name, Headline: textPtr(headline), PhotoFileID: uuidPtr(photo)}
}

// SourceFiles lists every entry of the project with its text or object key (slice 23).
func (s *Store) SourceFiles(ctx context.Context, projectID string) ([]domain.StoredFile, error) {
	id, ok := parseUUID(projectID)
	if !ok {
		return nil, domain.ErrNotFound
	}
	rows, err := s.q(ctx).SourceFiles(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list source files: %w", err)
	}
	files := make([]domain.StoredFile, len(rows))
	for i, r := range rows {
		files[i] = domain.StoredFile{
			File: domain.File{
				ID: r.ID.String(), ProjectID: projectID, Path: r.Path, Kind: domain.FileKind(r.Kind),
				Size: r.SizeBytes, Version: int(r.Version), ContentType: textPtr(r.ContentType),
				ScanStatus: scanStatus(r.ScanStatus), UpdatedAt: r.UpdatedAt.Time,
			},
			Content: textPtr(r.Content), ObjectKey: textPtr(r.ObjectKey),
		}
	}
	return files, nil
}

// MainFile is the project's main file path.
func (s *Store) MainFile(ctx context.Context, projectID string) (string, error) {
	id, ok := parseUUID(projectID)
	if !ok {
		return "", domain.ErrNotFound
	}
	main, err := s.q(ctx).MainFilePath(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read main file path: %w", err)
	}
	return main, nil
}

// SetMainFile sets the project's main file path.
func (s *Store) SetMainFile(ctx context.Context, projectID, path string) error {
	id, ok := parseUUID(projectID)
	if !ok {
		return domain.ErrNotFound
	}
	if err := s.q(ctx).SetMainFilePath(ctx, sqlcgen.SetMainFilePathParams{ID: id, Path: path}); err != nil {
		return fmt.Errorf("set main file path: %w", err)
	}
	return nil
}

// ProjectTitle is the project's title.
func (s *Store) ProjectTitle(ctx context.Context, projectID string) (string, error) {
	id, ok := parseUUID(projectID)
	if !ok {
		return "", domain.ErrNotFound
	}
	title, err := s.q(ctx).ProjectTitle(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read project title: %w", err)
	}
	return title, nil
}
