package postgres

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// wordSimilarityThreshold is the pg_trgm word similarity a name needs to match the search
// text when no name contains it (FR-6.2): word_similarity(q, full_name) compares q with
// the best-matching stretch of the name, so a typo in any one word matches. See
// docs/decisions.md for how it was chosen.
const wordSimilarityThreshold = 0.4

// Match ranks of the search queries (queries.sql), best first.
const (
	rankNameExact   = 0
	rankNamePrefix  = 1
	rankNameContain = 2
	rankNameSimilar = 3
	rankInstitution = 4
	rankInterest    = 5
	// 6 is a headline match.
)

// resultInterests is how many interests each result shows.
const resultInterests = 3

var _ domain.PeopleSearcher = (*Store)(nil)

// SearchPeople runs the researcher search in one read-only snapshot, so the page, total
// and facets agree (FR-6.1 to FR-6.3).
func (s *Store) SearchPeople(ctx context.Context, pq domain.PeopleQuery, maxFacets int) (domain.PeopleResults, error) {
	var res domain.PeopleResults
	err := s.searchTx(ctx, func(t pgx.Tx) error {
		r, err := searchPeople(ctx, s.q.WithTx(t), pq, maxFacets)
		res = r
		return err
	})
	if err != nil {
		return domain.PeopleResults{}, fmt.Errorf("search people: %w", err)
	}
	return res, nil
}

func searchPeople(ctx context.Context, q *sqlcgen.Queries, pq domain.PeopleQuery, maxFacets int) (domain.PeopleResults, error) {
	lim, err := limitsOf(pq, maxFacets)
	if err != nil {
		return domain.PeopleResults{}, err
	}
	if err := q.SetWordSimilarityThreshold(ctx, strconv.FormatFloat(wordSimilarityThreshold, 'f', -1, 64)); err != nil {
		return domain.PeopleResults{}, fmt.Errorf("set word similarity threshold: %w", err)
	}
	pattern := containsPattern(pq.Q)
	// A viewer that is not a UUID is NULL here, which matches no block.
	var viewer pgtype.UUID
	_ = viewer.Scan(pq.ViewerID)
	literal, err := q.AnyVisibleNameContains(ctx, sqlcgen.AnyVisibleNameContainsParams{Pattern: pattern, ViewerID: viewer})
	if err != nil {
		return domain.PeopleResults{}, fmt.Errorf("check name matches: %w", err)
	}
	interests := make([]string, len(pq.Interests))
	for i, name := range pq.Interests {
		interests[i] = strings.ToLower(name)
	}
	f := filters{viewer: viewer, q: pq.Q, pattern: pattern, fuzzy: !literal, institution: text(pq.Institution), interests: interests}

	sum, err := q.SearchPeopleSummary(ctx, sqlcgen.SearchPeopleSummaryParams{
		ViewerID: f.viewer, Q: f.q, Fuzzy: f.fuzzy, Pattern: f.pattern, Institution: f.institution, Interests: f.interests,
	})
	if err != nil {
		return domain.PeopleResults{}, fmt.Errorf("count results: %w", err)
	}
	res := domain.PeopleResults{Total: int(sum.Total)}
	if sum.CorrectedQuery != "" {
		res.Fuzzy = true
		res.CorrectedQuery = &sum.CorrectedQuery
	}

	if res.Hits, err = page(ctx, q, f, lim); err != nil {
		return domain.PeopleResults{}, err
	}

	// The institution facet leaves out its own filter, so the other institutions stay.
	insts, err := q.SearchInstitutionFacets(ctx, sqlcgen.SearchInstitutionFacetsParams{
		MaxFacets: lim.facets, ViewerID: f.viewer, Q: f.q, Fuzzy: f.fuzzy, Pattern: f.pattern, Interests: f.interests,
	})
	if err != nil {
		return domain.PeopleResults{}, fmt.Errorf("count institutions: %w", err)
	}
	res.Institutions = make([]domain.Facet, len(insts))
	for i, r := range insts {
		res.Institutions[i] = domain.Facet{Name: r.Name, Count: int(r.N)}
	}

	ints, err := q.SearchInterestFacets(ctx, sqlcgen.SearchInterestFacetsParams{
		MaxResults: lim.interestFacets, ViewerID: f.viewer,
		Q: f.q, Fuzzy: f.fuzzy, Pattern: f.pattern, Institution: f.institution, Interests: f.interests,
	})
	if err != nil {
		return domain.PeopleResults{}, fmt.Errorf("count interests: %w", err)
	}
	res.Interests = interestFacets(pq.Interests, ints)
	return res, nil
}

