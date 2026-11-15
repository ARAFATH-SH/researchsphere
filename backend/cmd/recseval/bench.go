package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
	recommendationpg "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/postgres/sqlcgen"
	recommendationdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/domain"
	recommendationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/usecase"
	socialpg "github.com/ARAFATH-SH/researchsphere/backend/internal/social/adapter/postgres"
	socialusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/social/usecase"
)

const benchWarmup = 10

// bench measures NFR-1: the endpoint's work with nothing cached (the caller's fields and
// stamp, the ranking, the tags and the connection statuses), as the recommendation usecase
// does it against this database, for o.queries random sample users. HTTP, the session check
// and Redis are not in it; they add a few milliseconds.
func bench(ctx context.Context, pool *pgxpool.Pool, s samples, scores recommendationusecase.ScoreMap, o options, out io.Writer) error {
	logger := slog.New(slog.DiscardHandler)
	social := socialusecase.New(socialpg.NewStore(pool), postgres.NewTransactor(pool), nil, nil, nil, logger)
	svc := recommendationusecase.New(recommendationpg.NewStore(pool), noCache{}, relations{social: social}, scores, logger)

	users := draw(s.all, benchWarmup+o.queries, o.seed, 3)
	fmt.Fprintf(out, "Benchmark: %d random sample users (seed %d) after %d warm-up calls, nothing cached.\n",
		len(users)-benchWarmup, o.seed, benchWarmup)
	times := make([]time.Duration, 0, len(users))
	for i, u := range users {
		start := time.Now()
		res, err := svc.People(ctx, u.id, recommendationusecase.MaxLimit)
		took := time.Since(start)
		if err != nil {
			return fmt.Errorf("recommendations for %s: %w", short(u.id), err)
		}
		if res.State == recommendationdomain.StateReady && len(res.Items) == 0 {
			fmt.Fprintf(out, "  note: %s got no recommendations\n", short(u.id))
		}
		if i >= benchWarmup {
			times = append(times, took)
		}
	}
	slices.Sort(times)
	var total time.Duration
	for _, t := range times {
		total += t
	}
	fmt.Fprintf(out, "\n| Measure | Time |\n| --- | ---: |\n")
	fmt.Fprintf(out, "| Calls | %d |\n", len(times))
	fmt.Fprintf(out, "| p50 | %s |\n", percentile(times, 50))
	fmt.Fprintf(out, "| p95 | %s |\n", percentile(times, 95))
	fmt.Fprintf(out, "| max | %s |\n", times[len(times)-1].Round(10*time.Microsecond))
	fmt.Fprintf(out, "| mean | %s |\n", (total / time.Duration(len(times))).Round(10*time.Microsecond))
	fmt.Fprintf(out, "\nNFR-1 target: p95 under 2 s. ")
	if percentile(times, 95) < 2*time.Second {
		fmt.Fprintln(out, "Met.")
	} else {
		fmt.Fprintln(out, "NOT met.")
	}

	plan, err := explainRank(ctx, pool, users[benchWarmup].id)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nEXPLAIN (ANALYZE, BUFFERS) of the ranking query for user %s:\n\n%s\n", short(users[benchWarmup].id), plan)
	return nil
}

// percentile is the nearest-rank percentile of sorted times.
func percentile(sorted []time.Duration, p int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	i := (p*len(sorted)+99)/100 - 1
	return sorted[min(max(i, 0), len(sorted)-1)].Round(10 * time.Microsecond)
}

// explainRank runs the endpoint's own ranking query text under EXPLAIN ANALYZE, in a
// transaction with the endpoint's settings.
func explainRank(ctx context.Context, pool *pgxpool.Pool, userID string) (string, error) {
	q, err := recommendationpg.RankPeopleSQL()
	if err != nil {
		return "", err
	}
	var lines []string
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := recommendationpg.SetupRanking(ctx, sqlcgen.New(tx)); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "EXPLAIN (ANALYZE, BUFFERS) "+q, userID, recommendationusecase.MaxLimit)
		if err != nil {
			return err
		}
		lines, err = pgx.CollectRows(rows, pgx.RowTo[string])
		return err
	})
	if err != nil {
		return "", fmt.Errorf("explain the ranking query: %w", err)
	}
	return joinLines(lines), nil
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}

// noCache never keeps anything, so every call does the whole work.
type noCache struct{}

func (noCache) Get(context.Context, string) (recommendationdomain.Kept, bool, error) {
	return recommendationdomain.Kept{}, false, nil
}

func (noCache) Set(context.Context, string, recommendationdomain.Kept, time.Duration) error {
	return nil
}

func (noCache) Delete(context.Context, ...string) error { return nil }

// relations reads connection statuses from the social module, as cmd/api does.
type relations struct {
	social *socialusecase.Service
}

func (r relations) Statuses(ctx context.Context, viewerID string, ids []string) (map[string]recommendationdomain.ConnectionStatus, error) {
	rs, err := r.social.Relations(ctx, viewerID, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]recommendationdomain.ConnectionStatus, len(rs))
	for id, rel := range rs {
		out[id] = recommendationdomain.ConnectionStatus(rel.Status)
	}
	return out, nil
}
