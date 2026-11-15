package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
)

// Hybrid search settings (AI-5.1).
const (
	// HybridEfSearch is the HNSW candidate list size (hnsw.ef_search); it must be at least
	// domain.HybridDepth for the index to return that many, and 100 keeps the approximate
	// top 50 close to the exact one, as for recommendations.
	HybridEfSearch = 100
	// MinStrongPrefix is the shortest text whose prefix matches count as strong name
	// matches: "Nafisa" and "Nafisa Ha" for "Nafisa Hasan", not "Na".
	MinStrongPrefix = 3
)

// SearchPeopleHybrid runs the keyword search, the strong name matches and the search by
// meaning in one read-only snapshot, merges them with domain.FusePeople, and counts the
// facets over the merged list: the institution facet over the list made without the
// institution filter, as in the keyword search (FR-6.3, AI-5.1, AI-5.2).
func (s *Store) SearchPeopleHybrid(ctx context.Context, pq domain.PeopleQuery, v domain.QueryVector, maxFacets int) (domain.PeopleResults, error) {
	var res domain.PeopleResults
	err := s.searchTx(ctx, func(t pgx.Tx) error {
		r, err := searchHybrid(ctx, s.q.WithTx(t), pq, v, maxFacets)
		res = r
		return err
	})
	if err != nil {
		return domain.PeopleResults{}, fmt.Errorf("hybrid search people: %w", err)
	}
	return res, nil
}

func searchHybrid(ctx context.Context, q *sqlcgen.Queries, pq domain.PeopleQuery, v domain.QueryVector, maxFacets int) (domain.PeopleResults, error) {
	lim, err := limitsOf(pq, maxFacets)
	if err != nil {
		return domain.PeopleResults{}, err
	}
	if err := setupHybrid(ctx, q); err != nil {
		return domain.PeopleResults{}, err
	}
	pattern := containsPattern(pq.Q)
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

	merged, people, err := hybridCandidates(ctx, q, f, v)
	if err != nil {
		return domain.PeopleResults{}, err
	}
	res := domain.PeopleResults{Total: len(merged)}

	// fuzzy and correctedQuery come from the keyword side.
	sum, err := q.SearchPeopleSummary(ctx, sqlcgen.SearchPeopleSummaryParams{
		ViewerID: f.viewer, Q: f.q, Fuzzy: f.fuzzy, Pattern: f.pattern, Institution: f.institution, Interests: f.interests,
	})
	if err != nil {
		return domain.PeopleResults{}, fmt.Errorf("count keyword results: %w", err)
	}
	if sum.CorrectedQuery != "" {
		res.Fuzzy = true
		res.CorrectedQuery = &sum.CorrectedQuery
	}

	ids, err := uuids(merged)
	if err != nil {
		return domain.PeopleResults{}, err
	}
	instIDs := ids
	if f.institution.Valid {
		g := f
		g.institution = pgtype.Text{}
		unfiltered, _, err := hybridCandidates(ctx, q, g, v)
		if err != nil {
			return domain.PeopleResults{}, err
		}
		if instIDs, err = uuids(unfiltered); err != nil {
			return domain.PeopleResults{}, err
		}
	}
	insts, err := q.InstitutionFacetsAmong(ctx, sqlcgen.InstitutionFacetsAmongParams{UserIds: instIDs, MaxFacets: lim.facets})
	if err != nil {
		return domain.PeopleResults{}, fmt.Errorf("count institutions: %w", err)
	}
	res.Institutions = make([]domain.Facet, len(insts))
	for i, r := range insts {
		res.Institutions[i] = domain.Facet{Name: r.Name, Count: int(r.N)}
	}
	ints, err := q.InterestFacetsAmong(ctx, sqlcgen.InterestFacetsAmongParams{Interests: f.interests, UserIds: ids, MaxResults: lim.interestFacets})
	if err != nil {
		return domain.PeopleResults{}, fmt.Errorf("count interests: %w", err)
	}
	rows := make([]sqlcgen.SearchInterestFacetsRow, len(ints))
	for i, r := range ints {
		rows[i] = sqlcgen.SearchInterestFacetsRow(r)
	}
	res.Interests = interestFacets(pq.Interests, rows)

	start := min(int(lim.offset), len(merged))
	end := min(start+int(lim.pageSize), len(merged))
	res.Hits = make([]domain.PersonHit, 0, end-start)
	for _, m := range merged[start:end] {
		h := people[m.UserID]
		h.MatchedOn = m.MatchedOn
		h.Interests = []string{}
		res.Hits = append(res.Hits, h)
	}
	if err := addResultInterests(ctx, q, f, res.Hits); err != nil {
		return domain.PeopleResults{}, err
	}
	return res, nil
}

