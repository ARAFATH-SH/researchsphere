//go:build integration

package postgres

import (
	"errors"
	"slices"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// NFR-13: a page centred on a message reads through visible_messages like the thread. For
// every user and conversation of the visibility world, each message of the thread is found and
// the newer ones come back oldest first; a message the thread hides (another conversation,
// before joining, after leaving) is not found.
func TestAroundAgreesWithTheThread(t *testing.T) {
	s, _ := newTestStore(t)
	w := newVisibilityWorld(t, s)
	ctx := t.Context()
	for _, user := range []string{w.ada, w.bob, w.cy, w.dee, w.eve} {
		for _, c := range w.conversations {
			thread, err := s.ListMessages(ctx, c, user, "", 1000) // newest first
			if err != nil {
				t.Fatal(err)
			}
			for i, m := range thread {
				got, err := s.FindVisibleMessage(ctx, c, user, m.ID)
				if err != nil || got.ID != m.ID {
					t.Errorf("user %s, %s: find = %v, %v", user, m.ID, got.ID, err)
				}
				newer, err := s.ListMessagesAfter(ctx, c, user, m.ID, 1000)
				if err != nil {
					t.Fatal(err)
				}
				want := slices.Clone(thread[:i])
				slices.Reverse(want)
				if !slices.EqualFunc(newer, want, func(a, b domain.Message) bool { return a.ID == b.ID }) {
					t.Errorf("user %s, after %s: %d newer, want %d", user, m.ID, len(newer), len(want))
				}
			}
		}
	}
	for _, missing := range w.adaMisses {
		for _, c := range w.conversations {
			m, err := s.FindVisibleMessage(ctx, c, w.ada, missing)
			if errors.Is(err, domain.ErrNotFound) {
				continue
			}
			// A deleted message stays in its place in the thread; only search misses it.
			if err != nil || !m.Deleted() {
				t.Errorf("ada finds %s in %s: %v", missing, c, err)
			}
		}
	}
	if _, err := s.FindVisibleMessage(ctx, w.conversations[0], w.ada, "not-a-uuid"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("not a UUID: %v", err)
	}
	if _, err := s.ListMessagesAfter(ctx, w.conversations[0], w.ada, "not-a-uuid", 10); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Errorf("after not a UUID: %v", err)
	}
}

// FR-16.6: a page names people who are or were in the conversation, also after they left;
// nobody else, whatever the client asks. The name is always given; available follows the
// profile view rule; a deleted account is gone.
func TestPagePeople(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := t.Context()
	ada, bob, cy, dee, eve := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy"),
		newUser(t, pool, "Dee"), newUser(t, pool, "Eve")
	g := newGroup(t, s, pool, ada, bob, cy, dee).ConversationID
	removeMember(t, s, pool, g, ada, cy, domain.EventMemberRemoved)
	direct := create(t, s, ada, eve, domain.StateActive, nil).ID

	people := func(refs ...domain.PersonRef) map[string]domain.PagePerson {
		t.Helper()
		got, err := s.People(ctx, ada, refs)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]domain.PagePerson{}
		for _, p := range got {
			out[p.UserID] = p
		}
		return out
	}
	got := people(
		domain.PersonRef{ConversationID: g, UserID: bob},
		domain.PersonRef{ConversationID: g, UserID: cy},  // removed: a past stay
		domain.PersonRef{ConversationID: g, UserID: eve}, // never in the group
		domain.PersonRef{ConversationID: direct, UserID: eve},
		domain.PersonRef{ConversationID: direct, UserID: dee}, // not in that conversation
		domain.PersonRef{ConversationID: g, UserID: bob},      // asked twice
		domain.PersonRef{ConversationID: g, UserID: "not-a-uuid"},
	)
	if len(got) != 3 || got[bob].FullName != "Bob" || got[cy].FullName != "Cy" || got[eve].FullName != "Eve" || !got[bob].Available {
		t.Errorf("people = %+v", got)
	}

	// Suspended, or blocking the viewer: named, not available. Deleted: gone.
	if _, err := pool.Exec(ctx, "UPDATE users SET suspended_at = now() WHERE id = $1", bob); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", cy, ada); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", dee); err != nil {
		t.Fatal(err)
	}
	got = people(
		domain.PersonRef{ConversationID: g, UserID: bob},
		domain.PersonRef{ConversationID: g, UserID: cy},
		domain.PersonRef{ConversationID: g, UserID: dee},
	)
	if len(got) != 2 || got[bob].Available || got[bob].FullName != "Bob" || got[cy].Available || got[cy].PhotoFileID != nil {
		t.Errorf("hidden people = %+v", got)
	}
	if got := people(); len(got) != 0 {
		t.Errorf("no refs = %v", got)
	}
}
