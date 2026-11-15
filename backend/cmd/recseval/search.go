package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/aiclient"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	profilepg "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/adapter/postgres"
	profiledomain "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/domain"
	profileusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/profile/usecase"
)

// searchQuery is one AI-5.2 evaluation query: plain language for one sample cluster that
// shares no word with that cluster's vocabulary (TestSearchQueriesShareNoWords).
type searchQuery struct {
	Cluster string `json:"cluster"`
	Lang    string `json:"lang"` // en or bn
	Text    string `json:"text"`
}

//go:embed search_queries.json
var searchQueriesJSON []byte

func loadSearchQueries() ([]searchQuery, error) {
	var qs []searchQuery
	if err := json.Unmarshal(searchQueriesJSON, &qs); err != nil {
		return nil, fmt.Errorf("search_queries.json: %w", err)
	}
	return qs, nil
}

// newAI is the AI client the API would use with these settings.
func newAI(getenv func(string) string) (*aiclient.Client, error) {
	token := strings.TrimSpace(getenv("AI_SERVICE_TOKEN"))
	if token == "" {
		return nil, errors.New("AI_SERVICE_TOKEN is not set: the search measurements embed their queries with the AI service")
	}
	base := strings.TrimRight(strings.TrimSpace(getenv("AI_SERVICE_URL")), "/")
	if base == "" {
		base = config.DefaultAIServiceURL
	}
	return aiclient.New(aiclient.Config{BaseURL: base, Token: token}), nil
}

// embedQuery embeds text as the API does: normalized, kind query.
func embedQuery(ctx context.Context, ai aiclient.Embedder, text string) (profiledomain.QueryVector, error) {
	res, err := ai.Embed(ctx, aiclient.KindQuery, []string{profileusecase.NormalizeQueryText(text)})
	if err != nil {
		return profiledomain.QueryVector{}, fmt.Errorf("embed %q: %w", text, err)
	}
	return profiledomain.QueryVector{Model: res.Model, Values: res.Vectors[0]}, nil
}

// testSplitJoins restrict each candidate query to sample profiles of the TEST split: the
// query's own text (profilepg.QueryText) with one join added after an anchor line that
// must appear exactly once, so a change to the product query changes the evaluation too,
// or stops it.
var testSplitJoins = map[string]struct{ anchor, join string }{
	"AnyVisibleNameContains":   {"JOIN users u ON u.id = p.user_id", "JOIN sample_profiles sp ON sp.user_id = p.user_id AND sp.split = 'test'"},
	"StrongNameMatches":        {"JOIN users u ON u.id = p.user_id", "JOIN sample_profiles sp ON sp.user_id = p.user_id AND sp.split = 'test'"},
	"SearchPeoplePage":         {"JOIN profiles p ON p.user_id = b.user_id", "JOIN sample_profiles sp ON sp.user_id = b.user_id AND sp.split = 'test'"},
	"SemanticPeopleCandidates": {"JOIN profiles p ON p.user_id = e.user_id", "JOIN sample_profiles sp ON sp.user_id = e.user_id AND sp.split = 'test'"},
}

var (
	nargRE          = regexp.MustCompile(`sqlc\.narg\((\w+)\)`)
	vectorLiteralRE = regexp.MustCompile(`'\[[-0-9.e,]+\]'`)
)

// productSQL is the named query with sqlc.narg(x) as @x, for pgx.NamedArgs.
func productSQL(name string) (string, error) {
	q, err := profilepg.QueryText(name)
	if err != nil {
		return "", err
	}
	return nargRE.ReplaceAllString(q, "@$1"), nil
}

// testSplitSQL is productSQL with the TEST split join.
func testSplitSQL(name string) (string, error) {
	q, err := productSQL(name)
	if err != nil {
		return "", err
	}
	j, ok := testSplitJoins[name]
	if !ok {
		return "", fmt.Errorf("no test split join for %s", name)
	}
	if n := strings.Count(q, j.anchor); n != 1 {
		return "", fmt.Errorf("%s: anchor %q found %d times, want 1: update testSplitJoins", name, j.anchor, n)
	}
	return strings.Replace(q, j.anchor, j.anchor+"\n"+j.join, 1), nil
}

// sides are the three candidate lists of one hybrid search, best first.
type sides struct {
	strong, meaning []string
	keyword         []profiledomain.KeywordCandidate
}

