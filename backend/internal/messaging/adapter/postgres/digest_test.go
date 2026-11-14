//go:build integration

package postgres

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// exec runs a statement the test needs for its setup.
func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(t.Context(), sql, args...); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

// age moves a message's time back by d.
func age(t *testing.T, pool *pgxpool.Pool, messageID string, d time.Duration) {
	t.Helper()
	exec(t, pool, "UPDATE messages SET created_at = now() - make_interval(secs => $2) WHERE id = $1", messageID, d.Seconds())
}

// oldMessage adds a message from sender that has been unread for 25 hours. Members who joined
// after it are moved back to 100 hours ago, as if they had been there all along; a test about
// joining sets joined_at itself afterwards.
func oldMessage(t *testing.T, s *Store, pool *pgxpool.Pool, c, sender, body string) domain.Message {
	t.Helper()
	m := add(t, s, c, sender, body)
	age(t, pool, m.ID, 25*time.Hour)
	exec(t, pool, `UPDATE conversation_members SET joined_at = now() - interval '100 hours'
	               WHERE conversation_id = $1 AND joined_at > now() - interval '100 hours'`, c)
	return m
}

func digestCutoff() time.Time { return time.Now().Add(-24 * time.Hour) }

// prepare creates the users' digest rows, as the job does before claiming.
func prepare(t *testing.T, s *Store, ids ...string) {
	t.Helper()
	if err := s.PrepareDigests(t.Context(), ids); err != nil {
		t.Fatal(err)
	}
}

func candidates(t *testing.T, s *Store) []string {
	t.Helper()
	ids, err := s.DigestCandidates(t.Context(), digestCutoff(), 200)
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func isCandidate(t *testing.T, s *Store, id string) bool {
	t.Helper()
	return slices.Contains(candidates(t, s), id)
}

func conversations(t *testing.T, s *Store, user string, coveredUntil time.Time) []domain.DigestConversation {
	t.Helper()
	got, err := s.DigestConversations(t.Context(), user, coveredUntil, digestCutoff())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestDigestCountsOnlyMessagesUnreadFor24Hours(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	young := add(t, s, c.ID, bob, "hello")
	age(t, pool, young.ID, 23*time.Hour)
	if isCandidate(t, s, ada) {
		t.Fatal("a message 23 hours old makes a digest")
	}
	oldMessage(t, s, pool, c.ID, bob, "older")
	if !isCandidate(t, s, ada) {
		t.Fatal("a message 25 hours old makes no digest")
	}
	if isCandidate(t, s, bob) {
		t.Error("the sender's own message makes a digest for them")
	}
	if got := conversations(t, s, ada, time.Time{}); len(got) != 1 || got[0] != (domain.DigestConversation{Name: "Bob", Unread: 1}) {
		t.Errorf("conversations = %+v, want only Bob (1)", got)
	}
}

func TestDigestLeavesOutWhatDoesNotCount(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, s *Store, pool *pgxpool.Pool, ada, bob, conv string)
	}{
		{"read", func(t *testing.T, s *Store, pool *pgxpool.Pool, ada, _, conv string) {
			exec(t, pool, `UPDATE conversation_members SET last_read_message_id = (SELECT id FROM messages WHERE conversation_id = $1 ORDER BY id DESC LIMIT 1)
			               WHERE conversation_id = $1 AND user_id = $2`, conv, ada)
		}},
		{"deleted", func(t *testing.T, _ *Store, pool *pgxpool.Pool, _, _, conv string) {
			exec(t, pool, "UPDATE messages SET deleted_at = now(), body = NULL WHERE conversation_id = $1", conv)
		}},
		{"the reader blocked the sender", func(t *testing.T, _ *Store, pool *pgxpool.Pool, ada, bob, _ string) {
			exec(t, pool, "INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", ada, bob)
		}},
		{"the sender blocked the reader", func(t *testing.T, _ *Store, pool *pgxpool.Pool, ada, bob, _ string) {
			exec(t, pool, "INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", bob, ada)
		}},
		{"reader suspended", func(t *testing.T, _ *Store, pool *pgxpool.Pool, ada, _, _ string) {
			exec(t, pool, "UPDATE users SET suspended_at = now(), suspension_reason = 'A reason of ten or more' WHERE id = $1", ada)
		}},
		{"reader unverified", func(t *testing.T, _ *Store, pool *pgxpool.Pool, ada, _, _ string) {
			exec(t, pool, "UPDATE users SET email_verified_at = NULL WHERE id = $1", ada)
		}},
		{"setting off", func(t *testing.T, s *Store, _ *pgxpool.Pool, ada, _, _ string) {
			off := false
			if _, err := s.UpdateSettings(t.Context(), ada, domain.SettingsUpdate{UnreadMessageDigest: &off}); err != nil {
				t.Fatal(err)
			}
		}},
		{"a digest sent within 24 hours", func(t *testing.T, _ *Store, pool *pgxpool.Pool, ada, _, _ string) {
			exec(t, pool, `INSERT INTO message_digest_state (user_id, last_digest_at, covered_until)
			               VALUES ($1, now() - interval '23 hours', now() - interval '47 hours')`, ada)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, pool := newTestStore(t)
			ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
			c := create(t, s, ada, bob, domain.StateActive, nil)
			oldMessage(t, s, pool, c.ID, bob, "hello")
			if !isCandidate(t, s, ada) {
				t.Fatal("no digest before the change")
			}
			tt.setup(t, s, pool, ada, bob, c.ID)
			if isCandidate(t, s, ada) {
				t.Error("still a candidate")
			}
			if tt.name == "read" || tt.name == "deleted" || tt.name == "the reader blocked the sender" || tt.name == "the sender blocked the reader" {
				if got := conversations(t, s, ada, time.Time{}); len(got) != 0 {
					t.Errorf("conversations = %+v, want none", got)
				}
			}
			if tt.name == "a digest sent within 24 hours" || tt.name == "setting off" || tt.name == "reader suspended" || tt.name == "reader unverified" {
				prepare(t, s, ada)
				inTx(t, pool, func(ctx context.Context) error {
					if _, ok, err := s.ClaimDigest(ctx, ada, digestCutoff()); err != nil || ok {
						t.Errorf("ClaimDigest = %v, %v; want not claimed", ok, err)
					}
					return nil
				})
			}
		})
	}
}

