package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	recommendationusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/recommendation/usecase"
)

// Calibration pairs per tuning query: its nearest tuning neighbours, the region the shown
// recommendations come from, and as many random tuning profiles, which anchor the low end.
const (
	calibNeighbours = 50
	calibRandom     = 50
)

// pair is a calibration example: a cosine and whether the two share their main cluster.
type pair struct {
	cosine float64
	same   bool
}

// calibrate fits the score map on the TUNING split only: queries and candidates are both
// tuning profiles, so the test split stays unseen.
func calibrate(ctx context.Context, pool *pgxpool.Pool, s samples, o options, out io.Writer) error {
	tuning := s.ids("tuning")
	if len(tuning) < 2 {
		return errors.New("too few tuning profiles to calibrate")
	}
	queries := draw(tuning, o.queries, o.seed, 1)
	r := rand.New(rand.NewPCG(o.seed, 2)) //nolint:gosec // reproducible draws of sample data, not a secret
	fmt.Fprintf(out, "Calibrating on %d tuning queries (seed %d): %d nearest tuning neighbours and %d random tuning profiles each.\n",
		len(queries), o.seed, calibNeighbours, calibRandom)
	started := time.Now()
	var pairs []pair
	for i, q := range queries {
		near, err := rankSamples(ctx, pool, q.id, calibNeighbours, "tuning")
		if err != nil {
			return err
		}
		for _, n := range near {
			pairs = append(pairs, pair{n.cosine, s.byID[n.id].cluster == q.cluster})
		}
		others := make([]string, 0, calibRandom)
		for len(others) < calibRandom {
			if p := tuning[r.IntN(len(tuning))]; p.id != q.id {
				others = append(others, p.id)
			}
		}
		cs, err := cosines(ctx, pool, q.id, others)
		if err != nil {
			return err
		}
		for _, id := range others {
			pairs = append(pairs, pair{cs[id], s.byID[id].cluster == q.cluster})
		}
		if (i+1)%500 == 0 {
			fmt.Fprintf(out, "  %d of %d queries (%s)\n", i+1, len(queries), time.Since(started).Round(time.Second))
		}
	}
	slope, intercept, err := fitLogistic(pairs)
	if err != nil {
		return err
	}
	m := recommendationusecase.ScoreMap{Model: s.model, Slope: slope, Intercept: intercept}
	if err := m.Validate(); err != nil {
		return fmt.Errorf("the fit gave an unusable map: %w", err)
	}
	same := 0
	for _, p := range pairs {
		if p.same {
			same++
		}
	}
	fmt.Fprintf(out, "Fitted on %d pairs (%d same cluster, %d different) in %s.\n\n", len(pairs), same, len(pairs)-same,
		time.Since(started).Round(time.Second))
	fmt.Fprintf(out, "RECS_SCORE_MODEL=%s\nRECS_SCORE_SLOPE=%.4f\nRECS_SCORE_INTERCEPT=%.4f\n\n", m.Model, m.Slope, m.Intercept)
	printCalibrationTable(out, pairs, m)
	return nil
}

// fitLogistic fits P(same | cos) = 1 / (1 + exp(-(a*cos + b))) by maximum likelihood
// (Newton's method), on a standardised cosine for a well-conditioned step, with a tiny L2
// penalty so a perfectly separable set still converges. It returns a and b on the raw
// cosine.
func fitLogistic(pairs []pair) (slope, intercept float64, err error) {
	n := float64(len(pairs))
	if n < 2 {
		return 0, 0, errors.New("too few pairs to fit")
	}
	var mean, sq float64
	for _, p := range pairs {
		mean += p.cosine
	}
	mean /= n
	for _, p := range pairs {
		sq += (p.cosine - mean) * (p.cosine - mean)
	}
	sd := math.Sqrt(sq / n)
	if sd == 0 {
		return 0, 0, errors.New("every pair has the same cosine")
	}
	const lambda = 1e-6
	var a, b float64 // on z = (cos - mean) / sd
	for range 100 {
		var ga, gb, haa, hab, hbb float64
		for _, p := range pairs {
			z := (p.cosine - mean) / sd
			pr := 1 / (1 + math.Exp(-(a*z + b)))
			y := 0.0
			if p.same {
				y = 1
			}
			ga += (pr - y) * z
			gb += pr - y
			w := pr * (1 - pr)
			haa += w * z * z
			hab += w * z
			hbb += w
		}
		ga += lambda * n * a
		haa += lambda * n
		det := haa*hbb - hab*hab
		if det <= 0 || math.IsNaN(det) {
			return 0, 0, errors.New("the fit did not converge")
		}
		da := (hbb*ga - hab*gb) / det
		db := (haa*gb - hab*ga) / det
		a, b = a-da, b-db
		if math.Abs(da) < 1e-10 && math.Abs(db) < 1e-10 {
			break
		}
	}
	return a / sd, b - a*mean/sd, nil
}

// printCalibrationTable shows, per cosine band, how many pairs fell in it, how many of them
// share a main cluster, and the score the map gives the band's middle.
func printCalibrationTable(out io.Writer, pairs []pair, m recommendationusecase.ScoreMap) {
	const width = 0.02
	type band struct{ n, same int }
	bands := map[int]*band{}
	lo, hi := math.MaxInt, math.MinInt
	for _, p := range pairs {
		k := int(math.Floor(p.cosine / width))
		if bands[k] == nil {
			bands[k] = &band{}
		}
		bands[k].n++
		if p.same {
			bands[k].same++
		}
		lo, hi = min(lo, k), max(hi, k)
	}
	fmt.Fprintln(out, "| Cosine | Pairs | Same cluster | Score at the middle |")
	fmt.Fprintln(out, "| --- | ---: | ---: | ---: |")
	for k := lo; k <= hi; k++ {
		b := bands[k]
		if b == nil || b.n < 20 {
			continue
		}
		from := float64(k) * width
		fmt.Fprintf(out, "| %.2f to %.2f | %d | %.1f%% | %d |\n", from, from+width, b.n,
			100*float64(b.same)/float64(b.n), m.Score(from+width/2))
	}
}
