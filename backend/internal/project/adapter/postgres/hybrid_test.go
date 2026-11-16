//go:build integration

package postgres

import (
	"math"
	"strings"
	"testing"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

const (
	hybridModel = "e5@aaaa"
	hybridDims  = 768
)

// unitVector is cos*e0 + sin*e1: its cosine with e0 is cos.
func unitVector(cos float64) []float32 {
	v := make([]float32, hybridDims)
	v[0], v[1] = float32(cos), float32(math.Sqrt(1-cos*cos))
	return v
}

func (d db) embedProject(id string, cos float64, model string) {
	d.t.Helper()
	d.exec(`INSERT INTO project_embeddings (project_id, model, content_hash, embedding)
		VALUES ($1, $2, repeat('c', 64), $3::text::vector)`, id, model, vectorText(unitVector(cos)))
}

func titles(res domain.SearchResult) []string {
	out := make([]string, len(res.Items))
	for i, it := range res.Items {
		out[i] = it.Title + "/" + it.MatchedOn
	}
	return out
}

// AI-5.1 for projects: a strong title match comes first; keyword and meaning results are
// merged; a project only the meaning side found is matched on meaning; only open projects
// with a visible owner are found, by either side; embeddings of another model are not
// compared; total is the merged size and the page is cut from it.
func TestSearchHybrid(t *testing.T) {
	d := newDB(t)
	ctx := t.Context()
	viewer := d.user("Viewer")
	owner := d.user("Owner")

	exact := d.project(owner, "Flood Risk", "Mapping rivers")
	d.embedProject(exact, 0.2, hybridModel)
	d.project(owner, "Coastal survey", "Flood risk along the coast")
	meaningOnly := d.project(owner, "Inundation forecasting", "Predicting where water rises")
	d.embedProject(meaningOnly, 0.95, hybridModel)
	both := d.project(owner, "Urban drainage", "Flood risk in cities")
	d.embedProject(both, 0.9, hybridModel)

	closed := d.project(owner, "Flood closed", "Flood risk")
	d.embedProject(closed, 0.99, hybridModel)
	d.status(closed, domain.StatusClosed)
	blocker := d.user("Blocker")
	d.exec("INSERT INTO blocks (blocker_id, blocked_id) VALUES ($1, $2)", blocker, viewer)
	hidden := d.project(blocker, "Flood hidden", "Flood risk")
	d.embedProject(hidden, 0.99, hybridModel)
	otherModel := d.project(owner, "Drought study", "Dry seasons")
	d.embedProject(otherModel, 0.999, "e5@bbbb")

	v := domain.QueryVector{Model: hybridModel, Values: unitVector(1)}
	res, err := d.store.SearchHybrid(ctx, domain.SearchQuery{ViewerID: viewer, Q: "flood risk", Limit: 20}, v)
	if err != nil {
		t.Fatal(err)
	}
	got := titles(res)
	if len(got) == 0 || got[0] != "Flood Risk/title" {
		t.Fatalf("results %v, want the strong title match first", got)
	}
	want := map[string]bool{"Flood Risk/title": true, "Coastal survey/description": true,
		"Inundation forecasting/meaning": true, "Urban drainage/description": true}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected result %s (closed, hidden or another model's)", g)
		}
	}
	if len(got) != 4 || res.Total != 4 {
		t.Errorf("results %v total %d, want 4", got, res.Total)
	}
	// Urban drainage is on both lists, so it outranks the keyword-only and meaning-only ones.
	if got[1] != "Urban drainage/description" {
		t.Errorf("#2 = %s, want the project both sides found", got[1])
	}
	for _, it := range res.Items {
		if it.Skills == nil || it.Owner.UserID != owner || it.Status != domain.StatusOpen {
			t.Errorf("summary %+v", it)
		}
	}

	page2, err := d.store.SearchHybrid(ctx, domain.SearchQuery{ViewerID: viewer, Q: "flood risk", Limit: 2, Offset: 2}, v)
	if err != nil || len(page2.Items) != 2 || page2.Total != 4 || strings.Join(titles(page2), ",") != strings.Join(got[2:], ",") {
		t.Errorf("page 2 = %v (total %d), %v; want %v", titles(page2), page2.Total, err, got[2:])
	}
	far, err := d.store.SearchHybrid(ctx, domain.SearchQuery{ViewerID: viewer, Q: "flood risk", Limit: 20, Offset: 100}, v)
	if err != nil || len(far.Items) != 0 || far.Total != 4 {
		t.Errorf("past the end: %v, %v", titles(far), err)
	}
}

// The strong title match ignores letter case and accents, and needs 3 characters for a
// prefix; the fuzzy fields come from the keyword side.
func TestSearchHybridStrongTitleAndFuzzy(t *testing.T) {
	d := newDB(t)
	ctx := t.Context()
	viewer := d.user("Viewer")
	owner := d.user("Owner")
	d.project(owner, "Café culture", "Coffee houses")
	d.project(owner, "Graph learning", "Networks")
	v := domain.QueryVector{Model: hybridModel, Values: unitVector(1)}

	res, err := d.store.SearchHybrid(ctx, domain.SearchQuery{ViewerID: viewer, Q: "cafe", Limit: 20}, v)
	if err != nil || len(res.Items) != 1 || res.Items[0].MatchedOn != "title" {
		t.Errorf("cafe: %v, %v", titles(res), err)
	}
	res, err = d.store.SearchHybrid(ctx, domain.SearchQuery{ViewerID: viewer, Q: "graf lerning", Limit: 20}, v)
	if err != nil || !res.Fuzzy || res.CorrectedQuery == nil || *res.CorrectedQuery != "Graph learning" {
		t.Errorf("typo: fuzzy %v corrected %v, %v", res.Fuzzy, res.CorrectedQuery, err)
	}
}
