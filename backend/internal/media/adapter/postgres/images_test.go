//go:build integration

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/media/domain"
)

// createProfile stores a verified user with a profile row and no files. The keys are
// removed from the deletion queue when the test ends.
func createProfile(t *testing.T, pool *pgxpool.Pool, keys []string) string {
	t.Helper()
	userID := createUserWithFiles(t, pool)
	ctx := t.Context()
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), "DELETE FROM storage_deletions WHERE object_key = ANY($1)", keys); err != nil {
			t.Errorf("cleanup: %v", err)
		}
	})
	if _, err := pool.Exec(ctx, "UPDATE users SET email_verified_at = now() WHERE id = $1", userID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO profiles (user_id, full_name) VALUES ($1, 'Ada')", userID); err != nil {
		t.Fatal(err)
	}
	return userID
}

func newFile(owner string, p domain.Purpose, key string) domain.NewFile {
	return domain.NewFile{OwnerID: owner, Purpose: p, ObjectKey: key, ContentType: "image/jpeg", Size: 1234, Width: 400, Height: 400}
}

// profileImages returns the profile's photo and cover file IDs, "" when not set.
func profileImages(t *testing.T, pool *pgxpool.Pool, userID string) (string, string) {
	t.Helper()
	var photo, cover *string
	if err := pool.QueryRow(t.Context(), "SELECT photo_file_id::text, cover_file_id::text FROM profiles WHERE user_id = $1", userID).Scan(&photo, &cover); err != nil {
		t.Fatal(err)
	}
	deref := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	return deref(photo), deref(cover)
}

func isQueued(t *testing.T, pool *pgxpool.Pool, key string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(t.Context(), "SELECT EXISTS (SELECT 1 FROM storage_deletions WHERE object_key = $1)", key).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestReplaceProfileImage(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	keys := uniqueKeys(3)
	user := createProfile(t, pool, keys)

	first, err := s.ReplaceProfileImage(ctx, newFile(user, domain.PurposePhoto, keys[0]))
	if err != nil {
		t.Fatal(err)
	}
	cover, err := s.ReplaceProfileImage(ctx, newFile(user, domain.PurposeCover, keys[1]))
	if err != nil {
		t.Fatal(err)
	}
	if p, c := profileImages(t, pool, user); p != first || c != cover {
		t.Fatalf("profile = %s, %s; want %s, %s", p, c, first, cover)
	}
	f, err := s.FindFile(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if f.ScanStatus != "clean" || f.OwnerID != user || f.Purpose != domain.PurposePhoto || f.ObjectKey != keys[0] {
		t.Errorf("stored file = %+v", f)
	}

	second, err := s.ReplaceProfileImage(ctx, newFile(user, domain.PurposePhoto, keys[2]))
	if err != nil {
		t.Fatal(err)
	}
	if p, c := profileImages(t, pool, user); p != second || c != cover {
		t.Errorf("after replacing: profile = %s, %s; want %s, %s", p, c, second, cover)
	}
	if _, err := s.FindFile(ctx, first); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("old photo row: err = %v, want it deleted", err)
	}
	if !isQueued(t, pool, keys[0]) {
		t.Error("old photo object is not queued for deletion")
	}
	if isQueued(t, pool, keys[1]) || isQueued(t, pool, keys[2]) {
		t.Error("a current object is queued for deletion")
	}
}

func TestReplaceProfileImageWithoutProfile(t *testing.T) {
	s, pool := newTestStore(t)
	keys := uniqueKeys(1)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM storage_deletions WHERE object_key = ANY($1)", keys)
	})
	user := createUserWithFiles(t, pool)
	if _, err := s.ReplaceProfileImage(t.Context(), newFile(user, domain.PurposePhoto, keys[0])); !errors.Is(err, domain.ErrNoProfile) {
		t.Fatalf("err = %v, want ErrNoProfile", err)
	}
	var n int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM stored_files WHERE owner_user_id = $1", user).Scan(&n); err != nil || n != 0 {
		t.Errorf("stored files = %d (%v), want none", n, err)
	}
	if err := s.ClearProfileImage(t.Context(), user, domain.PurposePhoto); err != nil {
		t.Errorf("ClearProfileImage without a profile: %v", err)
	}
}

func TestClearProfileImage(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	keys := uniqueKeys(2)
	user := createProfile(t, pool, keys)
	if _, err := s.ReplaceProfileImage(ctx, newFile(user, domain.PurposePhoto, keys[0])); err != nil {
		t.Fatal(err)
	}
	cover, err := s.ReplaceProfileImage(ctx, newFile(user, domain.PurposeCover, keys[1]))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 { // the second time nothing is set
		if err := s.ClearProfileImage(ctx, user, domain.PurposePhoto); err != nil {
			t.Fatal(err)
		}
	}
	if p, c := profileImages(t, pool, user); p != "" || c != cover {
		t.Errorf("profile = %q, %q; want no photo and cover %s", p, c, cover)
	}
	if !isQueued(t, pool, keys[0]) {
		t.Error("removed photo is not queued for deletion")
	}
}

func TestFindFileOwnerState(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	keys := uniqueKeys(1)
	user := createProfile(t, pool, keys)
	id, err := s.ReplaceProfileImage(ctx, newFile(user, domain.PurposeCover, keys[0]))
	if err != nil {
		t.Fatal(err)
	}
	if f, err := s.FindFile(ctx, id); err != nil || !f.OwnerVerified || f.OwnerSuspended {
		t.Fatalf("FindFile = %+v, %v; want a verified, active owner", f, err)
	}
	if _, err := pool.Exec(ctx, "UPDATE users SET suspended_at = now(), email_verified_at = NULL WHERE id = $1", user); err != nil {
		t.Fatal(err)
	}
	if f, err := s.FindFile(ctx, id); err != nil || f.OwnerVerified || !f.OwnerSuspended {
		t.Errorf("FindFile = %+v, %v; want an unverified, suspended owner", f, err)
	}
	for _, bad := range []string{"not-a-uuid", "00000000-0000-0000-0000-000000000000"} {
		if _, err := s.FindFile(context.Background(), bad); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("FindFile(%q) err = %v, want ErrNotFound", bad, err)
		}
	}
}
