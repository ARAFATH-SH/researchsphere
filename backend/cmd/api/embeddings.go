package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	embeddingpg "github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/adapter/postgres"
	embeddingdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/domain"
	embeddingusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/aiclient"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/config"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/logging"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// aiEmbedder gives the embedding module the AI service, with its errors in the module's
// terms: down or turned off keeps the job; anything else is a problem with the text.
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

// embedderError keeps err and adds the embedding domain's error, so both errors.Is checks work.
func embedderError(err error) error {
	switch {
	case errors.Is(err, aiclient.ErrNotConfigured):
		return errors.Join(embeddingdomain.ErrNotConfigured, err)
	case errors.Is(err, aiclient.ErrUnavailable), errors.Is(err, context.DeadlineExceeded):
		// A job that ran out of time waited on the service, not on its text.
		return errors.Join(embeddingdomain.ErrUnavailable, err)
	default:
		return err
	}
}

// newEmbeddings is the embedding module: the admin's service and the queue worker (AI-1).
func newEmbeddings(pool *pgxpool.Pool, ai aiclient.Embedder, logger *slog.Logger) (*embeddingusecase.Service, *embeddingusecase.Worker) {
	store := embeddingpg.NewStore(pool)
	embedder := aiEmbedder{ai: ai}
	return embeddingusecase.New(store, embedder, logger),
		embeddingusecase.NewWorker(store, embedder, embeddingusecase.DefaultWorkerConfig(), time.Now, logger)
}

// runEmbedBackfillOnce queues every eligible profile and every project once (AI-1.5) and
// returns; the running API's worker embeds them. `make embed-backfill` runs it. An owner whose
// text and model have not changed is skipped by the worker.
func runEmbedBackfillOnce(ctx context.Context, getenv func(string) string, logOut io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	logger := logging.New(logOut, cfg.LogLevel)
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	svc, _ := newEmbeddings(pool, aiclient.New(aiclient.Config{BaseURL: cfg.AIServiceURL, Token: cfg.AIServiceToken}), logger)
	n, err := svc.Backfill(ctx)
	if err != nil {
		return err
	}
	logger.Info("embedding backfill queued", slog.Int("profiles", n))
	return nil
}
