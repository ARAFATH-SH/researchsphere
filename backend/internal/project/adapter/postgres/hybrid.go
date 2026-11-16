package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/adapter/postgres/sqlcgen"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/project/domain"
)

// Hybrid search settings (AI-5.1 for projects), as researcher search's.
const (
	// HybridEfSearch is the HNSW candidate list size (hnsw.ef_search); at least
	// domain.HybridDepth.
	HybridEfSearch = 100
	// MinStrongPrefix is the shortest text whose prefix matches count as strong title
	// matches.
	MinStrongPrefix = 3
)

// SearchHybrid runs the strong title matches, the keyword search and the search by meaning
// in one read-only snapshot and merges them with domain.FuseProjects. total is the merged
// list's size; fuzzy and correctedQuery come from the keyword side.
func (s *Store) SearchHybrid(ctx context.Context, sq domain.SearchQuery, v domain.QueryVector) (domain.SearchResult, error) {
	var res domain.SearchResult
	err := s.searchTx(ctx, func(t pgx.Tx) error {
		r, err := searchHybrid(ctx, sqlcgen.New(t), sq, v)
		res = r
		return err
	})
	if err != nil {
		return domain.SearchResult{}, fmt.Errorf("hybrid search projects: %w", err)
	}
	return res, nil
}

func searchHybrid(ctx context.Context, q *sqlcgen.Queries, sq domain.SearchQuery, v domain.QueryVector) (domain.SearchResult, error) {
	if sq.Limit < 1 || sq.Offset < 0 {
		return domain.SearchResult{}, fmt.Errorf("page %d at %d out of range", sq.Limit, sq.Offset)
	}
	if err := q.SetWordSimilarityThreshold(ctx, strconv.FormatFloat(wordSimilarityThreshold, 'f', -1, 64)); err != nil {
		return domain.SearchResult{}, fmt.Errorf("set word similarity threshold: %w", err)
	}
	if err := q.SetHNSWIterativeScan(ctx); err != nil {
		return domain.SearchResult{}, fmt.Errorf("set hnsw iterative scan: %w", err)
	}
	if err := q.SetHNSWEfSearch(ctx, strconv.Itoa(HybridEfSearch)); err != nil {
		return domain.SearchResult{}, fmt.Errorf("set hnsw ef_search: %w", err)
	}
	pattern := containsPattern(sq.Q)
	viewer := viewerUUID(sq.ViewerID)
	depth := int32(domain.HybridDepth)
	cards := map[string]domain.Summary{}
	put := func(id pgtype.UUID, title, desc, status, myRole string, created, updated pgtype.Timestamptz,
		owner pgtype.UUID, ownerName string, ownerHeadline pgtype.Text, ownerPhoto pgtype.UUID) string {
		key := id.String()
		cards[key] = domain.Summary{
			ID: key, Title: title, Description: desc, Status: domain.Status(status), MyRole: domain.Role(myRole),
			Owner:     domain.Person{UserID: owner.String(), FullName: ownerName, Headline: textPtr(ownerHeadline), PhotoFileID: uuidPtr(ownerPhoto)},
			CreatedAt: created.Time, UpdatedAt: updated.Time,
		}
		return key
	}

	strongRows, err := q.StrongTitleMatches(ctx, sqlcgen.StrongTitleMatchesParams{
		ViewerID: viewer, Q: sq.Q, PrefixOk: utf8.RuneCountInString(sq.Q) >= MinStrongPrefix,
		PrefixPattern: likeEscaper.Replace(sq.Q) + "%", Lim: depth,
	})
	if err != nil {
		return domain.SearchResult{}, fmt.Errorf("list strong title matches: %w", err)
	}
	strong := make([]string, len(strongRows))
	for i, r := range strongRows {
		strong[i] = put(r.ID, r.Title, r.Description, r.Status, r.MyRole, r.CreatedAt, r.UpdatedAt, r.OwnerID, r.OwnerName, r.OwnerHeadline, r.OwnerPhoto)
	}

	literal, err := q.AnyVisibleTitleContains(ctx, sqlcgen.AnyVisibleTitleContainsParams{Pattern: pattern, ViewerID: viewer})
	if err != nil {
		return domain.SearchResult{}, fmt.Errorf("check title matches: %w", err)
	}
	sum, err := q.SearchProjectsSummary(ctx, sqlcgen.SearchProjectsSummaryParams{ViewerID: viewer, Q: sq.Q, Fuzzy: !literal, Pattern: pattern})
	if err != nil {
		return domain.SearchResult{}, fmt.Errorf("count keyword results: %w", err)
	}
	kwRows, err := q.SearchProjectsPage(ctx, sqlcgen.SearchProjectsPageParams{
		ViewerID: viewer, Q: sq.Q, Fuzzy: !literal, Pattern: pattern, Lim: depth, Off: 0,
	})
	if err != nil {
		return domain.SearchResult{}, fmt.Errorf("list keyword candidates: %w", err)
	}
	keyword := make([]domain.KeywordCandidate, len(kwRows))
	for i, r := range kwRows {
		keyword[i] = domain.KeywordCandidate{
			ID:        put(r.ID, r.Title, r.Description, r.Status, r.MyRole, r.CreatedAt, r.UpdatedAt, r.OwnerID, r.OwnerName, r.OwnerHeadline, r.OwnerPhoto),
			MatchedOn: matchedOn(r.Rank),
		}
	}

	semRows, err := q.SemanticProjectCandidates(ctx, sqlcgen.SemanticProjectCandidatesParams{
		Embedding: vectorText(v.Values), Model: v.Model, ViewerID: viewer, Lim: depth,
	})
	if err != nil {
		return domain.SearchResult{}, fmt.Errorf("list meaning candidates: %w", err)
	}
	meaning := make([]string, len(semRows))
	for i, r := range semRows {
		meaning[i] = put(r.ID, r.Title, r.Description, r.Status, r.MyRole, r.CreatedAt, r.UpdatedAt, r.OwnerID, r.OwnerName, r.OwnerHeadline, r.OwnerPhoto)
	}

	merged := domain.FuseProjects(strong, keyword, meaning)
	res := domain.SearchResult{Total: len(merged), Items: []domain.Summary{}}
	if sum.CorrectedQuery != "" {
		res.Fuzzy = true
		res.CorrectedQuery = &sum.CorrectedQuery
	}
	start := min(sq.Offset, len(merged))
	end := min(start+sq.Limit, len(merged))
	ids := make([]pgtype.UUID, 0, end-start)
	for _, m := range merged[start:end] {
		c := cards[m.ID]
		c.MatchedOn = m.MatchedOn
		res.Items = append(res.Items, c)
		var u pgtype.UUID
		if err := u.Scan(m.ID); err != nil {
			return domain.SearchResult{}, fmt.Errorf("result project id: %w", err)
		}
		ids = append(ids, u)
	}
	if err := addSkillNames(ctx, q, ids, res.Items); err != nil {
		return domain.SearchResult{}, err
	}
	return res, nil
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
