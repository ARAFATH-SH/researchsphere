package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// profilesPerSecond is about the embedding rate measured on CPU in slice 16 (one API worker,
// the AI service in Docker, intfloat/multilingual-e5-base, 8 CPUs: 4.3 to 4.9 per second);
// it gives the estimate printed before a run. The real rate depends on the machine.
const profilesPerSecond = 4.0

// rateWindow is how far back the printed rate looks, so a pause does not drag it down for long.
const rateWindow = time.Minute

func estimate(profiles int) time.Duration {
	return (time.Duration(float64(profiles)/profilesPerSecond) * time.Second).Round(time.Minute)
}

// Sample jobs still to embed, and those that failed five times; and sample profiles embedded.
const sampleQueue = `
SELECT count(*) FILTER (WHERE j.failed_at IS NULL), count(*) FILTER (WHERE j.failed_at IS NOT NULL)
FROM embedding_jobs j JOIN sample_profiles s ON s.user_id = j.owner_id
WHERE j.owner_type = 'profile'`

const sampleEmbedded = `
SELECT count(*), (SELECT count(*) FROM sample_profiles)
FROM profile_embeddings e JOIN sample_profiles s ON s.user_id = e.user_id`

type queueState struct {
	pending, failed, embedded, total int
}

func readQueue(ctx context.Context, pool *pgxpool.Pool) (queueState, error) {
	var q queueState
	if err := pool.QueryRow(ctx, sampleQueue).Scan(&q.pending, &q.failed); err != nil {
		return q, err
	}
	if err := pool.QueryRow(ctx, sampleEmbedded).Scan(&q.embedded, &q.total); err != nil {
		return q, err
	}
	return q, nil
}

// waitForEmbeddings reports progress every interval until no sample profile waits for its
// embedding. The running API's worker does the work (`make api`, with the AI service up); when
// nothing moves for stall, it says so and keeps waiting. Ctrl-C stops waiting, not the work.
func waitForEmbeddings(ctx context.Context, pool *pgxpool.Pool, out io.Writer, interval, stall time.Duration) (queueState, error) {
	q, err := readQueue(ctx, pool)
	if err != nil {
		return q, err
	}
	type sample struct {
		at      time.Time
		pending int
	}
	window := []sample{{time.Now(), q.pending}}
	last, lastMove := q.pending, time.Now()
	warned := false
	for q.pending > 0 {
		select {
		case <-ctx.Done():
			return q, ctx.Err()
		case <-time.After(interval):
		}
		if q, err = readQueue(ctx, pool); err != nil {
			return q, err
		}
		if q.pending != last {
			last, lastMove, warned = q.pending, time.Now(), false
		}
		now := time.Now()
		window = append(window, sample{now, q.pending})
		for len(window) > 2 && now.Sub(window[1].at) >= rateWindow {
			window = window[1:]
		}
		line := fmt.Sprintf("Embedded %d of %d sample profiles; %d queued", q.embedded, q.total, q.pending)
		if done := window[0].pending - q.pending; done > 0 {
			rate := float64(done) / now.Sub(window[0].at).Seconds()
			left := time.Duration(float64(q.pending)/rate) * time.Second
			line += fmt.Sprintf(", %.1f per second, about %s left", rate, left.Round(time.Second))
		}
		fmt.Fprintln(out, line)
		if !warned && time.Since(lastMove) >= stall {
			fmt.Fprintf(out, "Nothing has moved for %s: is `make api` running, with the AI service up (make check)?\n", stall)
			warned = true
		}
	}
	return q, nil
}
