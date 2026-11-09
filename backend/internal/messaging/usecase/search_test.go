package usecase

import (
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/messaging/domain"
)

// searchHarness is newHarness with the search limit of the configuration's default.
func searchHarness() *harness {
	h := newHarness()
	limits := testLimits
	limits.SearchesPerMinute = 30
	h.svc = New(h.store, fakeTx{h.store}, h.conns, h.blocks, h.actors, h.limiter, h.pub, h.notifier, limits, h.clock, slog.New(slog.DiscardHandler))
	return h
}

const (
	hit1 = "019a5c2e-0001-7e8b-b7c1-2d3e4f5a6b7c"
	hit2 = "019a5c2e-0002-7e8b-b7c1-2d3e4f5a6b7c"
	hit3 = "019a5c2e-0003-7e8b-b7c1-2d3e4f5a6b7c"
)

// FR-16.8: the text must be 2 to 100 characters after trimming, counted as characters, so a
// 100-letter Bangla text passes and a 101-letter one does not.
func TestSearchChecksTheText(t *testing.T) {
	bn := strings.Repeat("ক", 100)
	for _, tc := range []struct {
		q    string
		want error
	}{
		{"", domain.ErrInvalidQuery},
		{"a", domain.ErrInvalidQuery},
		{"  a \n", domain.ErrInvalidQuery},
		{bn + "ক", domain.ErrInvalidQuery},
		{"a\x00b", domain.ErrInvalidQuery},
		{"\xffab", domain.ErrInvalidQuery},
		{" ab ", nil},
		{bn, nil},
		{"কা", nil},
	} {
		h := searchHarness()
		_, err := h.svc.Search(t.Context(), "ada", SearchInput{Query: tc.q})
		if !errors.Is(err, tc.want) {
			t.Errorf("%q: err = %v, want %v", tc.q, err, tc.want)
		}
		if tc.want == nil && h.store.searches[0].Text != strings.TrimSpace(tc.q) {
			t.Errorf("%q: store got %q", tc.q, h.store.searches[0].Text)
		}
		if tc.want != nil && len(h.store.searches) != 0 {
			t.Errorf("%q: searched anyway", tc.q)
		}
	}
}

// FR-16.8: the limit defaults to 20 and is cut to 50; below 1, and a cursor the server did not
// make, are refused. The store is asked for one more, to know whether a next page exists.
func TestSearchLimitAndCursor(t *testing.T) {
	ctx := t.Context()
	for _, tc := range []struct {
		limit *int
		want  int
	}{{nil, 21}, {intp(5), 6}, {intp(50), 51}, {intp(500), 51}} {
		h := searchHarness()
		if _, err := h.svc.Search(ctx, "ada", SearchInput{Query: "paper", Limit: tc.limit}); err != nil {
			t.Fatal(err)
		}
		if got := h.store.searches[0].Limit; got != tc.want {
			t.Errorf("limit %v: store limit = %d, want %d", tc.limit, got, tc.want)
		}
	}
	h := searchHarness()
	if _, err := h.svc.Search(ctx, "ada", SearchInput{Query: "paper", Limit: intp(0)}); !errors.Is(err, domain.ErrInvalidLimit) {
		t.Errorf("limit 0: %v", err)
	}
	for _, c := range []string{"x", encodeSearchCursor(domain.SearchCursor{Tier: 2, ID: hit1}), EncodeCursor(domain.Cursor{ID: hit1})} {
		if _, err := h.svc.Search(ctx, "ada", SearchInput{Query: "paper", Cursor: c}); !errors.Is(err, domain.ErrInvalidCursor) {
			t.Errorf("cursor %q: %v", c, err)
		}
	}
}

func intp(n int) *int { return &n }

// FR-16.8: paging follows the store's order with a cursor of the last item's tier and ID; the
// filters reach the store as given.
func TestSearchPages(t *testing.T) {
	h := searchHarness()
	h.connect("ada", "bob")
	conv := h.direct(t, "ada", "bob", "hi").Message.ConversationID
	h.store.searchHits = []domain.SearchHit{
		{MessageID: hit3, ConversationID: conv, Kind: domain.KindDirect, Tier: domain.TierWord, MatchedOn: domain.MatchText, Body: "paper one"},
		{MessageID: hit2, ConversationID: conv, Kind: domain.KindDirect, Tier: domain.TierSimilar, MatchedOn: domain.MatchText, Body: "papers"},
		{MessageID: hit1, ConversationID: conv, Kind: domain.KindDirect, Tier: domain.TierSimilar, MatchedOn: domain.MatchText, Body: "pape"},
	}
	after, before := h.now.Add(-time.Hour), h.now
	in := SearchInput{Query: "paper", ConversationID: conv, From: "bob", After: &after, Before: &before, Limit: intp(2)}
	page, err := h.svc.Search(t.Context(), "ada", in)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 || page.Items[0].MessageID != hit3 || page.Items[1].MessageID != hit2 || page.NextCursor == nil {
		t.Fatalf("page 1 = %+v", page)
	}
	q := h.store.searches[0]
	if q.ConversationID != conv || q.SenderID != "bob" || !q.After.Equal(after) || !q.Before.Equal(before) || q.Cursor != nil {
		t.Errorf("query = %+v", q)
	}
	in.Cursor = *page.NextCursor
	page, err = h.svc.Search(t.Context(), "ada", in)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].MessageID != hit1 || page.NextCursor != nil {
		t.Errorf("page 2 = %+v", page)
	}
	if c := h.store.searches[1].Cursor; c == nil || c.Tier != domain.TierSimilar || c.ID != hit2 {
		t.Errorf("cursor = %+v", c)
	}
}

