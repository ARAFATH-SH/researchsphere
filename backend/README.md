# Backend: Go API server and database migrations

## Layout

| Path | Holds |
| --- | --- |
| `cmd/api` | Entry point and the only wiring: reads settings, builds the modules, starts the server, shuts it down on Ctrl+C or SIGTERM |
| `internal/api/gen` | Code generated from `docs/openapi.yaml` (`api.gen.go`, do not edit) and the generator settings |
| `internal/platform/config` | Settings from environment variables |
| `internal/platform/logging` | JSON logger; request ID in the context |
| `internal/platform/httpx` | Error shape, request IDs, access log, panic recovery, JSON 404/405 |
| `internal/platform/httpserver` | Router (generated routes under `/api/v1` plus middleware) and graceful `Serve` |
| `internal/platform/postgres`, `internal/platform/redis` | Shared PostgreSQL pool and Redis client |
| `internal/health/domain` | Health entities and the `Pinger` interface |
| `internal/health/usecase` | Pings every dependency at once, each under a timeout |
| `internal/health/adapter/http` | `GET /api/v1/health` |
| `internal/health/adapter/postgres`, `adapter/redis` | `Pinger` for PostgreSQL and Redis |
| `db/migrations` | golang-migrate migrations |
| `db/tests` | SQL tests for database constraints |

## Settings

| Variable | Required | Default |
| --- | --- | --- |
| `DATABASE_URL` | yes | |
| `REDIS_URL` | yes | |
| `API_PORT` | no | `8080` |
| `LOG_LEVEL` | no | `info` (`debug`, `info`, `warn`, `error`) |

The server stops at startup with a message naming any missing or invalid setting. It never
logs the URLs, which can contain passwords.

Each module follows the layers and dependency rule in AGENTS.md (Conventions); depguard
makes `make lint` fail on a violation.

## Working on the API

1. Change `docs/openapi.yaml` first.
2. Run `make generate` and implement the new methods of `gen.StrictServerInterface` in the
   module's `adapter/http`, then embed that handler in `cmd/api/handlers.go`.
3. Run `make lint` and `make test` (from the repository root).

`make api` runs the server with the settings in `.env`.
