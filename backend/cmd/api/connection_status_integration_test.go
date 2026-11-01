//go:build integration

// End-to-end tests of the caller's connection status on search results and connection
// lists (FR-6.1, FR-7.4): one person in each state on the same page, the caller's own row
// as self, and a fixed number of queries per page, however many rows it has.
package main

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
)

const searchAda = "/api/v1/search/people?q=Ada"

// rowStatus is one list row's connection fields.
type rowStatus struct {
	status    string
	requestID any
}

// pageStatuses returns the connection fields of each row of the page at path, by user ID.
func (e mediaEnv) pageStatuses(t *testing.T, viewer, path string) map[string]rowStatus {
	t.Helper()
	_, items := e.listTotal(t, viewer, path)
	out := map[string]rowStatus{}
	for _, it := range items {
		m := it.(map[string]any)
		out[m["userId"].(string)] = rowStatus{m["connectionStatus"].(string), m["connectionRequestId"]}
	}
	return out
}

func (e mediaEnv) connect(t *testing.T, from, to string) string {
	t.Helper()
	code, req := e.sendRequest(t, from, to)
	if code != http.StatusCreated {
		t.Fatalf("send %s -> %s: %d %v", from[:8], to[:8], code, req)
	}
	return req["requestId"].(string)
}

func (e mediaEnv) accept(t *testing.T, receiver, requestID string) {
	t.Helper()
	if code, body := e.call(t, receiver, http.MethodPost, "/api/v1/connections/requests/"+requestID+"/accept", ""); code != http.StatusOK {
		t.Fatalf("accept: %d %v", code, body)
	}
}

func (e mediaEnv) reject(t *testing.T, receiver, requestID string) {
	t.Helper()
	if code, body := e.call(t, receiver, http.MethodPost, "/api/v1/connections/requests/"+requestID+"/reject", ""); code != http.StatusNoContent {
		t.Fatalf("reject: %d %v", code, body)
	}
}

// statesFixture gives the viewer one person in each state: sentTo (pending_sent),
// from (pending_received), friend (connected), rejected (none after a rejection) and
// stranger (none). Every profile is named Ada, so a search for "Ada" finds them all.
type statesFixture struct {
	viewer, sentTo, from, friend, rejected, stranger string
	sentID, fromID                                   string
}

func newStates(t *testing.T, e mediaEnv) statesFixture {
	t.Helper()
	f := statesFixture{viewer: e.newProfileUser(t), sentTo: e.newProfileUser(t), from: e.newProfileUser(t),
		friend: e.newProfileUser(t), rejected: e.newProfileUser(t), stranger: e.newProfileUser(t)}
	f.sentID = e.connect(t, f.viewer, f.sentTo)
	f.fromID = e.connect(t, f.from, f.viewer)
	e.accept(t, f.viewer, e.connect(t, f.friend, f.viewer))
	e.reject(t, f.rejected, e.connect(t, f.viewer, f.rejected))
	// A link between two others says nothing about the viewer.
	e.connect(t, f.stranger, f.friend)
	return f
}

func (f statesFixture) want() map[string]rowStatus {
	return map[string]rowStatus{
		f.viewer:   {"self", nil},
		f.sentTo:   {"pending_sent", f.sentID},
		f.from:     {"pending_received", f.fromID},
		f.friend:   {"connected", nil},
		f.rejected: {"none", nil},
		f.stranger: {"none", nil},
	}
}

func checkStatuses(t *testing.T, label string, got, want map[string]rowStatus) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: %d rows, want %d: %v", label, len(got), len(want), got)
	}
	for id, w := range want {
		if g, ok := got[id]; !ok || g != w {
			t.Errorf("%s: %s is %v (present %v), want %v", label, id[:8], g, ok, w)
		}
	}
}

// FR-6.1, FR-7: one search page shows the caller's state with each person.
func TestSearchResultConnectionStatus(t *testing.T) {
	e := newMediaEnv(t)
	f := newStates(t, e)
	checkStatuses(t, "search", e.pageStatuses(t, f.viewer, searchAda), f.want())
}

