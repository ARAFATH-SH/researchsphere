package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

var _ domain.ProfileImages = (*Store)(nil)

// ReplaceProfileImage returns domain.ErrNoProfile when the owner has no profile row.
func (s *Store) ReplaceProfileImage(ctx context.Context, f domain.NewFile) (string, error) {
	owner, err := parseUUID(f.OwnerID)
	if err != nil {
		return "", domain.ErrNoProfile
	}
	var id pgtype.UUID
	err = pgx.BeginFunc(ctx, s.pool, func(t pgx.Tx) error {
		q := s.q.WithTx(t)
		old, err := lockedImage(ctx, q, owner, f.Purpose)
		if err != nil {
			return err
		}
		id, err = q.InsertProfileImageFile(ctx, sqlcgen.InsertProfileImageFileParams{
			OwnerUserID: owner,
			Purpose:     string(f.Purpose),
			ObjectKey:   f.ObjectKey,
			ContentType: f.ContentType,
			SizeBytes:   f.Size,
			Width:       pgtype.Int4{Int32: int32(f.Width), Valid: true},  //nolint:gosec // at most 1584
			Height:      pgtype.Int4{Int32: int32(f.Height), Valid: true}, //nolint:gosec // at most 400
		})
		if err != nil {
			return fmt.Errorf("insert stored file: %w", err)
		}
		if err := setImage(ctx, q, owner, f.Purpose, id); err != nil {
			return err
		}
		return deleteOld(ctx, q, owner, old)
	})
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// ClearProfileImage does nothing when the user has no profile row or no image set.
func (s *Store) ClearProfileImage(ctx context.Context, userID string, purpose domain.Purpose) error {
	owner, err := parseUUID(userID)
	if err != nil {
		return nil
	}
	return pgx.BeginFunc(ctx, s.pool, func(t pgx.Tx) error {
		q := s.q.WithTx(t)
		old, err := lockedImage(ctx, q, owner, purpose)
		if errors.Is(err, domain.ErrNoProfile) || (err == nil && !old.Valid) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := setImage(ctx, q, owner, purpose, pgtype.UUID{}); err != nil {
			return err
		}
		return deleteOld(ctx, q, owner, old)
	})
}

// FindFile returns domain.ErrNotFound when no file has the ID, or it is not a UUID.
func (s *Store) FindFile(ctx context.Context, fileID string) (domain.StoredFile, error) {
	id, err := parseUUID(fileID)
	if err != nil {
		return domain.StoredFile{}, domain.ErrNotFound
	}
	row, err := s.q.FindStoredFile(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.StoredFile{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.StoredFile{}, fmt.Errorf("find stored file: %w", err)
	}
	return domain.StoredFile{
		ID:             row.ID.String(),
		OwnerID:        row.OwnerUserID.String(),
		Purpose:        domain.Purpose(row.Purpose),
		ObjectKey:      row.ObjectKey,
		ScanStatus:     row.ScanStatus,
		OwnerVerified:  row.OwnerVerified,
		OwnerSuspended: row.OwnerSuspended,
	}, nil
}

// lockedImage locks the profile row and returns the file ID its purpose column holds.
func lockedImage(ctx context.Context, q *sqlcgen.Queries, owner pgtype.UUID, purpose domain.Purpose) (pgtype.UUID, error) {
	row, err := q.LockProfileImages(ctx, owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, domain.ErrNoProfile
	}
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("lock profile: %w", err)
	}
	switch purpose {
	case domain.PurposePhoto:
		return row.PhotoFileID, nil
	case domain.PurposeCover:
		return row.CoverFileID, nil
	}
	return pgtype.UUID{}, fmt.Errorf("purpose %q is not a profile image", purpose)
}

func setImage(ctx context.Context, q *sqlcgen.Queries, owner pgtype.UUID, purpose domain.Purpose, file pgtype.UUID) error {
	var err error
	switch purpose {
	case domain.PurposePhoto:
		err = q.SetProfilePhoto(ctx, sqlcgen.SetProfilePhotoParams{FileID: file, UserID: owner})
	case domain.PurposeCover:
		err = q.SetProfileCover(ctx, sqlcgen.SetProfileCoverParams{FileID: file, UserID: owner})
	default:
		return fmt.Errorf("purpose %q is not a profile image", purpose)
	}
	if err != nil {
		return fmt.Errorf("set profile %s: %w", purpose, err)
	}
	return nil
}

// deleteOld deletes the file row the profile pointed at before; none is not an error.
func deleteOld(ctx context.Context, q *sqlcgen.Queries, owner, old pgtype.UUID) error {
	if !old.Valid {
		return nil
	}
	if err := q.DeleteOwnStoredFile(ctx, sqlcgen.DeleteOwnStoredFileParams{ID: old, OwnerUserID: owner}); err != nil {
		return fmt.Errorf("delete old stored file: %w", err)
	}
	return nil
}

func parseUUID(s string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(s); err != nil {
		return pgtype.UUID{}, fmt.Errorf("parse uuid: %w", err)
	}
	return id, nil
}
