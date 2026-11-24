package main

import (
	// Venue deadlines are stated in IANA time zones (docs/decisions.md, 2026-11-24): the
	// database of zones is built in, so they resolve on an image without /usr/share/zoneinfo.
	_ "time/tzdata"

	"github.com/jackc/pgx/v5/pgxpool"

	eventhttp "github.com/ARAFATH-SH/researchsphere/backend/internal/event/adapter/http"
	eventpg "github.com/ARAFATH-SH/researchsphere/backend/internal/event/adapter/postgres"
	eventusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/event/usecase"
	"github.com/ARAFATH-SH/researchsphere/backend/internal/platform/postgres"
)

// newVenues wires the event module (FR-14, FR-15.1): venues live in PostgreSQL, and the
// database's triggers queue their embeddings (AI-1.3), so the module needs nothing else.
func newVenues(pool *pgxpool.Pool) *eventhttp.Handler {
	svc := eventusecase.New(eventpg.NewStore(pool), postgres.NewTransactor(pool), nil)
	return eventhttp.NewHandler(svc, nil)
}
