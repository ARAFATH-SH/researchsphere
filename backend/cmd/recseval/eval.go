package main

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	recommendationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/usecase"
)

// evalK is the list length NFR-6 measures: the 10 recommendations the endpoint shows.
const evalK = recommendationusecase.MaxLimit

// queryMetrics are one query's measures at k (docs/evaluation.md):
//
//	precision@k      = hits / k
//	capped recall@k  = hits / min(k, |R|)
//	standard recall@k = hits / |R|
//
// where hits is how many of the top k are relevant and |R| how many candidates are relevant
// in all. A list shorter than k still divides by k. ok is false when nothing is relevant.
func queryMetrics(hits, relevant, k int) (precision, capped, standard float64, ok bool) {
	if relevant <= 0 || k <= 0 {
		return 0, 0, 0, false
	}
	h := float64(hits)
	return h / float64(k), h / float64(min(k, relevant)), h / float64(relevant), true
}

// evalResult is the mean over the queries of one relevance rule.
type evalResult struct {
	queries                     int
	skipped                     int // no relevant candidate
	precision, capped, standard float64
	meanRelevant                float64
}

func (r *evalResult) add(hits, relevant int) {
	p, c, s, ok := queryMetrics(hits, relevant, evalK)
	if !ok {
		r.skipped++
		return
	}
	r.queries++
	r.precision += p
	r.capped += c
	r.standard += s
	r.meanRelevant += float64(relevant)
}

func (r *evalResult) finish() {
	if r.queries == 0 {
		return
	}
	q := float64(r.queries)
	r.precision /= q
	r.capped /= q
	r.standard /= q
	r.meanRelevant /= q
}

// randomPairsPerQuery is how many random candidates each test query is paired with for
// the score distribution over all pairs (not only the top 10).
const randomPairsPerQuery = 200

// scoreGroups collects the scores of each group of pairs.
type scoreGroups map[string][]int

// evaluate measures NFR-6 on the held-out TEST split: each test profile is a query, the
// candidates are every other embedded sample profile of both splits, ranked as the endpoint
// ranks (rankSamplesSQL). It reports two relevance rules: the NFR-6 one, same main cluster,
// and a looser one, a shared research cluster (related: main or second). It also reports the
// score distribution of top-10 items and of random pairs, per group. Nothing is fitted here.
func evaluate(ctx context.Context, pool *pgxpool.Pool, s samples, scores recommendationusecase.ScoreMap, seed uint64, out io.Writer) error {
	test := s.ids("test")
	fmt.Fprintf(out, "Evaluating %d test queries against %d candidates, k = %d (score map fitted for %s).\n",
		len(test), len(s.all)-1, evalK, scores.Model)
	started := time.Now()
	var mainRes, relRes evalResult
	misses, relatedMisses := 0, 0
	groups := scoreGroups{}
	r := rand.New(rand.NewPCG(seed, 4)) //nolint:gosec // reproducible draws of sample data, not a secret
	for i, q := range test {
		top, err := rankSamples(ctx, pool, q.id, evalK, "")
		if err != nil {
			return err
		}
		mainHits, relHits := 0, 0
		for _, n := range top {
			c := s.byID[n.id]
			sc := scores.Score(n.cosine)
			switch {
			case c.cluster == q.cluster:
				mainHits++
				relHits++
				groups["top10 same"] = append(groups["top10 same"], sc)
			case related(q, c):
				relHits++
				misses++
				relatedMisses++
				groups["top10 other"] = append(groups["top10 other"], sc)
				groups["top10 other, related"] = append(groups["top10 other, related"], sc)
			default:
				misses++
				groups["top10 other"] = append(groups["top10 other"], sc)
				groups["top10 other, unrelated"] = append(groups["top10 other, unrelated"], sc)
			}
		}
		relevantRelated := 0
		for _, c := range s.all {
			if c.id != q.id && related(q, c) {
				relevantRelated++
			}
		}
		mainRes.add(mainHits, s.clusterSize[q.cluster]-1)
		relRes.add(relHits, relevantRelated)

		others := make([]string, 0, randomPairsPerQuery)
		for len(others) < randomPairsPerQuery {
			if c := s.all[r.IntN(len(s.all))]; c.id != q.id {
				others = append(others, c.id)
			}
		}
		cs, err := cosines(ctx, pool, q.id, others)
		if err != nil {
			return err
		}
		for _, id := range others {
			key := "random other"
			if s.byID[id].cluster == q.cluster {
				key = "random same"
			}
			groups[key] = append(groups[key], scores.Score(cs[id]))
		}
		if (i+1)%500 == 0 {
			fmt.Fprintf(out, "  %d of %d queries (%s)\n", i+1, len(test), time.Since(started).Round(time.Second))
		}
	}
	mainRes.finish()
	relRes.finish()
	fmt.Fprintf(out, "Done in %s.\n\n", time.Since(started).Round(time.Second))

	fmt.Fprintf(out, "| Metric | Same main cluster (NFR-6) | Shared main or second cluster |\n| --- | ---: | ---: |\n")
	fmt.Fprintf(out, "| Queries (test split) | %d | %d |\n", mainRes.queries, relRes.queries)
	fmt.Fprintf(out, "| Mean relevant candidates per query, \\|R\\| | %.1f | %.1f |\n", mainRes.meanRelevant, relRes.meanRelevant)
	fmt.Fprintf(out, "| Recall@10 (capped: hits / min(10, \\|R\\|)) | %.4f | %.4f |\n", mainRes.capped, relRes.capped)
	fmt.Fprintf(out, "| Precision@10 (hits / 10) | %.4f | %.4f |\n", mainRes.precision, relRes.precision)
	fmt.Fprintf(out, "| Standard recall@10 (hits / \\|R\\|) | %.4f | %.4f |\n", mainRes.standard, relRes.standard)
	if mainRes.skipped+relRes.skipped > 0 {
		fmt.Fprintf(out, "\nQueries without a relevant candidate, left out: %d and %d.\n", mainRes.skipped, relRes.skipped)
	}
	share := 0.0
	if misses > 0 {
		share = 100 * float64(relatedMisses) / float64(misses)
	}
	fmt.Fprintf(out, "\nTop-10 items outside the query's main cluster: %d of %d; %d of them (%.1f%%) share the main or second cluster.\n\n",
		misses, len(test)*evalK, relatedMisses, share)

	fmt.Fprintf(out, "| Score of | Pairs | Mean | p10 | p25 | p50 | p75 | p90 |\n| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, g := range []string{"random same", "random other", "top10 same", "top10 other", "top10 other, related", "top10 other, unrelated"} {
		fmt.Fprintln(out, scoreRow(g, groups[g]))
	}
	return nil
}

// scoreRow is one line of the score distribution table: the count, the mean and the
// nearest-rank percentiles of the scores.
func scoreRow(name string, scores []int) string {
	if len(scores) == 0 {
		return fmt.Sprintf("| %s | 0 | | | | | | |", name)
	}
	sorted := slices.Clone(scores)
	slices.Sort(sorted)
	sum := 0
	for _, v := range sorted {
		sum += v
	}
	pct := func(p int) int {
		i := (p*len(sorted)+99)/100 - 1
		return sorted[min(max(i, 0), len(sorted)-1)]
	}
	return fmt.Sprintf("| %s | %d | %.1f | %d | %d | %d | %d | %d |", name, len(sorted),
		float64(sum)/float64(len(sorted)), pct(10), pct(25), pct(50), pct(75), pct(90))
}
