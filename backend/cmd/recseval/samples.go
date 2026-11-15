package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	recommendationpg "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/postgres"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/adapter/postgres/sqlcgen"
)

// sample is one embedded sample profile and its ground truth (sample_profiles).
type sample struct {
	id      string
	seq     int
	cluster string // main_cluster
	second  string // second_cluster, "" when none
	split   string
}

// related reports whether a and b share a research cluster: the main or the second cluster
// of one is the main or the second cluster of the other. Same main cluster is related.
func related(a, b sample) bool {
	for _, x := range []string{a.cluster, a.second} {
		if x != "" && (x == b.cluster || x == b.second) {
			return true
		}
	}
	return false
}

// samples are the embedded sample profiles, in seq order, all of one model.
type samples struct {
	model       string
	all         []sample
	byID        map[string]sample
	clusterSize map[string]int // embedded sample profiles per main cluster, both splits
}

func (s samples) ids(split string) []sample {
	var out []sample
	for _, p := range s.all {
		if p.split == split {
			out = append(out, p)
		}
	}
	return out
}

// loadSamples reads the labels of every embedded sample profile. Only this command reads
// sample_profiles (AGENTS.md). It refuses while embeddings of two models exist: vectors of
// different models are not comparable.
func loadSamples(ctx context.Context, pool *pgxpool.Pool) (samples, error) {
	rows, err := pool.Query(ctx, `SELECT sp.user_id::text, sp.seq, sp.main_cluster, COALESCE(sp.second_cluster, ''), sp.split, e.model
		FROM sample_profiles sp
		JOIN profile_embeddings e ON e.user_id = sp.user_id
		ORDER BY sp.seq`)
	if err != nil {
		return samples{}, fmt.Errorf("read sample profiles: %w", err)
	}
	s := samples{byID: map[string]sample{}, clusterSize: map[string]int{}}
	models := map[string]int{}
	for rows.Next() {
		var p sample
		var model string
		if err := rows.Scan(&p.id, &p.seq, &p.cluster, &p.second, &p.split, &model); err != nil {
			return samples{}, err
		}
		models[model]++
		s.all = append(s.all, p)
		s.byID[p.id] = p
		s.clusterSize[p.cluster]++
	}
	if err := rows.Err(); err != nil {
		return samples{}, fmt.Errorf("read sample profiles: %w", err)
	}
	if len(s.all) == 0 {
		return samples{}, errors.New("no embedded sample profiles: run make sample-data n=10000 wait=1 first")
	}
	if len(models) != 1 {
		return samples{}, fmt.Errorf("sample embeddings of %d models: wait until the API has re-embedded them all", len(models))
	}
	for m := range models {
		s.model = m
	}
	return s, nil
}

// rankSamplesSQL ranks the sample profiles for one, exactly as the endpoint's RankPeople
// does (internal/recommendation/adapter/postgres/queries.sql): the same HNSW index, cosine
// operator, model, verification and suspension filters and order, under the same
// iterative scan and ef_search (recommendationpg.SetupRanking). The differences: only
// sample profiles are candidates ($3 narrows them to one split, or ” for both), and
// connections and blocks are not left out (NFR-6 measures matching, not the social graph).
const rankSamplesSQL = `
WITH me AS (
    SELECT embedding, model FROM profile_embeddings WHERE user_id = $1::uuid
),
cand AS MATERIALIZED (
    SELECT e.user_id, (e.embedding <=> (SELECT embedding FROM me))::float8 AS distance
    FROM profile_embeddings e
    JOIN users u ON u.id = e.user_id
    JOIN profiles p ON p.user_id = e.user_id
    JOIN sample_profiles sp ON sp.user_id = e.user_id
    WHERE e.user_id <> $1::uuid
      AND e.model = (SELECT model FROM me)
      AND u.email_verified_at IS NOT NULL
      AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until <= now()))
      AND ($3::text = '' OR sp.split = $3::text)
    ORDER BY e.embedding <=> (SELECT embedding FROM me)
    LIMIT $2
)
SELECT cand.user_id::text, (1 - cand.distance)::float8
FROM cand
ORDER BY cand.distance, cand.user_id`

// neighbour is a ranked candidate.
type neighbour struct {
	id     string
	cosine float64
}

// rankSamples returns the k nearest sample profiles to id, from split ("" for both).
func rankSamples(ctx context.Context, pool *pgxpool.Pool, id string, k int, split string) ([]neighbour, error) {
	var out []neighbour
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if err := recommendationpg.SetupRanking(ctx, sqlcgen.New(tx)); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, rankSamplesSQL, id, k, split)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (neighbour, error) {
			var n neighbour
			err := r.Scan(&n.id, &n.cosine)
			return n, err
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("rank samples for %s: %w", id, err)
	}
	return out, nil
}

// cosines returns the cosine similarity between id and each of others.
func cosines(ctx context.Context, pool *pgxpool.Pool, id string, others []string) (map[string]float64, error) {
	rows, err := pool.Query(ctx, `SELECT e.user_id::text, (1 - (e.embedding <=> m.embedding))::float8
		FROM profile_embeddings e, profile_embeddings m
		WHERE m.user_id = $1::uuid AND e.user_id = ANY($2::uuid[])`, id, others)
	if err != nil {
		return nil, fmt.Errorf("pair cosines: %w", err)
	}
	out := map[string]float64{}
	for rows.Next() {
		var other string
		var c float64
		if err := rows.Scan(&other, &c); err != nil {
			return nil, err
		}
		out[other] = c
	}
	return out, rows.Err()
}

// draw returns n of from in a random order fixed by seed and stream.
func draw(from []sample, n int, seed, stream uint64) []sample {
	r := rand.New(rand.NewPCG(seed, stream)) //nolint:gosec // reproducible draws of sample data, not a secret
	perm := r.Perm(len(from))
	n = min(n, len(from))
	out := make([]sample, n)
	for i := range n {
		out[i] = from[perm[i]]
	}
	return out
}

// short is the beginning of an ID, for progress lines.
func short(id string) string {
	return strings.SplitN(id, "-", 2)[0]
}
