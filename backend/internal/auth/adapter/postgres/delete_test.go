//go:build integration

package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/auth/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

// testUser registers a user with a profile and a verification token.
func testUser(t *testing.T, s *Store, name string) string {
	t.Helper()
	id, err := register(t.Context(), s, testenv.Email(t), name, rand.Text(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
	return id
}

// exec runs a seeding statement and fails the test on error.
func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("seed %q: %v", sql, err)
	}
}

// insertID runs an INSERT ... RETURNING id and returns the id.
func insertID(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(t.Context(), sql, args...).Scan(&id); err != nil {
		t.Fatalf("seed %q: %v", sql, err)
	}
	return id
}

// testProject stores a project owned by ownerID, with editors.
func testProject(t *testing.T, pool *pgxpool.Pool, title, ownerID string, editors ...string) string {
	t.Helper()
	ctx := t.Context()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var id string
	if err := tx.QueryRow(ctx, "INSERT INTO projects (title, description) VALUES ($1, 'test') RETURNING id", title).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'owner')", id, ownerID); err != nil {
		t.Fatal(err)
	}
	for _, e := range editors {
		if _, err := tx.Exec(ctx, "INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'editor')", id, e); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return id
}

// testPublication stores a publication linked to each of userIDs.
func testPublication(t *testing.T, pool *pgxpool.Pool, title string, userIDs ...string) string {
	t.Helper()
	id := insertID(t, pool, "INSERT INTO publications (title) VALUES ($1) RETURNING id", title)
	for _, u := range userIDs {
		exec(t, pool, "INSERT INTO user_publications (user_id, publication_id) VALUES ($1, $2)", u, id)
	}
	return id
}

