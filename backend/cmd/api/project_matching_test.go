package main

import (
	"testing"

	profileusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
	projectusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/project/usecase"
)

// People and project search share kept query vectors (projectQueryVectors), keyed by the
// normalized text, so both modules must normalize alike.
func TestQueryTextNormalizedAlike(t *testing.T) {
	for _, text := range []string{"Graph  Learning", "  flood\trisk\n maps ", "নাফিসা  হাসান", "ÉCOLE Normale", "x"} {
		if p, q := profileusecase.NormalizeQueryText(text), projectusecase.NormalizeQueryText(text); p != q {
			t.Errorf("%q: people %q, projects %q", text, p, q)
		}
	}
}