// setupHybrid sets the transaction's word similarity threshold (FR-6.2) and HNSW scan.
func setupHybrid(ctx context.Context, q *sqlcgen.Queries) error {
	if err := q.SetWordSimilarityThreshold(ctx, strconv.FormatFloat(wordSimilarityThreshold, 'f', -1, 64)); err != nil {
		return fmt.Errorf("set word similarity threshold: %w", err)
	}
	if err := q.SetHNSWIterativeScan(ctx); err != nil {
		return fmt.Errorf("set hnsw iterative scan: %w", err)
	}
	if err := q.SetHNSWEfSearch(ctx, strconv.Itoa(HybridEfSearch)); err != nil {
		return fmt.Errorf("set hnsw ef_search: %w", err)
	}
	return nil
}

// hybridCandidates reads the three candidate lists with filters f and merges them. people
// holds the public fields of every candidate, by user ID.
func hybridCandidates(ctx context.Context, q *sqlcgen.Queries, f filters, v domain.QueryVector) ([]domain.Fused, map[string]domain.PersonHit, error) {
	depth := int32(domain.HybridDepth)
	people := map[string]domain.PersonHit{}
	put := func(id pgtype.UUID, name string, headline, institution pgtype.Text, photo pgtype.UUID) string {
		s := id.String()
		people[s] = domain.PersonHit{
			UserID: s, FullName: name, Headline: textPtr(headline), Institution: textPtr(institution), PhotoFileID: uuidPtr(photo),
		}
		return s
	}

	strongRows, err := q.StrongNameMatches(ctx, sqlcgen.StrongNameMatchesParams{
		Q: f.q, PrefixOk: utf8.RuneCountInString(f.q) >= MinStrongPrefix, PrefixPattern: likeEscaper.Replace(f.q) + "%",
		ViewerID: f.viewer, Institution: f.institution, Interests: f.interests, Lim: depth,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list strong name matches: %w", err)
	}
	strong := make([]string, len(strongRows))
	for i, r := range strongRows {
		strong[i] = put(r.UserID, r.FullName, r.Headline, r.Institution, r.PhotoFileID)
	}

	kwRows, err := q.SearchPeoplePage(ctx, sqlcgen.SearchPeoplePageParams{
		ViewerID: f.viewer, Q: f.q, PageOffset: 0, PageSize: depth,
		Fuzzy: f.fuzzy, Pattern: f.pattern, Institution: f.institution, Interests: f.interests,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list keyword candidates: %w", err)
	}
	keyword := make([]domain.KeywordCandidate, len(kwRows))
	for i, r := range kwRows {
		keyword[i] = domain.KeywordCandidate{
			UserID: put(r.UserID, r.FullName, r.Headline, r.Institution, r.PhotoFileID), MatchedOn: matchField(r.Rank),
		}
	}

	semRows, err := q.SemanticPeopleCandidates(ctx, sqlcgen.SemanticPeopleCandidatesParams{
		Embedding: vectorText(v.Values), Model: v.Model,
		ViewerID: f.viewer, Institution: f.institution, Interests: f.interests, Lim: depth,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("list meaning candidates: %w", err)
	}
	meaning := make([]string, len(semRows))
	for i, r := range semRows {
		meaning[i] = put(r.UserID, r.FullName, r.Headline, r.Institution, r.PhotoFileID)
	}
	return domain.FusePeople(strong, keyword, meaning), people, nil
}

// addResultInterests fills up to resultInterests interests of each hit: those matching the
// text or a selected interest first, then by name.
func addResultInterests(ctx context.Context, q *sqlcgen.Queries, f filters, hits []domain.PersonHit) error {
	if len(hits) == 0 {
		return nil
	}
	ids := make([]pgtype.UUID, len(hits))
	at := make(map[string]int, len(hits))
	for i, h := range hits {
		if err := ids[i].Scan(h.UserID); err != nil {
			return fmt.Errorf("result user id: %w", err)
		}
		at[h.UserID] = i
	}
	tags, err := q.ListSearchResultInterests(ctx, sqlcgen.ListSearchResultInterestsParams{
		Pattern: f.pattern, Interests: f.interests, UserIds: ids,
	})
	if err != nil {
		return fmt.Errorf("list result interests: %w", err)
	}
	for _, t := range tags {
		if i, ok := at[t.UserID.String()]; ok && len(hits[i].Interests) < resultInterests {
			hits[i].Interests = append(hits[i].Interests, t.Name)
		}
	}
	return nil
}

func uuids(fs []domain.Fused) ([]pgtype.UUID, error) {
	out := make([]pgtype.UUID, len(fs))
	for i, f := range fs {
		if err := out[i].Scan(f.UserID); err != nil {
			return nil, fmt.Errorf("result user id: %w", err)
		}
	}
	return out, nil
}

// vectorText is the pgvector text form of v: [x1,x2,...]. It is passed as a parameter and
// cast in SQL, never joined into a query.
func vectorText(v []float32) string {
	var b strings.Builder
	b.Grow(len(v) * 12)
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
