package usecase

import (
	"context"
	"log/slog"
	"time"

	"github.com/ARAFATH-SH/researchsphere/backend/internal/embedding/domain"
)

// modelTimeout bounds the statistics' question to the AI service, so the admin page does not
// wait for a service that is down.
const modelTimeout = 2 * time.Second

// Service answers the administrators' questions about embeddings and queues them all again.
// Role checks are the HTTP adapter's.
type Service struct {
	store    domain.Store
	embedder domain.Embedder
	logger   *slog.Logger
}

// New returns a Service.
func New(store domain.Store, embedder domain.Embedder, logger *slog.Logger) *Service {
	return &Service{store: store, embedder: embedder, logger: logger}
}

// Stats is the admin page's view: the AI service's model ("" when it does not answer) and
// each owner type's counts.
type Stats struct {
	Model  string
	Owners []domain.OwnerStats
}

// Stats returns the current model and the counts of each owner type.
func (s *Service) Stats(ctx context.Context) (Stats, error) {
	mctx, cancel := context.WithTimeout(ctx, modelTimeout)
	model, err := s.embedder.Model(mctx)
	cancel()
	if err != nil {
		model = ""
	}
	owners, err := s.store.Stats(ctx, model)
	if err != nil {
		return Stats{}, err
	}
	return Stats{Model: model, Owners: owners}, nil
}

// Rebuild queues every eligible owner to be embedded again, at low priority, and records
// who asked. Asking twice leaves one job per owner. It returns how many it queued.
func (s *Service) Rebuild(ctx context.Context, adminID string) (int, error) {
	n, err := s.store.Rebuild(ctx, adminID)
	if err != nil {
		return 0, err
	}
	s.logger.InfoContext(ctx, "embeddings rebuild queued", slog.String("admin_id", adminID), slog.Int("queued", n))
	return n, nil
}

// Backfill queues every eligible owner at low priority, not forced: an owner whose text and
// model have not changed is skipped. `make embed-backfill` runs it once.
func (s *Service) Backfill(ctx context.Context) (int, error) {
	return s.store.QueueAll(ctx, false)
}