// FR-16.8, NFR-13: a conversation the caller is not a member of is not found, the same as one
// that does not exist, and nothing is searched.
func TestSearchOneConversationNeedsMembership(t *testing.T) {
	h := searchHarness()
	h.connect("ada", "bob")
	conv := h.direct(t, "ada", "bob", "hi").Message.ConversationID
	for _, id := range []string{conv, "019a5c2e-9999-7e8b-b7c1-2d3e4f5a6b7c"} {
		if _, err := h.svc.Search(t.Context(), "cy", SearchInput{Query: "hi there", ConversationID: id}); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: err = %v", id, err)
		}
	}
	if len(h.store.searches) != 0 {
		t.Error("searched anyway")
	}
	if _, err := h.svc.Search(t.Context(), "bob", SearchInput{Query: "hi there", ConversationID: conv}); err != nil {
		t.Errorf("member: %v", err)
	}
}

// FR-16.8: 30 searches a minute per user; the 31st gets the time left. A refused text does not
// count, other users have their own count, and a limiter that cannot answer lets searches through.
func TestSearchRateLimit(t *testing.T) {
	h := searchHarness()
	ctx := t.Context()
	h.limiter.left["searches:minute"] = 42 * time.Second
	if _, err := h.svc.Search(ctx, "ada", SearchInput{Query: "x"}); !errors.Is(err, domain.ErrInvalidQuery) {
		t.Fatal(err)
	}
	for i := range 30 {
		if _, err := h.svc.Search(ctx, "ada", SearchInput{Query: "paper"}); err != nil {
			t.Fatalf("search %d: %v", i+1, err)
		}
	}
	var limited *domain.RateLimitedError
	if _, err := h.svc.Search(ctx, "ada", SearchInput{Query: "paper"}); !errors.As(err, &limited) || limited.RetryAfter != 42*time.Second {
		t.Fatalf("31st: %v", err)
	}
	if len(h.store.searches) != 30 {
		t.Errorf("store searched %d times", len(h.store.searches))
	}
	if _, err := h.svc.Search(ctx, "bob", SearchInput{Query: "paper"}); err != nil {
		t.Errorf("another user: %v", err)
	}
	h.limiter.failed = true
	if _, err := h.svc.Search(ctx, "ada", SearchInput{Query: "paper"}); err != nil {
		t.Errorf("limiter down: %v", err)
	}
}

// FR-16.8: a group result carries its avatars, a direct one an empty list; an attachment match
// shows the file's name as its snippet.
func TestSearchResults(t *testing.T) {
	h := searchHarness()
	avatars := []domain.Person{{UserID: "bob"}, {UserID: "cy"}}
	h.store.avatars = map[string][]domain.Person{"g1": avatars}
	h.store.searchHits = []domain.SearchHit{
		{MessageID: hit2, ConversationID: "g1", Kind: domain.KindGroup, Name: "Lab", MatchedOn: domain.MatchText, Body: "the thesis draft"},
		{MessageID: hit1, ConversationID: "d1", Kind: domain.KindDirect, MatchedOn: domain.MatchAttachment, Body: "see attached",
			AttachmentName: "Thesis_draft-v2.pdf"},
	}
	page, err := h.svc.Search(t.Context(), "ada", SearchInput{Query: "thesis"})
	if err != nil {
		t.Fatal(err)
	}
	g, d := page.Items[0], page.Items[1]
	if len(g.Avatars) != 2 || g.Snippet != "the thesis draft" || !rangesAre(t, g.Snippet, g.Highlights, "thesis") {
		t.Errorf("group = %+v", g)
	}
	if d.Avatars == nil || len(d.Avatars) != 0 || d.Snippet != "Thesis_draft-v2.pdf" || !rangesAre(t, d.Snippet, d.Highlights, "Thesis") {
		t.Errorf("direct = %+v", d)
	}
}

// rangesAre reports whether the ranges, read as code points of snippet, give want in order.
func rangesAre(t *testing.T, snippet string, rs []domain.Range, want ...string) bool {
	t.Helper()
	r := []rune(snippet)
	if len(rs) != len(want) {
		t.Errorf("%d ranges %v in %q, want %q", len(rs), rs, snippet, want)
		return false
	}
	for i, w := range want {
		rg := rs[i]
		if rg.Start < 0 || rg.Length < 1 || rg.Start+rg.Length > len(r) {
			t.Errorf("range %d %+v is outside %q", i, rg, snippet)
			return false
		}
		if got := string(r[rg.Start : rg.Start+rg.Length]); got != w {
			t.Errorf("range %d %+v of %q is %q, not %q", i, rg, snippet, got, w)
			return false
		}
	}
	return true
}

