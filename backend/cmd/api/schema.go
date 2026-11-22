package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// requiredSchemaVersion is the newest migration in db/migrations, the database this code
// needs. TestRequiredSchemaVersion keeps it equal to the files, so a slice that adds a
// migration raises it too.
const requiredSchemaVersion = 37

// schemaTimeout bounds the check, so a database that is down does not hold up the start.
const schemaTimeout = 5 * time.Second

// errSchemaBehind refuses to start on a database whose migrations are not all applied: a
// route would otherwise fail with a missing table only when used (docs/decisions.md
// 2026-11-18: figure uploads answered 500 on a database at migration 35).
var errSchemaBehind = errors.New("the database is not migrated")

// checkSchema reads golang-migrate's version. A database that cannot be reached is only
// logged, as the API starts while the database is down and the health check reports it.
func checkSchema(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	ctx, cancel := context.WithTimeout(ctx, schemaTimeout)
	defer cancel()
	var (
		version int64
		dirty   bool
	)
	if err := pool.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty); err != nil {
		logger.WarnContext(ctx, "database migration version not read; not checked", slog.Any("error", err))
		return nil
	}
	return compareSchema(version, dirty)
}

func compareSchema(version int64, dirty bool) error {
	switch {
	case dirty:
		return fmt.Errorf("%w: migration %d failed halfway (dirty); fix it, then run make migrate", errSchemaBehind, version)
	case version < requiredSchemaVersion:
		return fmt.Errorf("%w: it is at migration %d and this API needs %d; run make migrate", errSchemaBehind, version, requiredSchemaVersion)
	}
	return nil
}