// testSplitCandidates runs the three candidate queries of the hybrid search for text and v,
// with no filters and a viewer that blocks nobody, among TEST split sample profiles, under
// the endpoint's transaction settings.
func testSplitCandidates(ctx context.Context, pool *pgxpool.Pool, text string, v profiledomain.QueryVector) (sides, error) {
	var out sides
	err := pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		for _, setup := range []string{"SetWordSimilarityThreshold", "SetHNSWIterativeScan", "SetHNSWEfSearch"} {
			q, err := productSQL(setup)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, q, pgx.NamedArgs{"threshold": "0.4", "ef_search": fmt.Sprint(profilepg.HybridEfSearch)}); err != nil {
				return fmt.Errorf("%s: %w", setup, err)
			}
		}
		pattern := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text) + "%"
		args := pgx.NamedArgs{
			"q": text, "pattern": pattern, "viewer_id": nil, "institution": nil, "interests": []string{},
			"lim": profiledomain.HybridDepth, "page_size": profiledomain.HybridDepth, "page_offset": 0,
			"prefix_ok":      len([]rune(text)) >= profilepg.MinStrongPrefix,
			"prefix_pattern": strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(text) + "%",
			"embedding":      vectorText(v.Values), "model": v.Model,
		}
		q, err := testSplitSQL("AnyVisibleNameContains")
		if err != nil {
			return err
		}
		var literal bool
		if err := tx.QueryRow(ctx, q, args).Scan(&literal); err != nil {
			return fmt.Errorf("AnyVisibleNameContains: %w", err)
		}
		args["fuzzy"] = !literal
		ids := func(name string) ([]string, error) {
			q, err := testSplitSQL(name)
			if err != nil {
				return nil, err
			}
			rows, err := tx.Query(ctx, q, args)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
			return pgx.CollectRows(rows, func(r pgx.CollectableRow) (string, error) {
				vals, err := r.Values()
				if err != nil {
					return "", err
				}
				return fmt.Sprintf("%x", vals[0]), nil
			})
		}
		if out.strong, err = ids("StrongNameMatches"); err != nil {
			return err
		}
		kw, err := ids("SearchPeoplePage")
		if err != nil {
			return err
		}
		for _, id := range kw {
			out.keyword = append(out.keyword, profiledomain.KeywordCandidate{UserID: id, MatchedOn: profiledomain.MatchName})
		}
		out.meaning, err = ids("SemanticPeopleCandidates")
		return err
	})
	return out, err
}

// uuidHex is a sample's ID in the form testSplitCandidates returns: 32 hex digits.
func uuidHex(id string) string { return strings.ReplaceAll(id, "-", "") }

// evaluateSearch measures AI-5.2: precision@10 of each evaluation query, relevant = a
// TEST split sample profile whose main cluster is the query's, hybrid against keyword
// (and the meaning side alone, for reference). Nothing is fitted.
func evaluateSearch(ctx context.Context, pool *pgxpool.Pool, s samples, ai aiclient.Embedder, out io.Writer) error {
	qs, err := loadSearchQueries()
	if err != nil {
		return err
	}
	cluster := map[string]string{} // hex ID -> main cluster, TEST split only
	for _, p := range s.ids("test") {
		cluster[uuidHex(p.id)] = p.cluster
	}
	p10 := func(ids []string, want string) (float64, int) {
		ids = ids[:min(10, len(ids))]
		hits := 0
		for _, id := range ids {
			if cluster[id] == want {
				hits++
			}
		}
		return float64(hits) / 10, len(ids)
	}
	type sums struct{ hybrid, keyword, meaning float64 }
	byLang := map[string]*sums{"en": {}, "bn": {}}
	var all sums
	fmt.Fprintf(out, "\nAI-5.2 evaluation: %d queries, candidates = the %d TEST split sample profiles, relevant = same main cluster.\n\n",
		len(qs), len(cluster))
	fmt.Fprintln(out, "| Cluster | Lang | Query | Hybrid P@10 | Keyword P@10 (results) | Meaning only P@10 |")
	fmt.Fprintln(out, "| --- | --- | --- | ---: | ---: | ---: |")
	for _, q := range qs {
		v, err := embedQuery(ctx, ai, q.Text)
		if err != nil {
			return err
		}
		c, err := testSplitCandidates(ctx, pool, q.Text, v)
		if err != nil {
			return fmt.Errorf("query %q: %w", q.Text, err)
		}
		fused := profiledomain.FusePeople(c.strong, c.keyword, c.meaning)
		hybridIDs := make([]string, len(fused))
		for i, f := range fused {
			hybridIDs[i] = f.UserID
		}
		kwIDs := make([]string, len(c.keyword))
		for i, k := range c.keyword {
			kwIDs[i] = k.UserID
		}
		h, _ := p10(hybridIDs, q.Cluster)
		k, kn := p10(kwIDs, q.Cluster)
		m, _ := p10(c.meaning, q.Cluster)
		fmt.Fprintf(out, "| %s | %s | %s | %.1f | %.1f (%d) | %.1f |\n", q.Cluster, q.Lang, q.Text, h, k, kn, m)
		for _, acc := range []*sums{&all, byLang[q.Lang]} {
			acc.hybrid += h
			acc.keyword += k
			acc.meaning += m
		}
	}
	n := float64(len(qs))
	fmt.Fprintf(out, "\n| Queries | Hybrid P@10 | Keyword P@10 | Meaning only P@10 |\n| --- | ---: | ---: | ---: |\n")
	for _, lang := range []string{"en", "bn"} {
		l := byLang[lang]
		fmt.Fprintf(out, "| %s (%d) | %.3f | %.3f | %.3f |\n", lang, len(qs)/2, l.hybrid/(n/2), l.keyword/(n/2), l.meaning/(n/2))
	}
	fmt.Fprintf(out, "| all (%d) | %.3f | %.3f | %.3f |\n", len(qs), all.hybrid/n, all.keyword/n, all.meaning/n)
	return nil
}

