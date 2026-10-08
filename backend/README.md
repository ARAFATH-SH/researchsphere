# Backend: Go API server and database migrations

## Layout

| Path | Holds |
| --- | --- |
| `cmd/api` | Entry point: reads settings, starts the server, shuts it down on Ctrl+C or SIGTERM |
| `internal/api` | Code generated from `docs/openapi.yaml` (`api.gen.go`, do not edit) and the generator settings |
| `internal/config` | Settings from environment variables |
| `internal/httpx` | Error shape, request IDs, access log, panic recovery, JSON 404/405 |
| `internal/health` | `GET /api/v1/health` |
| `internal/server` | Builds the router: generated routes under `/api/v1` plus middleware |
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

## Working on the API

1. Change `docs/openapi.yaml` first.
2. Run `make generate` and implement the new methods of `api.StrictServerInterface`.
3. Run `make lint` and `make test` (from the repository root).

`make api` runs the server with the settings in `.env`.