// deleteAccount runs the transaction the usecase runs, refusing when the user owns a
// shared project.
func deleteAccount(ctx context.Context, s *Store, userID string) error {
	return s.RunInTx(ctx, func(tx domain.Tx) error {
		if err := tx.LockOwnedProjects(ctx, userID); err != nil {
			return err
		}
		shared, err := tx.FindSharedOwnedProjects(ctx, userID)
		if err != nil {
			return err
		}
		if len(shared) > 0 {
			return &domain.SharedProjectsError{Projects: shared}
		}
		if _, err := tx.DeleteSoloProjects(ctx, userID); err != nil {
			return err
		}
		if _, err := tx.DeleteUserPublications(ctx, userID); err != nil {
			return err
		}
		return tx.DeleteUser(ctx, userID)
	})
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(t.Context(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

// FR-2.8: deleting the user removes everything that is the user's.
func TestDeleteAccountRemovesUserData(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	ada := testUser(t, s, "Ada Lovelace")
	grace := testUser(t, s, "Grace Hopper")

	exec(t, pool, "INSERT INTO sessions (user_id, refresh_token_hash, expires_at) VALUES ($1, $2, now() + interval '1 day')", ada, rand.Text())
	exec(t, pool, `INSERT INTO profile_embeddings (user_id, model, content_hash, embedding)
		VALUES ($1, 'test@1', repeat('a', 64), array_fill(0.1::real, ARRAY[768])::vector)`, ada)
	exec(t, pool, "INSERT INTO connections (requester_id, receiver_id, status) VALUES ($1, $2, 'accepted')", ada, grace)
	adaPost := insertID(t, pool, "INSERT INTO posts (author_id, body) VALUES ($1, 'Ada''s post') RETURNING id", ada)
	gracePost := insertID(t, pool, "INSERT INTO posts (author_id, body) VALUES ($1, 'Grace''s post') RETURNING id", grace)
	exec(t, pool, "INSERT INTO post_comments (post_id, author_id, body) VALUES ($1, $2, 'by Ada')", gracePost, ada)
	exec(t, pool, "INSERT INTO post_comments (post_id, author_id, body) VALUES ($1, $2, 'by Grace')", adaPost, grace)
	exec(t, pool, "INSERT INTO post_likes (post_id, user_id) VALUES ($1, $2)", gracePost, ada)
	exec(t, pool, "INSERT INTO post_likes (post_id, user_id) VALUES ($1, $2)", adaPost, grace)
	solo := testProject(t, pool, "Ada's notes", ada)
	gracesProject := testProject(t, pool, "Grace's compiler", grace, ada)
	soloPub := testPublication(t, pool, "Notes on the Analytical Engine", ada)
	sharedPub := testPublication(t, pool, "A joint paper", ada, grace)

	if err := deleteAccount(ctx, s, ada); err != nil {
		t.Fatalf("delete account: %v", err)
	}

	for _, c := range []struct{ table, sql string }{
		{"users", "SELECT count(*) FROM users WHERE id = $1"},
		{"profiles", "SELECT count(*) FROM profiles WHERE user_id = $1"},
		{"posts", "SELECT count(*) FROM posts WHERE author_id = $1"},
		{"post_comments", "SELECT count(*) FROM post_comments WHERE author_id = $1"},
		{"post_likes", "SELECT count(*) FROM post_likes WHERE user_id = $1"},
		{"connections", "SELECT count(*) FROM connections WHERE requester_id = $1 OR receiver_id = $1"},
		{"sessions", "SELECT count(*) FROM sessions WHERE user_id = $1"},
		{"auth_tokens", "SELECT count(*) FROM auth_tokens WHERE user_id = $1"},
		{"profile_embeddings", "SELECT count(*) FROM profile_embeddings WHERE user_id = $1"},
		{"project_members", "SELECT count(*) FROM project_members WHERE user_id = $1"},
		{"user_publications", "SELECT count(*) FROM user_publications WHERE user_id = $1"},
	} {
		if n := count(t, pool, c.sql, ada); n != 0 {
			t.Errorf("%s: %d rows of the deleted user remain", c.table, n)
		}
	}
	// Other users' comments and likes on the deleted user's post go with the post.
	if n := count(t, pool, "SELECT count(*) FROM post_comments WHERE post_id = $1", adaPost); n != 0 {
		t.Errorf("%d comments on the deleted post remain", n)
	}
	if n := count(t, pool, "SELECT count(*) FROM post_likes WHERE post_id = $1", adaPost); n != 0 {
		t.Errorf("%d likes on the deleted post remain", n)
	}
	if n := count(t, pool, "SELECT count(*) FROM projects WHERE id = $1", solo); n != 0 {
		t.Error("the project where the user was the only member remains")
	}
	if n := count(t, pool, "SELECT count(*) FROM project_members WHERE project_id = $1 AND user_id = $2 AND role = 'owner'", gracesProject, grace); n != 1 {
		t.Error("another user's project lost its owner")
	}
	if n := count(t, pool, "SELECT count(*) FROM publications WHERE id = $1", soloPub); n != 0 {
		t.Error("a publication with no author left remains")
	}
	if n := count(t, pool, "SELECT count(*) FROM user_publications WHERE publication_id = $1 AND user_id = $2", sharedPub, grace); n != 1 {
		t.Error("a publication shared with a co-author was not kept for the co-author")
	}
	// The other user keeps their own data.
	if n := count(t, pool, "SELECT count(*) FROM posts WHERE id = $1", gracePost); n != 1 {
		t.Error("another user's post was deleted")
	}
	if _, err := s.FindUserByID(ctx, grace); err != nil {
		t.Errorf("find the other user: %v", err)
	}
	if _, err := s.FindUserByID(ctx, ada); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("find the deleted user: error = %v, want ErrNotFound", err)
	}
}

// FR-2.9: the owner of a project that has other members cannot be deleted, and nothing
// changes.
func TestDeleteAccountRefusedForOwnerOfSharedProject(t *testing.T) {
	ctx := t.Context()
	s, pool := newTestStore(t)
	ada := testUser(t, s, "Ada Lovelace")
	grace := testUser(t, s, "Grace Hopper")
	solo := testProject(t, pool, "Ada's notes", ada)
	shared := testProject(t, pool, "Joint survey", ada, grace)
	soloPub := testPublication(t, pool, "Notes", ada)

	err := deleteAccount(ctx, s, ada)
	var refused *domain.SharedProjectsError
	if !errors.As(err, &refused) {
		t.Fatalf("delete account: error = %v, want a SharedProjectsError", err)
	}
	if want := []domain.ProjectRef{{ID: shared, Title: "Joint survey"}}; len(refused.Projects) != 1 || refused.Projects[0] != want[0] {
		t.Errorf("projects = %v, want %v", refused.Projects, want)
	}
	if _, err := s.FindUserByID(ctx, ada); err != nil {
		t.Errorf("the user was deleted: %v", err)
	}
	if n := count(t, pool, "SELECT count(*) FROM projects WHERE id = $1", solo); n != 1 {
		t.Error("the solo project was deleted although the deletion was refused")
	}
	if n := count(t, pool, "SELECT count(*) FROM user_publications WHERE publication_id = $1", soloPub); n != 1 {
		t.Error("a publication link was deleted although the deletion was refused")
	}
}

func TestDeleteUserNotFound(t *testing.T) {
	s, _ := newTestStore(t)
	err := s.RunInTx(t.Context(), func(tx domain.Tx) error {
		return tx.DeleteUser(t.Context(), "00000000-0000-0000-0000-000000000000")
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("DeleteUser() error = %v, want ErrNotFound", err)
	}
}