// benchSearch measures NFR-1 for hybrid search: the usecase's whole work (query vector,
// the three candidate queries, fusion, facets, page and result interests) against this
// database, for o.queries texts, first with an empty vector cache (every text embedded by
// the AI service) and then again with every vector kept. HTTP, the session check, the rate
// limit, connection statuses and Redis are not in it.
func benchSearch(ctx context.Context, pool *pgxpool.Pool, s samples, ai aiclient.Embedder, o options, out io.Writer) error {
	texts, err := benchTexts(ctx, pool, o)
	if err != nil {
		return err
	}
	cache := &memoryVectors{kept: map[string][]float32{}}
	svc := profileusecase.New(profilepg.NewStore(pool), profileusecase.WithPeopleSearch(profileusecase.PeopleSearch{
		Searcher: profilepg.NewStore(pool), Limiter: noLimit{}, Embedder: aiEmbedder{ai: ai}, Cache: cache,
		Logger: slog.New(slog.DiscardHandler),
	}))
	viewers := draw(s.all, len(texts), o.seed, 4)
	// Warm-up: other texts, so the measured cold run still embeds each of its texts.
	for i := range benchWarmup {
		if _, err := svc.SearchPeople(ctx, viewers[i].id, profileusecase.SearchInput{Q: ptr(fmt.Sprintf("warm up %d", i)), Page: 1}); err != nil {
			return err
		}
	}
	measure := func(label string) ([]time.Duration, error) {
		times := make([]time.Duration, 0, len(texts))
		for i, text := range texts {
			start := time.Now()
			res, err := svc.SearchPeople(ctx, viewers[i].id, profileusecase.SearchInput{Q: ptr(text), Page: 1})
			took := time.Since(start)
			if err != nil {
				return nil, fmt.Errorf("%s search %q: %w", label, text, err)
			}
			if res.Mode != profiledomain.ResultHybrid {
				return nil, fmt.Errorf("%s search %q: mode %s, want hybrid (is the AI service up?)", label, text, res.Mode)
			}
			times = append(times, took)
		}
		slices.Sort(times)
		return times, nil
	}
	before := cache.sets
	cold, err := measure("cold")
	if err != nil {
		return err
	}
	embeds := cache.sets - before
	warm, err := measure("cached")
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nBenchmark: %d search texts (the %d evaluation queries and sample interest names, seed %d), viewers drawn at random, after %d warm-up searches. Cold: %d texts embedded by the AI service. Cached: the same texts again, every vector kept (%d new embeddings).\n",
		len(texts), min(len(texts), 40), o.seed, benchWarmup, embeds, cache.sets-before-embeds)
	fmt.Fprintf(out, "\n| Measure | Cold query vector | Cached query vector |\n| --- | ---: | ---: |\n")
	fmt.Fprintf(out, "| Searches | %d | %d |\n", len(cold), len(warm))
	fmt.Fprintf(out, "| p50 | %s | %s |\n", percentile(cold, 50), percentile(warm, 50))
	fmt.Fprintf(out, "| p95 | %s | %s |\n", percentile(cold, 95), percentile(warm, 95))
	fmt.Fprintf(out, "| max | %s | %s |\n", cold[len(cold)-1].Round(10*time.Microsecond), warm[len(warm)-1].Round(10*time.Microsecond))
	fmt.Fprintf(out, "\nNFR-1 target: p95 under 2 s, cold. ")
	if percentile(cold, 95) < 2*time.Second {
		fmt.Fprintln(out, "Met.")
	} else {
		fmt.Fprintln(out, "NOT met.")
	}

	v, err := embedQuery(ctx, ai, texts[0])
	if err != nil {
		return err
	}
	plan, err := explainSemantic(ctx, pool, v, viewers[0].id)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nEXPLAIN (ANALYZE, BUFFERS) of the meaning query (SemanticPeopleCandidates, no filters) for %q:\n\n%s\n", texts[0], plan)
	return nil
}

