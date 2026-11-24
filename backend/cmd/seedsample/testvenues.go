package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	embeddingpg "github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/adapter/postgres"
	embeddingdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/domain"
	embeddingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/aiclient"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// Sample venues for the NFR-6 venue evaluation (slice 25, chunk 25.1; docs/evaluation.md).
// The development database holds real venues, so the labelled sample venues are made in the
// TEST database (TEST_DATABASE_URL) only: -test-venues empties its venues and embedding jobs,
// makes the venuesPerCluster * clusters sample venues of `-venues` dated from today, and embeds
// them with the AI service through the embedding module's own worker (the same text, hash and
// model as the API). It never opens DATABASE_URL. `make eval-venues` runs it, then recseval.
//
// The integration tests empty the test database too: do not run both at once.

// testVenuesTimeout bounds the embedding of the sample venues (200 short texts; about a
// minute on CPU).
const testVenuesTimeout = 15 * time.Minute

// runTestVenues makes and embeds the sample venues in the test database.
func runTestVenues(ctx context.Context, getenv func(string) string, seed int64, out io.Writer) error {
	testURL := getenv("TEST_DATABASE_URL")
	if err := checkTestTarget(testURL, getenv("APP_ENV")); err != nil {
		return err
	}
	ai, err := newAI(getenv)
	if err != nil {
		return err
	}
	target := describeTarget(testURL)
	fmt.Fprintf(out, "Databases: writes %s only (its venues, sample venue labels, venue embeddings and embedding jobs are replaced); the development database is not opened.\n", target)

	c, err := loadContent()
	if err != nil {
		return err
	}
	g := &generator{c: c, seed: seed}
	pool, err := postgres.NewPool(ctx, testURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `TRUNCATE venues, embedding_jobs CASCADE`); err != nil {
		return fmt.Errorf("empty the test database's venues: %w", err)
	}
	if err := runVenues(ctx, pool, g, time.Now().UTC(), out); err != nil {
		return err
	}
	return embedTestVenues(ctx, pool, ai, out)
}

// embedTestVenues runs the embedding worker over the test database until no job waits.
func embedTestVenues(ctx context.Context, pool *pgxpool.Pool, ai aiclient.Embedder, out io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, testVenuesTimeout)
	defer cancel()
	cfg := embeddingusecase.DefaultWorkerConfig()
	cfg.Poll = 200 * time.Millisecond
	worker := embeddingusecase.NewWorker(embeddingpg.NewStore(pool), aiEmbedder{ai: ai}, cfg, time.Now,
		slog.New(slog.DiscardHandler))
	started := time.Now()
	for {
		wait := worker.Tick(ctx)
		var pending, failed int
		if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE failed_at IS NULL), count(*) FILTER (WHERE failed_at IS NOT NULL)
			FROM embedding_jobs`).Scan(&pending, &failed); err != nil {
			return fmt.Errorf("count embedding jobs: %w", err)
		}
		if failed > 0 {
			return fmt.Errorf("%d sample venues failed to embed; see last_error in the test database's embedding_jobs", failed)
		}
		if pending == 0 {
			break
		}
		if ctx.Err() != nil {
			return fmt.Errorf("sample venues not embedded after %s (%d left): is the AI service up (make ai-up)?", testVenuesTimeout, pending)
		}
		if wait > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(min(wait, time.Second)):
			}
		}
	}
	var venues, embedded int
	var model string
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM sample_venues), count(*), coalesce(min(model), '')
		FROM venue_embeddings`).Scan(&venues, &embedded, &model); err != nil {
		return err
	}
	if embedded != venues {
		return fmt.Errorf("%d of %d sample venues embedded", embedded, venues)
	}
	fmt.Fprintf(out, "Embedded %d sample venues with %s in %s.\n", embedded, model, time.Since(started).Round(time.Second))
	return nil
}

// checkTestTarget accepts only the local test database: its name ends in _test, the host is
// this machine, and APP_ENV is not production.
func checkTestTarget(databaseURL, appEnv string) error {
	if databaseURL == "" {
		return errors.New("TEST_DATABASE_URL is not set")
	}
	if strings.EqualFold(strings.TrimSpace(appEnv), "production") {
		return errors.New("refusing to run with APP_ENV=production")
	}
	u, err := url.Parse(databaseURL)
	if err != nil {
		return errors.New("TEST_DATABASE_URL is not a valid URL") // the error would repeat the password
	}
	name := strings.TrimPrefix(u.Path, "/")
	if !strings.HasSuffix(name, "_test") {
		return fmt.Errorf("refusing %q: the sample venues go only into a test database, whose name ends in _test", name)
	}
	if host := u.Hostname(); host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return fmt.Errorf("refusing the database on %s: only a local test database", host)
	}
	return nil
}

// newAI is the AI client the API would use with these settings.
func newAI(getenv func(string) string) (*aiclient.Client, error) {
	token := strings.TrimSpace(getenv("AI_SERVICE_TOKEN"))
	if token == "" {
		return nil, errors.New("AI_SERVICE_TOKEN is not set: the sample venues are embedded with the AI service")
	}
	base := strings.TrimRight(strings.TrimSpace(getenv("AI_SERVICE_URL")), "/")
	if base == "" {
		base = config.DefaultAIServiceURL
	}
	return aiclient.New(aiclient.Config{BaseURL: base, Token: token}), nil
}

// aiEmbedder gives the embedding worker the AI service, as cmd/api does: down or turned off
// keeps the job; anything else is a problem with the text.
type aiEmbedder struct {
	ai aiclient.Embedder
}

var _ embeddingdomain.Embedder = aiEmbedder{}

func (e aiEmbedder) EmbedDocument(ctx context.Context, text string, maxChunks int) ([]float32, string, int, error) {
	res, err := e.ai.EmbedDocument(ctx, text, maxChunks)
	if err != nil {
		return nil, "", 0, embedderError(err)
	}
	return res.Vector, res.Model, res.Chunks, nil
}

func (e aiEmbedder) Model(ctx context.Context) (string, error) {
	m, err := e.ai.Model(ctx)
	if err != nil {
		return "", embedderError(err)
	}
	return m, nil
}

func embedderError(err error) error {
	switch {
	case errors.Is(err, aiclient.ErrNotConfigured):
		return errors.Join(embeddingdomain.ErrNotConfigured, err)
	case errors.Is(err, aiclient.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		return errors.Join(embeddingdomain.ErrUnavailable, err)
	default:
		return err
	}
}
