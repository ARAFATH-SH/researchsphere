//go:build integration

package postgres

import (
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
)

// venue stores a live special issue (deadline in AoE, deadline given as an SQL expression of
// the database clock) or, with deadline "", a rolling journal, with one research field, in
// one statement so the field count check at commit passes; then its embedding at cosine cos
// from vector(1), made by model. It returns the venue's ID.
func (d db) venue(name, deadline string, cos float64, model string) string {
	d.t.Helper()
	var id string
	q := `WITH v AS (
			INSERT INTO venues (name, type, scope, submission_deadline, deadline_tz, official_url)
			VALUES ($1, 'special_issue', 'Scope of ' || $1, ` + deadline + `, 'AoE', 'https://venue.example.org')
			RETURNING id),
		f AS (INSERT INTO venue_fields (venue_id, field_key) SELECT id, 'water_resources' FROM v)
		SELECT id::text FROM v`
	if deadline == "" {
		q = `WITH v AS (
			INSERT INTO venues (name, type, scope, rolling, official_url)
			VALUES ($1, 'journal', 'Scope of ' || $1, true, 'https://journal.example.org')
			RETURNING id),
		f AS (INSERT INTO venue_fields (venue_id, field_key) SELECT id, 'water_resources' FROM v)
		SELECT id::text FROM v`
	}
	if err := d.pool.QueryRow(d.t.Context(), q, name).Scan(&id); err != nil {
		d.t.Fatalf("venue %s: %v", name, err)
	}
	d.exec(`INSERT INTO venue_embeddings (venue_id, model, content_hash, embedding)
		VALUES ($1, $2, repeat('c', 64), $3::text::vector)`, id, model, vector(cos))
	return id
}

// abstractEmbedding stores the project's title-and-abstract embedding: vector(1), by model.
func (d db) abstractEmbedding(projectID, model string) {
	d.t.Helper()
	d.exec(`INSERT INTO project_abstract_embeddings (project_id, model, content_hash, embedding)
		VALUES ($1, $2, repeat('d', 64), $3::text::vector)`, projectID, model, vector(1))
}

func venueIDs(cands []domain.VenueCandidate) []string {
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.ID
	}
	return out
}

// AI-6.1: venues are ranked by cosine only, top 5; a deadline that passed a moment ago (an
// AoE deadline is its instant), a removed venue, another model and rolling journals are left
// out of the deadline list, and only rolling journals are in the journal list.
func TestRankVenues(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	owner := d.user("Owner")
	p := d.project(owner, "Flood maps", "open")
	d.abstractEmbedding(p, modelA)

	soon := "clock_timestamp() + interval '1 minute'"
	var order []string
	for i, cos := range []float64{0.95, 0.9, 0.85, 0.8, 0.75, 0.7} {
		order = append(order, d.venue("Venue "+string(rune('A'+i)), soon, cos, modelA))
	}
	passed := d.venue("Just passed", "clock_timestamp() - interval '1 second'", 0.99, modelA)
	removed := d.venue("Removed", soon, 0.98, modelA)
	d.exec("UPDATE venues SET removed_at = now(), removal_reason = 'Duplicate entry' WHERE id = $1", removed)
	other := d.venue("Other model", soon, 0.97, modelB)
	j1 := d.venue("Journal one", "", 0.6, modelA)
	j2 := d.venue("Journal two", "", 0.96, modelA)

	got, err := s.RankVenues(t.Context(), p, 5)
	if err != nil {
		t.Fatal(err)
	}
	if want := order[:5]; !slices.Equal(venueIDs(got), want) {
		t.Fatalf("RankVenues = %v, want %v (most similar first, without past, removed, other model, rolling)", venueIDs(got), want)
	}
	for _, id := range []string{passed, removed, other, j1, j2, order[5]} {
		if contains(venueIDs(got), id) {
			t.Errorf("venue %s listed", id)
		}
	}
	first := got[0]
	if math.Abs(first.Cosine-0.95) > 1e-4 || first.Type != "special_issue" || first.TimeZone == nil || *first.TimeZone != "AoE" ||
		first.Deadline == nil || !first.Deadline.After(time.Now().Add(-time.Minute)) || first.OfficialURL != "https://venue.example.org" ||
		first.LinkKind != "official" || len(first.Fields) != 1 || first.Fields[0] != (domain.VenueField{Key: "water_resources", Label: "Water resources and hydrology"}) {
		t.Errorf("first = %+v", first)
	}

	journals, err := s.RankJournals(t.Context(), p, 3)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(venueIDs(journals), []string{j2, j1}) || journals[0].Deadline != nil || journals[0].TimeZone != nil {
		t.Errorf("RankJournals = %+v, want journal two then one, without deadlines", journals)
	}

	ok, err := s.VenuesStillEligible(t.Context(), []string{order[0], passed, removed, j1, "not-a-uuid"})
	if err != nil {
		t.Fatal(err)
	}
	if !ok[order[0]] || !ok[j1] || ok[passed] || ok[removed] || len(ok) != 2 {
		t.Errorf("VenuesStillEligible = %v", ok)
	}
}

// AI-6.3: the project's abstract and title-and-abstract embedding as the usecase reads them.
func TestVenueSelf(t *testing.T) {
	d := newDB(t)
	s := NewStore(d.pool)
	owner := d.user("Owner")
	p := d.project(owner, "Flood maps", "open")

	self, err := s.VenueSelf(t.Context(), p)
	if err != nil || self.HasAbstract || self.Embedding != nil {
		t.Fatalf("no abstract: %+v, %v", self, err)
	}
	d.exec("UPDATE projects SET abstract = $2 WHERE id = $1", p, " \n\t ")
	if self, err = s.VenueSelf(t.Context(), p); err != nil || self.HasAbstract {
		t.Errorf("blank abstract: %+v, %v", self, err)
	}
	d.exec("UPDATE projects SET abstract = 'We map floods.' WHERE id = $1", p)
	d.abstractEmbedding(p, modelA)
	if self, err = s.VenueSelf(t.Context(), p); err != nil || !self.HasAbstract || self.Embedding == nil || self.Embedding.Model != modelA {
		t.Errorf("with abstract: %+v, %v", self, err)
	}
	for _, id := range []string{"00000000-0000-4000-8000-000000000000", "nope"} {
		if _, err := s.VenueSelf(t.Context(), id); !errors.Is(err, domain.ErrProjectNotFound) {
			t.Errorf("VenueSelf(%s) = %v, want ErrProjectNotFound", id, err)
		}
	}
	// No title-and-abstract embedding: nothing is ranked.
	q := d.project(owner, "Other", "open")
	if got, err := s.RankVenues(t.Context(), q, 5); err != nil || len(got) != 0 {
		t.Errorf("RankVenues without embedding = %v, %v", got, err)
	}
}
