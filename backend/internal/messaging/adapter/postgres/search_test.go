//go:build integration

package postgres

import (
	"context"
	"slices"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// search runs one query and returns the IDs found, with each one's tier and source.
func search(t *testing.T, s *Store, viewer string, q domain.SearchQuery) []domain.SearchHit {
	t.Helper()
	if q.Limit == 0 {
		q.Limit = 50
	}
	hits, err := s.SearchMessages(t.Context(), viewer, q)
	if err != nil {
		t.Fatalf("search %q: %v", q.Text, err)
	}
	return hits
}

func hitIDs(hits []domain.SearchHit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.MessageID
	}
	return out
}

// FR-16.8: Bangla, English and mixed text match by whole words with the 'simple' configuration;
// partial words and typos match by trigrams and rank below word matches; attachment names are
// searched as words too. Within a tier, newer first.
func TestSearchMatches(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil).ID
	bn := add(t, s, c, bob, "আমরা আগামীকাল গবেষণা নিয়ে কথা বলব")
	en := add(t, s, c, ada, "Please send the paper draft")
	mixed := add(t, s, c, bob, "নতুন paper নিয়ে গবেষণা চলছে")
	collab := add(t, s, c, ada, "We need a collaboration plan")
	add(t, s, c, bob, "unrelated hello there")
	file := sendWith(t, s, pool, c, ada, newAttachment(t, s, c, ada, "Thesis_draft-v2.pdf"))
	add(t, s, c, bob, "the craft fair") // "draft" scores 0.5 here, below the threshold
	papers := add(t, s, c, ada, "PAPERS are due")

	type want struct {
		id   string
		tier int
		on   domain.MatchSource
	}
	for _, tc := range []struct {
		name, q string
		want    []want
	}{
		{"bangla word", "গবেষণা", []want{{mixed.ID, 0, domain.MatchText}, {bn.ID, 0, domain.MatchText}}},
		{"english word, any case, above a similar word", "Paper", []want{
			{mixed.ID, 0, domain.MatchText}, {en.ID, 0, domain.MatchText}, {papers.ID, 1, domain.MatchText}}},
		{"mixed sentence needs both words", "গবেষণা paper", []want{{mixed.ID, 0, domain.MatchText}}},
		{"partial word", "collab", []want{{collab.ID, 1, domain.MatchText}}},
		{"english typo", "colaboration", []want{{collab.ID, 1, domain.MatchText}}},
		{"bangla typo", "গবেষনা", []want{{mixed.ID, 1, domain.MatchText}, {bn.ID, 1, domain.MatchText}}},
		{"bangla partial word", "গবেষ", []want{{mixed.ID, 1, domain.MatchText}, {bn.ID, 1, domain.MatchText}}},
		{"attachment name", "thesis", []want{{file.ID, 0, domain.MatchAttachment}}},
		{"text and attachment, newest first", "draft", []want{{file.ID, 0, domain.MatchAttachment}, {en.ID, 0, domain.MatchText}}},
		{"nothing", "zzzz", nil},
		{"punctuation only", "!!", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hits := search(t, s, ada, domain.SearchQuery{Text: tc.q})
			if len(hits) != len(tc.want) {
				t.Fatalf("got %v, want %v", hitIDs(hits), tc.want)
			}
			for i, w := range tc.want {
				if h := hits[i]; h.MessageID != w.id || h.Tier != w.tier || h.MatchedOn != w.on {
					t.Errorf("hit %d = %s tier %d %s, want %+v", i, h.MessageID, h.Tier, h.MatchedOn, w)
				}
			}
		})
	}

	// A hit carries what the result shows.
	hits := search(t, s, ada, domain.SearchQuery{Text: "thesis"})
	h := hits[0]
	if h.ConversationID != c || h.Kind != domain.KindDirect || h.SenderID != ada || h.SenderName != "Ada" || h.Other == nil ||
		h.Other.UserID != bob || h.Other.FullName != "Bob" || !h.Other.Available || h.AttachmentName != "Thesis_draft-v2.pdf" ||
		h.Body != "" || !h.CreatedAt.Equal(file.CreatedAt) {
		t.Errorf("hit = %+v, other %+v", h, h.Other)
	}
	if h := search(t, s, ada, domain.SearchQuery{Text: "গবেষণা"})[1]; h.Body != *bn.Body || h.SenderName != "Bob" {
		t.Errorf("bangla hit = %+v", h)
	}
}

// visibilityWorld is one of each case of NFR-13: every text message has the word "token".
type visibilityWorld struct {
	ada, bob, cy, dee, eve string
	conversations          []string
	// Messages ada must and must not find.
	adaSees, adaMisses []string
}