func TestDigestLeavesOutRequestsAndSystemEvents(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob, cy := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy")
	req := create(t, s, ada, bob, domain.StateRequest, &bob)
	oldMessage(t, s, pool, req.ID, bob, "may I?")
	declined := create(t, s, ada, cy, domain.StateDeclined, &cy)
	oldMessage(t, s, pool, declined.ID, cy, "please")
	// A group whose only messages are system events (its creation).
	created := newGroup(t, s, pool, bob, ada)
	exec(t, pool, "UPDATE messages SET created_at = now() - interval '25 hours' WHERE conversation_id = $1", created.ConversationID)
	exec(t, pool, "UPDATE conversation_members SET joined_at = now() - interval '26 hours' WHERE conversation_id = $1", created.ConversationID)

	if isCandidate(t, s, ada) {
		t.Error("requests, declined conversations or system events make a digest")
	}
	n, err := s.PendingRequests(t.Context(), ada)
	if err != nil || n != 1 {
		t.Errorf("PendingRequests = %d, %v; want 1", n, err)
	}
}

func TestDigestCountsGroupsFromJoining(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob, cy := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy")
	g := newGroup(t, s, pool, bob, ada, cy)
	before := oldMessage(t, s, pool, g.ConversationID, bob, "before Ada joined")
	age(t, pool, before.ID, 30*time.Hour)
	oldMessage(t, s, pool, g.ConversationID, bob, "after")
	oldMessage(t, s, pool, g.ConversationID, cy, "after too")
	exec(t, pool, "UPDATE conversation_members SET joined_at = now() - interval '28 hours' WHERE conversation_id = $1 AND user_id = $2", g.ConversationID, ada)

	got := conversations(t, s, ada, time.Time{})
	if len(got) != 1 || got[0] != (domain.DigestConversation{Name: "Lab", Group: true, Unread: 2}) {
		t.Fatalf("conversations = %+v, want Lab (2) as a group", got)
	}
	// Cy blocks Ada: Cy's group message no longer counts for her; Bob's still does.
	exec(t, pool, "INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", cy, ada)
	if got := conversations(t, s, ada, time.Time{}); len(got) != 1 || got[0].Unread != 1 {
		t.Errorf("after the block: conversations = %+v, want Lab (1)", got)
	}
}

func TestDigestConversationsAreMostUnreadFirst(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob, cy := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy")
	withBob := create(t, s, ada, bob, domain.StateActive, nil)
	withCy := create(t, s, ada, cy, domain.StateActive, nil)
	oldMessage(t, s, pool, withBob.ID, bob, "1")
	for range 3 {
		oldMessage(t, s, pool, withCy.ID, cy, "x")
	}
	got := conversations(t, s, ada, time.Time{})
	want := []domain.DigestConversation{{Name: "Cy", Unread: 3}, {Name: "Bob", Unread: 1}}
	if !slices.Equal(got, want) {
		t.Errorf("conversations = %+v, want %+v", got, want)
	}
}