// filters are the parameters every search query shares.
type filters struct {
	viewer      pgtype.UUID
	q, pattern  string
	fuzzy       bool
	institution pgtype.Text
	interests   []string // lower case
}

// limits are the LIMIT and OFFSET values of the search queries.
type limits struct {
	offset, pageSize, facets, interestFacets int32
}

func limitsOf(pq domain.PeopleQuery, maxFacets int) (limits, error) {
	var (
		l   limits
		err error
	)
	for _, v := range []struct {
		dst *int32
		n   int
	}{
		{&l.offset, (pq.Page - 1) * pq.PageSize},
		{&l.pageSize, pq.PageSize},
		{&l.facets, maxFacets},
		{&l.interestFacets, maxFacets + len(pq.Interests)}, // the selected ones come on top
	} {
		if *v.dst, err = int32Of(v.n); err != nil {
			return limits{}, fmt.Errorf("search limits: %w", err)
		}
	}
	return l, nil
}

// page returns the requested page with up to resultInterests interests per result.
func page(ctx context.Context, q *sqlcgen.Queries, f filters, lim limits) ([]domain.PersonHit, error) {
	rows, err := q.SearchPeoplePage(ctx, sqlcgen.SearchPeoplePageParams{
		ViewerID:   f.viewer,
		Q:          f.q,
		PageOffset: lim.offset,
		PageSize:   lim.pageSize,
		Fuzzy:      f.fuzzy, Pattern: f.pattern, Institution: f.institution, Interests: f.interests,
	})
	if err != nil {
		return nil, fmt.Errorf("list results: %w", err)
	}
	hits := make([]domain.PersonHit, len(rows))
	ids := make([]pgtype.UUID, len(rows))
	byID := make(map[string]*domain.PersonHit, len(rows))
	for i, r := range rows {
		hits[i] = domain.PersonHit{
			UserID:      r.UserID.String(),
			FullName:    r.FullName,
			Headline:    textPtr(r.Headline),
			Institution: textPtr(r.Institution),
			PhotoFileID: uuidPtr(r.PhotoFileID),
			Interests:   []string{},
			MatchedOn:   matchField(r.Rank),
		}
		ids[i] = r.UserID
		byID[hits[i].UserID] = &hits[i]
	}
	if len(rows) == 0 {
		return hits, nil
	}
	tags, err := q.ListSearchResultInterests(ctx, sqlcgen.ListSearchResultInterestsParams{
		Pattern: f.pattern, Interests: f.interests, UserIds: ids,
	})
	if err != nil {
		return nil, fmt.Errorf("list result interests: %w", err)
	}
	for _, t := range tags {
		if h := byID[t.UserID.String()]; h != nil && len(h.Interests) < resultInterests {
			h.Interests = append(h.Interests, t.Name)
		}
	}
	return hits, nil
}

// interestFacets puts the selected interests first, in the order given, with the stored
// spelling and count; one that no result has gets count 0. The others follow in the
// query's order.
func interestFacets(selected []string, rows []sqlcgen.SearchInterestFacetsRow) []domain.Facet {
	out := make([]domain.Facet, 0, len(rows)+len(selected))
	for _, name := range selected {
		f := domain.Facet{Name: name}
		if i := slices.IndexFunc(rows, func(r sqlcgen.SearchInterestFacetsRow) bool {
			return r.Selected && strings.EqualFold(r.Name, name)
		}); i >= 0 {
			f = domain.Facet{Name: rows[i].Name, Count: int(rows[i].N)}
		}
		out = append(out, f)
	}
	for _, r := range rows {
		if !r.Selected {
			out = append(out, domain.Facet{Name: r.Name, Count: int(r.N)})
		}
	}
	return out
}

// matchField names the field of a match rank.
func matchField(rank int32) domain.MatchField {
	switch rank {
	case rankNameExact, rankNamePrefix, rankNameContain, rankNameSimilar:
		return domain.MatchName
	case rankInstitution:
		return domain.MatchInstitution
	case rankInterest:
		return domain.MatchInterest
	default: // 6
		return domain.MatchHeadline
	}
}

// likeEscaper escapes the characters ILIKE treats specially (backslash is its default
// escape character), so the search text matches only itself.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// containsPattern is the ILIKE pattern of text anywhere in a value. It is passed as a
// query parameter, never joined into SQL.
func containsPattern(text string) string {
	return "%" + likeEscaper.Replace(text) + "%"
}