func newVisibilityWorld(t *testing.T, s *Store) visibilityWorld {
	t.Helper()
	pool := s.pool
	ctx := t.Context()
	w := visibilityWorld{
		ada: newUser(t, pool, "Ada"), bob: newUser(t, pool, "Bob"), cy: newUser(t, pool, "Cy"),
		dee: newUser(t, pool, "Dee"), eve: newUser(t, pool, "Eve"),
	}
	direct := create(t, s, w.ada, w.bob, domain.StateActive, nil).ID
	w.adaSees = append(w.adaSees, add(t, s, direct, w.bob, "token in our chat").ID)

	// Someone else's conversation.
	others := create(t, s, w.cy, w.dee, domain.StateActive, nil).ID
	w.adaMisses = append(w.adaMisses, add(t, s, others, w.cy, "token between others").ID)

	// A group ada joins later: she sees only what came after.
	g := newGroup(t, s, pool, w.bob, w.cy).ConversationID
	w.adaMisses = append(w.adaMisses, add(t, s, g, w.bob, "token before ada joined").ID)
	inTx(t, pool, func(ctx context.Context) error {
		ev, err := s.AddSystemMessage(ctx, g, w.bob, domain.SystemEvent{Type: domain.EventMemberAdded, UserIDs: []string{w.ada}})
		if err != nil {
			return err
		}
		return s.AddGroupMembers(ctx, g, []string{w.ada}, ev.CreatedAt)
	})
	w.adaSees = append(w.adaSees, add(t, s, g, w.cy, "token after ada joined").ID)

	// A group ada has left: nothing in it, not even what she sent.
	left := newGroup(t, s, pool, w.ada, w.bob).ConversationID
	w.adaMisses = append(w.adaMisses, add(t, s, left, w.ada, "token from ada before leaving").ID,
		add(t, s, left, w.bob, "token in a group ada left").ID)
	removeMember(t, s, pool, left, w.ada, w.ada, domain.EventMemberLeft)
	w.adaMisses = append(w.adaMisses, add(t, s, left, w.bob, "token after ada left").ID)

	// A deleted message, and the earlier text of an edited one.
	gone := add(t, s, direct, w.ada, "token soon deleted")
	if _, err := s.DeleteMessage(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	w.adaMisses = append(w.adaMisses, gone.ID)
	edited := add(t, s, direct, w.bob, "oldword token")
	if _, err := s.EditMessage(ctx, edited.ID, "newword token"); err != nil {
		t.Fatal(err)
	}
	w.adaSees = append(w.adaSees, edited.ID)

	// A request from eve, one ada declined from dee, and a chat ada blocked: all readable by her.
	request := create(t, s, w.eve, w.ada, domain.StateRequest, &w.eve).ID
	w.adaSees = append(w.adaSees, add(t, s, request, w.eve, "token in a request").ID)
	declined := create(t, s, w.dee, w.ada, domain.StateDeclined, &w.dee).ID
	w.adaSees = append(w.adaSees, add(t, s, declined, w.dee, "token in a declined request").ID)
	blocked := create(t, s, w.ada, w.cy, domain.StateActive, nil).ID
	w.adaSees = append(w.adaSees, add(t, s, blocked, w.cy, "token before the block").ID)
	if _, err := pool.Exec(ctx, "INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", w.ada, w.cy); err != nil {
		t.Fatal(err)
	}
	w.conversations = []string{direct, others, g, left, request, declined, blocked}
	return w
}

// NFR-13: search finds what the thread shows and nothing else: not other people's
// conversations, not a group's messages from before joining or after leaving, not deleted
// messages, not earlier texts of edited ones, not system messages.
func TestSearchVisibility(t *testing.T) {
	s, _ := newTestStore(t)
	w := newVisibilityWorld(t, s)
	got := hitIDs(search(t, s, w.ada, domain.SearchQuery{Text: "token"}))
	for _, id := range w.adaSees {
		if !slices.Contains(got, id) {
			t.Errorf("ada does not find %s", id)
		}
	}
	for _, id := range w.adaMisses {
		if slices.Contains(got, id) {
			t.Errorf("ada finds %s", id)
		}
	}
	if len(got) != len(w.adaSees) {
		t.Errorf("ada finds %d messages, want %d", len(got), len(w.adaSees))
	}
	if hits := search(t, s, w.ada, domain.SearchQuery{Text: "oldword"}); len(hits) != 0 {
		t.Errorf("the edited-away text is found: %v", hitIDs(hits))
	}
	if hits := search(t, s, w.ada, domain.SearchQuery{Text: "newword"}); len(hits) != 1 {
		t.Errorf("the current text: %v", hitIDs(hits))
	}
	// The request is the two participants' only.
	if hits := search(t, s, w.bob, domain.SearchQuery{Text: "request"}); len(hits) != 0 {
		t.Errorf("bob finds the request: %v", hitIDs(hits))
	}
	if hits := search(t, s, w.eve, domain.SearchQuery{Text: "request"}); len(hits) != 1 || hits[0].MessageID != w.adaSees[3] {
		t.Errorf("eve's request: %v", hitIDs(hits))
	}
}

// NFR-13: the search and the thread share visible_messages, so for every user they agree:
// the search for a word every text message has finds exactly the thread's messages less
// deleted and system messages, in every conversation, for each of the five users.
func TestSearchAgreesWithTheThread(t *testing.T) {
	s, _ := newTestStore(t)
	w := newVisibilityWorld(t, s)
	for _, user := range []string{w.ada, w.bob, w.cy, w.dee, w.eve} {
		var thread []string
		for _, c := range w.conversations {
			ms, err := s.ListMessages(t.Context(), c, user, "", 1000)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range ms {
				if !m.Deleted() && !m.System() {
					thread = append(thread, m.ID)
				}
			}
		}
		found := hitIDs(search(t, s, user, domain.SearchQuery{Text: "token"}))
		slices.Sort(thread)
		slices.Sort(found)
		if !slices.Equal(thread, found) {
			t.Errorf("user %s: thread %v, search %v", user, thread, found)
		}
	}
}

// FR-16.8: the filters narrow the search: one conversation, one sender, after (inclusive) and
// before (exclusive) the moments given.
func TestSearchFilters(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob, cy := newUser(t, pool, "Ada"), newUser(t, pool, "Bob"), newUser(t, pool, "Cy")
	ab := create(t, s, ada, bob, domain.StateActive, nil).ID
	ac := create(t, s, ada, cy, domain.StateActive, nil).ID
	m1 := add(t, s, ab, ada, "budget one")
	m2 := add(t, s, ab, bob, "budget two")
	m3 := add(t, s, ac, cy, "budget three")
	m4 := add(t, s, ab, bob, "budget four")

	for _, tc := range []struct {
		name string
		q    domain.SearchQuery
		want []string
	}{
		{"all", domain.SearchQuery{}, []string{m4.ID, m3.ID, m2.ID, m1.ID}},
		{"conversation", domain.SearchQuery{ConversationID: ab}, []string{m4.ID, m2.ID, m1.ID}},
		{"sender", domain.SearchQuery{SenderID: bob}, []string{m4.ID, m2.ID}},
		{"conversation and sender", domain.SearchQuery{ConversationID: ac, SenderID: bob}, nil},
		{"after, inclusive", domain.SearchQuery{After: &m2.CreatedAt}, []string{m4.ID, m3.ID, m2.ID}},
		{"before, exclusive", domain.SearchQuery{Before: &m2.CreatedAt}, []string{m1.ID}},
		{"between", domain.SearchQuery{After: &m2.CreatedAt, Before: &m4.CreatedAt}, []string{m3.ID, m2.ID}},
		{"not a UUID", domain.SearchQuery{ConversationID: "x"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.q.Text = "budget"
			if got := hitIDs(search(t, s, ada, tc.q)); !slices.Equal(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// FR-16.8: pages follow one another across the two tiers with no gap and no repeat.
func TestSearchPagesAcrossTiers(t *testing.T) {
	s, pool := newTestStore(t)
	ada, bob := newUser(t, pool, "Ada"), newUser(t, pool, "Bob")
	c := create(t, s, ada, bob, domain.StateActive, nil).ID
	var words, similar []string
	for i := range 3 {
		similar = append(similar, add(t, s, c, ada, "papers "+string(rune('a'+i))).ID)
		words = append(words, add(t, s, c, bob, "the paper "+string(rune('a'+i))).ID)
	}
	slices.Reverse(words)
	slices.Reverse(similar)
	want := append(words, similar...)

	var got []string
	var cursor *domain.SearchCursor
	for range 10 {
		hits := search(t, s, ada, domain.SearchQuery{Text: "paper", Limit: 2, Cursor: cursor})
		got = append(got, hitIDs(hits)...)
		if len(hits) < 2 {
			break
		}
		last := hits[len(hits)-1]
		cursor = &domain.SearchCursor{Tier: last.Tier, ID: last.MessageID}
	}
	if !slices.Equal(got, want) {
		t.Errorf("pages %v, want %v", got, want)
	}
}