// FR-7.4: another user's connections page shows the caller's own state with each person
// on it, the caller's own row included; the caller's own list is all connected.
func TestProfileConnectionsStatus(t *testing.T) {
	e := newMediaEnv(t)
	f := newStates(t, e)
	owner := e.newProfileUser(t)
	for _, u := range []string{f.viewer, f.sentTo, f.from, f.rejected, f.stranger} {
		e.accept(t, owner, e.connect(t, u, owner))
	}
	// friend is already connected with the viewer; it connects with owner from the other side.
	e.accept(t, f.friend, e.connect(t, owner, f.friend))
	checkStatuses(t, "owner's connections", e.pageStatuses(t, f.viewer, "/api/v1/profiles/"+owner+"/connections"), f.want())

	checkStatuses(t, "own connections", e.pageStatuses(t, f.viewer, "/api/v1/connections"), map[string]rowStatus{
		f.friend: {"connected", nil},
		owner:    {"connected", nil},
	})
}

// queryCounter is a pgx tracer that counts the statements sent to the database.
type queryCounter struct{ n atomic.Int64 }

func (c *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (c *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// newCountingEnv is newMediaEnv on a pool to the test database whose statements are
// counted.
func newCountingEnv(t *testing.T) (mediaEnv, *queryCounter) {
	t.Helper()
	cfg := testenv.DB(t).Config()
	counter := &queryCounter{}
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return newMediaEnvOnPool(t, pool, "", ""), counter
}

// queries is the number of statements a GET of path as viewer runs, and the rows it
// returns.
func (e mediaEnv) queries(t *testing.T, c *queryCounter, viewer, path string) (int64, int) {
	t.Helper()
	before := c.n.Load()
	_, items := e.listTotal(t, viewer, path)
	return c.n.Load() - before, len(items)
}

// A page of 20 runs as many statements as a page of 2: the statuses are read for the
// whole page at once, never one row at a time.
func TestConnectionStatusNoNPlusOne(t *testing.T) {
	e, counter := newCountingEnv(t)
	viewer, owner := e.newProfileUser(t), e.newProfileUser(t)
	// addPerson connects a new person with owner and, by turns, links them with the viewer
	// in some state, so the status lookup has rows to return.
	n := 0
	addPerson := func() {
		u := e.newProfileUser(t)
		e.accept(t, owner, e.connect(t, u, owner))
		switch n % 3 {
		case 0:
			e.connect(t, viewer, u)
		case 1:
			e.connect(t, u, viewer)
		case 2:
			e.accept(t, viewer, e.connect(t, u, viewer))
		}
		n++
	}
	addPerson()
	addPerson()
	connections := "/api/v1/profiles/" + owner + "/connections"
	smallSearch, rows := e.queries(t, counter, viewer, searchAda)
	if rows != 4 { // viewer, owner and two others
		t.Fatalf("small search page has %d rows, want 4", rows)
	}
	smallList, rows := e.queries(t, counter, viewer, connections)
	if rows != 2 {
		t.Fatalf("small connections page has %d rows, want 2", rows)
	}
	for range 18 {
		addPerson()
	}
	bigSearch, rows := e.queries(t, counter, viewer, searchAda)
	if rows != 20 {
		t.Fatalf("big search page has %d rows, want 20", rows)
	}
	bigList, rows := e.queries(t, counter, viewer, connections)
	if rows != 20 {
		t.Fatalf("big connections page has %d rows, want 20", rows)
	}
	if bigSearch != smallSearch {
		t.Errorf("search: %d statements for 20 rows, %d for 4; want the same", bigSearch, smallSearch)
	}
	if bigList != smallList {
		t.Errorf("connections list: %d statements for 20 rows, %d for 2; want the same", bigList, smallList)
	}
	t.Logf("statements per page: search %d, connections list %d", bigSearch, bigList)
}