// FR-16.8: highlights count code points, not bytes or UTF-16 units, so they hold for Bangla,
// for an emoji (one code point, two UTF-16 units) before a match, and at the start and end.
func TestSnippetHighlights(t *testing.T) {
	filler := strings.Repeat("lorem ipsum dolor sit amet ", 10) // 270 characters
	for _, tc := range []struct {
		name, text, q string
		snippet       string // "" means: do not compare the whole snippet
		want          []string
		first         int // the first range's start, -1 to skip
	}{
		{"bangla word", "আমি গবেষণা করি", "গবেষণা", "আমি গবেষণা করি", []string{"গবেষণা"}, 4},
		{"bangla typo", "আমি গবেষণা করি", "গবেষনা", "", []string{"গবেষণা"}, 4},
		{"bangla partial", "আমাদের গবেষণাপত্র তৈরি", "গবেষণা", "", []string{"গবেষণা"}, 7},
		{"emoji first", "👍 গবেষণা", "গবেষণা", "", []string{"গবেষণা"}, 2},
		{"english word, any case", "Send the PAPER today", "paper", "", []string{"PAPER"}, 9},
		{"partial word", "We need a collaboration plan", "collab", "", []string{"collab"}, 10},
		{"typo", "We need a collaboration plan", "colaboration", "", []string{"collaboration"}, 10},
		{"swapped letters", "We need a collaboration plan", "collaboartion", "", []string{"collaboration"}, 10},
		{"mixed sentence", "নতুন paper নিয়ে গবেষণা চলছে", "গবেষণা paper", "", []string{"paper", "গবেষণা"}, 5},
		{"start", "Deadline is Friday", "deadline", "", []string{"Deadline"}, 0},
		{"end", "The review is due before the deadline", "deadline", "", []string{"deadline"}, 29},
		{"white space", "hello\n\n   world", "world", "hello world", []string{"world"}, 6},
		{"no word to mark", "please send the data set", "dataset", "please send the data set", nil, -1},
		{"long, at the start", "Deadline " + filler, "deadline", "", []string{"Deadline"}, 0},
		{"long, at the end", filler + "deadline", "deadline", "", []string{"deadline"}, -1},
		{"long, in the middle", filler + "গবেষণা " + filler, "গবেষণা", "", []string{"গবেষণা"}, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snippet, rs := Snippet(tc.text, queryTerms(tc.q))
			if tc.snippet != "" && snippet != tc.snippet {
				t.Errorf("snippet = %q", snippet)
			}
			rangesAre(t, snippet, rs, tc.want...)
			if tc.first >= 0 && len(rs) > 0 && rs[0].Start != tc.first {
				t.Errorf("first start = %d, want %d", rs[0].Start, tc.first)
			}
			if n := len([]rune(strings.Trim(snippet, ellipsis))); n > SnippetLength {
				t.Errorf("snippet has %d characters", n)
			}
		})
	}
}

// FR-16.8: a long text is cut on word boundaries, with "…" where it is cut.
func TestSnippetCuts(t *testing.T) {
	filler := strings.Repeat("lorem ipsum dolor sit amet ", 10)
	s, _ := Snippet("Deadline "+filler, queryTerms("deadline"))
	if !strings.HasPrefix(s, "Deadline ") || !strings.HasSuffix(s, ellipsis) || strings.HasSuffix(s, " "+ellipsis) {
		t.Errorf("start: %q", s)
	}
	s, rs := Snippet(filler+"deadline", queryTerms("deadline"))
	r := []rune(s)
	if !strings.HasPrefix(s, ellipsis) || !strings.HasSuffix(s, "deadline") || rs[0].Start+rs[0].Length != len(r) {
		t.Errorf("end: %q %v", s, rs)
	}
	if after := r[1]; after == ' ' || strings.HasPrefix(string(r[1:]), "orem") || strings.HasPrefix(string(r[1:]), "psum") {
		t.Errorf("end snippet starts inside a word: %q", s)
	}
	s, rs = Snippet(filler+"গবেষণা "+filler, queryTerms("গবেষণা"))
	if !strings.HasPrefix(s, ellipsis) || !strings.HasSuffix(s, ellipsis) || rs[0].Start < 20 || rs[0].Start > 50 {
		t.Errorf("middle: %q %v", s, rs)
	}
	// Nothing to mark: the start of the text.
	s, rs = Snippet(filler, queryTerms("zzzz"))
	if !strings.HasPrefix(s, "lorem ipsum") || !strings.HasSuffix(s, ellipsis) || rs != nil {
		t.Errorf("no highlight: %q %v", s, rs)
	}
}