// The watermark: once a digest covered a message, no later digest counts it again.
func TestDigestWatermark(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	first := oldMessage(t, s, pool, c.ID, bob, "first")
	age(t, pool, first.ID, 50*time.Hour)
	prepare(t, s, ada)

	// A digest sent 26 hours ago covered everything up to 50 hours ago.
	var sentAt, covered time.Time
	inTx(t, pool, func(ctx context.Context) error {
		cutoff := time.Now().Add(-50 * time.Hour).Truncate(time.Microsecond)
		rec, ok, err := s.ClaimDigest(ctx, ada, cutoff.Add(-time.Hour))
		if err != nil || !ok || rec.FullName != "Ada" || !rec.CoveredUntil.IsZero() {
			t.Fatalf("ClaimDigest = %+v, %v, %v", rec, ok, err)
		}
		sentAt, covered = time.Now().Add(-26*time.Hour), cutoff
		return s.RecordDigest(ctx, domain.DigestRecord{UserID: ada, SentAt: sentAt, CoveredUntil: covered, Unread: 1, Conversations: 1})
	})
	if isCandidate(t, s, ada) {
		t.Fatal("the message the last digest covered makes a new digest")
	}
	inTx(t, pool, func(ctx context.Context) error {
		rec, ok, err := s.ClaimDigest(ctx, ada, digestCutoff())
		if err != nil || !ok || !rec.CoveredUntil.Equal(covered) {
			t.Fatalf("ClaimDigest = %+v, %v, %v; want the watermark", rec, ok, err)
		}
		if got := conversations(t, s, ada, rec.CoveredUntil); len(got) != 0 {
			t.Errorf("conversations after the watermark = %+v, want none", got)
		}
		return nil
	})

	oldMessage(t, s, pool, c.ID, bob, "second")
	if !isCandidate(t, s, ada) {
		t.Fatal("a newer message makes no digest")
	}
	if got := conversations(t, s, ada, covered); len(got) != 1 || got[0].Unread != 1 {
		t.Errorf("conversations = %+v, want only the new message", got)
	}
	var records int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM message_digests WHERE user_id = $1", ada).Scan(&records); err != nil || records != 1 {
		t.Errorf("records = %d, %v", records, err)
	}
}

// A second worker skips a user the first one holds, without waiting.
func TestClaimDigestSkipsALockedUser(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil)
	oldMessage(t, s, pool, c.ID, bob, "hello")
	prepare(t, s, ada)

	held := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- postgres.NewTransactor(pool).InTx(context.Background(), func(ctx context.Context) error {
			if _, ok, err := s.ClaimDigest(ctx, ada, digestCutoff()); err != nil || !ok {
				close(held)
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	<-held
	start := time.Now()
	inTx(t, pool, func(ctx context.Context) error {
		if _, ok, err := s.ClaimDigest(ctx, ada, digestCutoff()); err != nil || ok {
			t.Errorf("second ClaimDigest = %v, %v; want skipped", ok, err)
		}
		return nil
	})
	if time.Since(start) > 2*time.Second {
		t.Error("the second worker waited for the lock")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSettings(t *testing.T) {
	s, pool := newTestStore(t)
	ada := newUser(t, pool, "Ada")
	got, err := s.Settings(t.Context(), ada)
	if err != nil || got != domain.DefaultSettings || !got.UnreadMessageDigest {
		t.Fatalf("Settings = %+v, %v; want the default (on)", got, err)
	}
	off := false
	if got, err = s.UpdateSettings(t.Context(), ada, domain.SettingsUpdate{UnreadMessageDigest: &off}); err != nil || got.UnreadMessageDigest {
		t.Fatalf("UpdateSettings(off) = %+v, %v", got, err)
	}
	// An update that names nothing keeps the setting.
	if got, err = s.UpdateSettings(t.Context(), ada, domain.SettingsUpdate{}); err != nil || got.UnreadMessageDigest {
		t.Fatalf("UpdateSettings(nothing) = %+v, %v", got, err)
	}
	on := true
	if got, err = s.UpdateSettings(t.Context(), ada, domain.SettingsUpdate{UnreadMessageDigest: &on}); err != nil || !got.UnreadMessageDigest {
		t.Fatalf("UpdateSettings(on) = %+v, %v", got, err)
	}
	if err := s.TurnOffDigest(t.Context(), ada); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Settings(t.Context(), ada); got.UnreadMessageDigest {
		t.Error("TurnOffDigest left it on")
	}
	// Unknown users and IDs that are not UUIDs are no-ops.
	for _, id := range []string{"00000000-0000-4000-8000-000000000001", "nope"} {
		if err := s.TurnOffDigest(t.Context(), id); err != nil {
			t.Errorf("TurnOffDigest(%q) = %v", id, err)
		}
	}
}
