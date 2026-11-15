//go:build integration

package postgres

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/testenv"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

const (
	modelA = "e5@aaaa"
	modelB = "e5@bbbb"
	dims   = 768
)

type db struct {
	t    *testing.T
	pool *pgxpool.Pool
}

func newDB(t *testing.T) db {
	t.Helper()
	return db{t: t, pool: testenv.DB(t)}
}

func (d db) exec(sql string, args ...any) {
	d.t.Helper()
	if _, err := d.pool.Exec(d.t.Context(), sql, args...); err != nil {
		d.t.Fatalf("%s: %v", sql, err)
	}
}

// user stores a verified user with a profile named name and returns its ID.
func (d db) user(name string) string {
	d.t.Helper()
	var id string
	if err := d.pool.QueryRow(d.t.Context(), `INSERT INTO users (email, password_hash, email_verified_at)
		VALUES ($1, 'hash', now()) RETURNING id::text`, testenv.Email(d.t)).Scan(&id); err != nil {
		d.t.Fatal(err)
	}
	d.exec("INSERT INTO profiles (user_id, full_name, headline, institution) VALUES ($1, $2, 'Researcher', 'Northfield Institute')", id, name)
	return id
}

// vector is a unit vector whose cosine with vector(1) is cos: cos*e0 + sin*e1.
func vector(cos float64) string {
	parts := make([]string, dims)
	for i := range parts {
		parts[i] = "0"
	}
	parts[0] = strconv.FormatFloat(cos, 'f', 6, 64)
	parts[1] = strconv.FormatFloat(math.Sqrt(1-cos*cos), 'f', 6, 64)
	return "[" + strings.Join(parts, ",") + "]"
}

// embed stores userID's embedding at cosine cos from the query vector, made by model.
func (d db) embed(userID string, cos float64, model string) {
	d.t.Helper()
	d.exec(`INSERT INTO profile_embeddings (user_id, model, content_hash, embedding)
		VALUES ($1, $2, repeat('a', 64), $3::text::vector)
		ON CONFLICT (user_id) DO UPDATE SET model = EXCLUDED.model, embedding = EXCLUDED.embedding`, userID, model, vector(cos))
}

func (d db) tag(table, link, col, userID, name string) {
	d.t.Helper()
	d.exec("INSERT INTO "+table+" (name) VALUES ($1) ON CONFLICT (name) DO NOTHING", name)
	d.exec("INSERT INTO "+link+" (user_id, "+col+") SELECT $1, id FROM "+table+" WHERE name = $2", userID, name)
}

func (d db) interest(userID, name string) {
	d.tag("research_interests", "user_interests", "interest_id", userID, name)
}
func (d db) skill(userID, name string) { d.tag("skills", "user_skills", "skill_id", userID, name) }

func (d db) connect(a, b, status string) {
	d.t.Helper()
	d.exec("INSERT INTO connections (requester_id, receiver_id, status) VALUES ($1, $2, $3)", a, b, status)
}

func ids(cands []domain.Candidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.UserID
	}
	return out
}

func contains(list []string, id string) bool {
	for _, x := range list {
		if x == id {
			return true
		}
	}
	return false
}