// benchTexts are the evaluation queries, then names of research interests sample profiles
// hold, drawn with the seed, up to o.queries.
func benchTexts(ctx context.Context, pool *pgxpool.Pool, o options) ([]string, error) {
	qs, err := loadSearchQueries()
	if err != nil {
		return nil, err
	}
	texts := make([]string, 0, o.queries)
	for _, q := range qs {
		if len(texts) < o.queries {
			texts = append(texts, q.Text)
		}
	}
	rows, err := pool.Query(ctx, `SELECT DISTINCT t.name::text FROM research_interests t
		JOIN user_interests l ON l.interest_id = t.id
		JOIN sample_profiles sp ON sp.user_id = l.user_id
		WHERE char_length(t.name::text) >= 2
		ORDER BY 1`)
	if err != nil {
		return nil, fmt.Errorf("read interest names: %w", err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	pool2 := make([]sample, len(names))
	for i, n := range names {
		pool2[i] = sample{id: n}
	}
	for _, p := range draw(pool2, o.queries-len(texts), o.seed, 5) {
		texts = append(texts, p.id)
	}
	return texts, nil
}

// explainSemantic runs the product's meaning query under EXPLAIN ANALYZE with the search's
// transaction settings.
func explainSemantic(ctx context.Context, pool *pgxpool.Pool, v profiledomain.QueryVector, viewerID string) (string, error) {
	var lines []string
	err := pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		for _, setup := range []string{"SetHNSWIterativeScan", "SetHNSWEfSearch"} {
			q, err := productSQL(setup)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, q, pgx.NamedArgs{"ef_search": fmt.Sprint(profilepg.HybridEfSearch)}); err != nil {
				return err
			}
		}
		q, err := productSQL("SemanticPeopleCandidates")
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+q, pgx.NamedArgs{
			"embedding": vectorText(v.Values), "model": v.Model, "viewer_id": viewerID, "institution": nil,
			"interests": []string{}, "lim": profiledomain.HybridDepth,
		})
		if err != nil {
			return err
		}
		lines, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return "", fmt.Errorf("explain the meaning query: %w", err)
	}
	for i, l := range lines {
		lines[i] = vectorLiteralRE.ReplaceAllString(l, "'[768 numbers]'")
	}
	return joinLines(lines), nil
}

// vectorText is the pgvector text form of v.
func vectorText(v []float32) string {
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = fmt.Sprint(x)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func ptr(s string) *string { return &s }

// aiEmbedder is cmd/api's query embedder: kind query, one text.
type aiEmbedder struct{ ai aiclient.Embedder }

func (e aiEmbedder) EmbedQuery(ctx context.Context, text string) (profiledomain.QueryVector, error) {
	res, err := e.ai.Embed(ctx, aiclient.KindQuery, []string{text})
	if err != nil {
		return profiledomain.QueryVector{}, err
	}
	return profiledomain.QueryVector{Model: res.Model, Values: res.Vectors[0]}, nil
}

// memoryVectors is a query vector cache in memory, for the benchmark.
type memoryVectors struct {
	mu   sync.Mutex
	kept map[string][]float32
	sets int
}

func (m *memoryVectors) Get(_ context.Context, model, text string) ([]float32, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.kept[model+"\x00"+text]
	return v, ok, nil
}

func (m *memoryVectors) Set(_ context.Context, text string, v profiledomain.QueryVector, _ time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.kept[v.Model+"\x00"+text] = v.Values
	m.sets++
	return nil
}

// noLimit never limits the benchmark's searches.
type noLimit struct{}

func (noLimit) Used(context.Context, string) (int, time.Duration, error) { return 0, 0, nil }
func (noLimit) Record(context.Context, string, time.Duration) error      { return nil }
