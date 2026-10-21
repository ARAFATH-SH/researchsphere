//go:build integration

package postgres

import (
	"context"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// The profile reads its photo and cover (FR-4.6), and editing the profile fields keeps them.
func TestProfileImagesAreReadAndKept(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	id := newUser(t, pool)
	if _, err := s.UpsertProfile(ctx, domain.Profile{UserID: id, FullName: "Ada"}); err != nil {
		t.Fatal(err)
	}
	var photo, cover string
	for purpose, dst := range map[string]*string{"photo": &photo, "cover": &cover} {
		err := pool.QueryRow(ctx, `INSERT INTO stored_files (owner_user_id, purpose, object_key, content_type, size_bytes)
			VALUES ($1, $2, $3, 'image/jpeg', 100) RETURNING id::text`, id, purpose, "users/"+id+"/"+purpose+"/x.jpg").Scan(dst)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM storage_deletions WHERE object_key LIKE $1", "users/"+id+"/%")
	})
	if _, err := pool.Exec(ctx, "UPDATE profiles SET photo_file_id = $2, cover_file_id = $3 WHERE user_id = $1", id, photo, cover); err != nil {
		t.Fatal(err)
	}

	a, err := s.FindAccount(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if a.Profile.PhotoFileID == nil || *a.Profile.PhotoFileID != photo || a.Profile.CoverFileID == nil || *a.Profile.CoverFileID != cover {
		t.Errorf("FindAccount: photo %v, cover %v; want %s, %s", a.Profile.PhotoFileID, a.Profile.CoverFileID, photo, cover)
	}
	got, err := s.UpsertProfile(ctx, domain.Profile{UserID: id, FullName: "Ada Lovelace"})
	if err != nil {
		t.Fatal(err)
	}
	if got.PhotoFileID == nil || *got.PhotoFileID != photo || got.CoverFileID == nil || *got.CoverFileID != cover {
		t.Errorf("UpsertProfile: photo %v, cover %v; want them kept", got.PhotoFileID, got.CoverFileID)
	}
}