// AI-2.1: each kind of user that must be left out is, alone; a rejected request and an
// ended timed suspension leave a user in.
func TestRankExclusions(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	me := d.user("Me")
	d.embed(me, 1, modelA)

	keep := d.user("Keep")
	d.embed(keep, 0.5, modelA)

	excluded := map[string]string{}
	add := func(why string, setup func(id string)) {
		id := d.user(why)
		setup(id)
		excluded[why] = id
	}
	add("connected", func(id string) { d.embed(id, 0.99, modelA); d.connect(me, id, "accepted") })
	add("connected, they asked", func(id string) { d.embed(id, 0.99, modelA); d.connect(id, me, "accepted") })
	add("pending sent", func(id string) { d.embed(id, 0.98, modelA); d.connect(me, id, "pending") })
	add("pending received", func(id string) { d.embed(id, 0.98, modelA); d.connect(id, me, "pending") })
	add("blocked by me", func(id string) {
		d.embed(id, 0.97, modelA)
		d.exec("INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", me, id)
	})
	add("blocked me", func(id string) {
		d.embed(id, 0.97, modelA)
		d.exec("INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", id, me)
	})
	add("unverified", func(id string) {
		d.embed(id, 0.96, modelA)
		d.exec("UPDATE users SET email_verified_at = NULL WHERE id = $1", id)
	})
	add("suspended", func(id string) {
		// The suspension trigger deletes the embedding; storing it afterwards tests the
		// query's own filter.
		d.exec("UPDATE users SET suspended_at = now(), suspension_reason = 'reason long enough' WHERE id = $1", id)
		d.embed(id, 0.96, modelA)
	})
	add("no embedding", func(string) {})
	add("other model", func(id string) { d.embed(id, 0.999, modelB) })

	rejected := d.user("Rejected")
	d.embed(rejected, 0.6, modelA)
	d.connect(rejected, me, "rejected")
	ended := d.user("Ended suspension")
	d.exec("UPDATE users SET suspended_at = now() - interval '2 days', suspended_until = now() - interval '1 hour', suspension_reason = 'reason long enough' WHERE id = $1", ended)
	d.embed(ended, 0.55, modelA)

	got, err := s.Rank(t.Context(), me, 10)
	if err != nil {
		t.Fatal(err)
	}
	gotIDs := ids(got)
	for why, id := range excluded {
		if contains(gotIDs, id) {
			t.Errorf("%s: recommended, want left out", why)
		}
	}
	if contains(gotIDs, me) {
		t.Error("the caller is recommended to themselves")
	}
	if want := []string{rejected, ended, keep}; strings.Join(gotIDs, ",") != strings.Join(want, ",") {
		t.Errorf("Rank = %v, want %v (rejected request, ended suspension, keep)", gotIDs, want)
	}

	// StillEligible applies the same filters.
	all := append([]string{keep, rejected, ended}, mapValues(excluded)...)
	ok, err := s.StillEligible(t.Context(), me, append(all, "not-a-uuid"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{keep, rejected, ended} {
		if !ok[id] {
			t.Errorf("StillEligible left out %s", id)
		}
	}
	for why, id := range excluded {
		if ok[id] {
			t.Errorf("StillEligible kept %s", why)
		}
	}
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// The order is the cosine similarity, most similar first, with the cosine returned, and
// the limit is kept; a caller without an embedding gets nothing.
func TestRankOrderAndLimit(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	me := d.user("Me")
	d.embed(me, 1, modelA)
	coss := []float64{0.2, 0.9, 0.5, 0.7, 0.3, 0.8, 0.4, 0.6, 0.1, 0.85, 0.95, 0.75}
	byCos := map[float64]string{}
	for _, c := range coss {
		id := d.user("U")
		d.embed(id, c, modelA)
		byCos[c] = id
	}
	got, err := s.Rank(t.Context(), me, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []float64{0.95, 0.9, 0.85, 0.8, 0.75, 0.7, 0.6, 0.5, 0.4, 0.3}
	if len(got) != len(want) {
		t.Fatalf("%d candidates, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].UserID != byCos[w] || math.Abs(got[i].Cosine-w) > 1e-4 {
			t.Errorf("#%d = %s (cos %.4f), want cos %.2f", i, got[i].UserID, got[i].Cosine, w)
		}
	}
	if got, err := s.Rank(t.Context(), me, 3); err != nil || len(got) != 3 {
		t.Errorf("limit 3: %d candidates, %v", len(got), err)
	}

	stranger := d.user("No embedding")
	if got, err := s.Rank(t.Context(), stranger, 10); err != nil || len(got) != 0 {
		t.Errorf("caller without an embedding: %d candidates, %v; want none", len(got), err)
	}
}

// AI-2.4: a less similar user with many connections, publications and citations ranks
// below a more similar one with none.
func TestRankIgnoresPopularity(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	me := d.user("Me")
	d.embed(me, 1, modelA)
	newcomer := d.user("Newcomer")
	d.embed(newcomer, 0.9, modelA)
	famous := d.user("Famous")
	d.embed(famous, 0.8, modelA)
	for i := range 30 {
		other := d.user("Fan")
		d.connect(other, famous, "accepted")
		var pub string
		if err := d.pool.QueryRow(t.Context(), "INSERT INTO publications (title, publication_year) VALUES ($1, 2020) RETURNING id::text",
			"Highly cited paper "+strconv.Itoa(i)).Scan(&pub); err != nil {
			t.Fatal(err)
		}
		d.exec("INSERT INTO user_publications (user_id, publication_id, confirmed_at) VALUES ($1, $2, now())", famous, pub)
	}
	got, err := s.Rank(t.Context(), me, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < 2 || got[0].UserID != newcomer || got[1].UserID != famous {
		t.Errorf("order %v, want the newcomer (cos 0.9) before the famous user (cos 0.8)", ids(got))
	}
}

// Self reads the AI-4.2 fields, the tags in name order and the embedding stamp; a user
// without a profile or embedding reads as empty.
func TestSelf(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	me := d.user("Me")
	self, err := s.Self(t.Context(), me)
	if err != nil {
		t.Fatal(err)
	}
	if len(self.Interests) != 0 || len(self.Skills) != 0 || self.HasBio || self.Embedding != nil {
		t.Errorf("new profile: %+v, want empty", self)
	}
	d.exec("UPDATE profiles SET bio = 'I study rivers.' WHERE user_id = $1", me)
	d.interest(me, "water quality")
	d.interest(me, "Hydrology")
	d.skill(me, "R")
	d.embed(me, 1, modelA)
	self, err = s.Self(t.Context(), me)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(self.Interests, "|") != "Hydrology|water quality" || strings.Join(self.Skills, "|") != "R" || !self.HasBio {
		t.Errorf("Self = %+v", self)
	}
	if self.Embedding == nil || self.Embedding.Model != modelA || len(self.Embedding.ContentHash) != 64 || self.Embedding.UpdatedAt.IsZero() {
		t.Errorf("stamp = %+v", self.Embedding)
	}
	if self, err := s.Self(t.Context(), "not-a-uuid"); err != nil || self.Embedding != nil {
		t.Errorf("malformed ID: %+v, %v", self, err)
	}
}

// Rank returns each candidate's tags, so the usecase can find the shared ones.
func TestRankTags(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	me := d.user("Me")
	d.embed(me, 1, modelA)
	other := d.user("Other")
	d.embed(other, 0.9, modelA)
	d.interest(other, "GIS")
	d.interest(other, "Flood risk")
	d.skill(other, "Python")
	photo := d.user("No tags")
	d.embed(photo, 0.8, modelA)
	got, err := s.Rank(t.Context(), me, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || strings.Join(got[0].Interests, "|") != "Flood risk|GIS" || strings.Join(got[0].Skills, "|") != "Python" {
		t.Fatalf("Rank = %+v", got)
	}
	if got[0].FullName != "Other" || got[0].Headline == nil || *got[0].Institution != "Northfield Institute" || got[0].PhotoFileID != nil {
		t.Errorf("person = %+v", got[0].Person)
	}
	if len(got[1].Interests) != 0 || len(got[1].Skills) != 0 {
		t.Errorf("user without tags got %v %v", got[1].Interests, got[1].Skills)
	}
}
