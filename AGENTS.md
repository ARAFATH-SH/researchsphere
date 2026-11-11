# ResearchSphere: instructions for AI coding assistants

ResearchSphere is a web platform where researchers build a profile, find collaborators
through AI matching, write papers together in a shared LaTeX editor and get venue
recommendations. One developer builds it in 30 slices over 12 weeks.

## Read first

- `docs/SRS.md` says what to build. Requirement IDs such as FR-7.2, AI-2.1 and NFR-4
  refer to it. When code and the SRS disagree, stop and ask which one changes.
- `docs/openapi.yaml` is the API contract (added in slice 4). Change it before a handler.
- `docs/decisions.md` lists decisions already made. Do not reopen one without asking.
- `docs/realtime.md` is the WebSocket contract: events, payloads, close codes and what a client
  must do after a reconnect. OpenAPI covers only the upgrade.

## Layout

| Folder | Holds | Stack |
| --- | --- | --- |
| `frontend/` | Web client | Next.js, React, TypeScript, Tailwind CSS |
| `backend/` | API server; migrations in `backend/db/migrations` | Go 1.25+, sqlc, oapi-codegen, golang-migrate |
| `ai/` | Embeddings, matching, search, venue ranking, writing assistant | Python 3.12 with uv, FastAPI, Sentence Transformers |
| `sync/` | Real-time editing server (slice 22) | Node.js, Hocuspocus, Yjs |
| `compile/` | LaTeX compile service (slice 23) | TeX Live in a Docker sandbox |
| `docs/` | SRS, API contract, decisions | Markdown, OpenAPI 3 |
| `design/` | Reference design: one HTML artboard per screen, desktop and `_m` mobile (not served, not imported) | HTML, CSS |

Data stores: PostgreSQL 16 with pgvector, Redis, and S3-compatible object storage (MinIO
locally, console at http://localhost:9001). All run in Docker, with Mailpit for local email
and ClamAV (clamd), which scans message attachments for malware.

## Commands

Run these from the repository root. `make help` prints the current list.

| Command | What it does |
| --- | --- |
| `make up` / `make down` | Start or stop PostgreSQL, Redis, Mailpit (local email inbox at http://localhost:8025), MinIO and ClamAV; `make up` also creates the private bucket, and starts ClamAV last without waiting for it (clamd takes 30 to 90 seconds and 1.5 to 3 GB of memory to load its signatures; attachments stay pending until it answers). `make up CLAMAV=0` leaves ClamAV out, as the CI stack job does |
| `make check` | Confirm that PostgreSQL, pgvector, Redis and storage respond, and that the bucket refuses anonymous access; show whether ClamAV answers yet (not a failure) |
| `make psql` | Open a psql prompt in the database |
| `make migrate` / `make migrate-down` | Apply all migrations, or undo the latest one |
| `make migrate-new name=add_users` | Create a new migration pair |
| `make admin email=...` | Give that account the admin role (the only way to make an administrator; there is no API for it). Prints a message and fails if no account has the address. Takes effect at the user's next token refresh or login |
| `make unadmin email=...` | Set that account back to the researcher role |
| `make seed-ui-compare` | Create or refresh the fake accounts `npm run ui:compare` signs in with: Compare Alpha and Compare Beta, connected, with about 40 messages over several days and Beta's open report of a message from Alpha, Compare Gamma's pending message request to Alpha, and Compare Admin, an administrator (`@compare.researchsphere.local`). Needs `make migrate` and `UI_COMPARE_PASSWORD`. Refuses a database name ending in `_test`, `APP_ENV=production` and any host but this machine |
| `make api` | Run the API server (needs `make up`; settings from `.env`) |
| `make web` | Run the web client dev server on port 3000; it forwards `/api/v1` to `API_URL` |
| `make stop` | Stop this project's dev servers: the API on `API_PORT` (8080) and the Next.js dev server on ports 3000-3002. Stops only your own processes running inside this repository, prints what it stopped, and removes a stale `frontend/.next/dev/lock` when no dev server is left. Leaves the Docker containers running (use `make down` for those) |
| `make web-clean` | Delete the web client build cache (`frontend/.next`). Stop `make web` first (`make stop`); run it after switching branches if styles look broken |
| `make generate` | Regenerate Go code in `backend/internal/api` and TypeScript types in `frontend/src/lib/api` from `docs/openapi.yaml`, and the sqlc query code in `backend/internal/*/adapter/postgres/sqlcgen` |
| `make check-generated` | Regenerate and fail if the generated code differs from what is committed |
| `make test` | Run all tests: `test-prepare`, `test-db`, `test-go`, `test-integration` and `test-web` (needs `make up`) |
| `make test-prepare` | Create the test database `researchsphere_test` (`TEST_POSTGRES_DB`) in the same container if it is missing and apply all migrations to it. Refuses a name that does not end in `_test` |
| `make test-db` | Run the database constraint tests on the test database (runs `test-prepare` first; needs `make up`) |
| `make test-go` | Run the backend Go tests (`go test -race`); needs no database |
| `make test-integration` | Run the Go repository tests and the end-to-end tests in `cmd/api` (build tag `integration`) against the test database (`TEST_DATABASE_URL`), Redis DB 15 (`TEST_REDIS_URL`) and MinIO, one package at a time (`-p 1`). Runs `test-prepare` first; needs `make up` |
| `make test-one pkg=./internal/x/ run=TestName [count=1]` | Run one integration test, or the tests `run` matches (a `go test -run` pattern), with the same settings as `test-integration`: `-race`, `-p 1`, the build tag and the test environment. `pkg` is relative to `backend/`; `count` repeats it to find a flaky test. Runs `test-prepare` first; needs `make up` |
| `make test-web` | Run the frontend tests (Jest) |
| `make lint` | Run all linters: `lint-go` and `lint-web` |
| `make lint-go` | Run golangci-lint for the backend |
| `make lint-web` | Run ESLint and the TypeScript type check for the frontend |

The frontend targets need its packages: run `cd frontend && npm ci` once, and again after
`package-lock.json` changes. When a slice adds a command to the Makefile, add it to this
table in the same change.

CI (`.github/workflows/ci.yml`) runs on every pull request to `main` and every push to `main`
(NFR-7), not on pushes to other branches. A `detect` job runs first; when a change touches only
`docs/**`, `design/**` or Markdown files (never `docs/openapi.yaml`), every other job shows as
skipped. Next, in parallel, the fast checks lint and test each of `backend/`, `frontend/` and
`ai/` once that folder has its manifest (`go.mod`, `package.json`, `pyproject.toml`). The
backend job also fails when the code generated from `docs/openapi.yaml` is not up to date; the
frontend job runs `npm ci`, checks that the generated API types are up to date, then runs lint,
type check, tests and a production build. Only when they pass does the stack job start
PostgreSQL, Redis, Mailpit and MinIO (`make up CLAMAV=0`), run `make check`, apply all
migrations, undo them, check that nothing is left, apply them again, prepare the test database
(`make test-prepare`) and run `make test-db` and the Go repository tests (`make test-integration`).
The `clamav` job scans the EICAR string with a real clamd only on push to `main`, nightly and
by hand. The job names (`Detect`, `Backend (Go)`, `Frontend (Node.js)`, `AI (Python, uv)`,
`Stack (integration)`, `ClamAV (EICAR)`) are the status check names branch protection uses:
do not rename them.

## Rules

1. Work on one slice. The user names the slice and its requirement IDs. Build nothing beyond them.
2. Plan before code: list the endpoints, database changes, files and tests, and wait for approval.
3. Order inside a slice: OpenAPI entry and migration, then tests, then code, then the page.
4. Never edit a migration that is already merged. Add a new one, and give every up a working down.
5. Write SQL as sqlc queries with parameters. Never build SQL by joining strings.
6. Every endpoint checks the user's role on the server, as the tables in SRS section 2.3 require.
7. Every requirement a slice covers gets at least one test, including the cases that must be refused.
8. Keep secrets out of code, tests, commits and logs. Settings come from environment variables; add each new one to `.env.example`.
9. Ask before adding a dependency, and say what it is needed for.
10. Change only the folders the slice touches, plus `docs/` and this file.
11. Before calling a slice done, run `make lint` and `make test` and report what they printed.
12. When a requirement is unclear or looks wrong, stop and ask. Do not guess.
13. Say plainly what you did not do or could not verify.

## Conventions

- **Go:** one application (modular monolith). Modules live in `backend/internal/<module>`: health,
  then auth, profile, media, social, project, event, notification, messaging, moderation and realtime. Return errors, do not panic.
  Table-driven tests beside the code they test. Each module is split into layers (clean
  architecture); `backend/internal/health` is the example to copy:

  | Path | Holds | May import |
  | --- | --- | --- |
  | `internal/<module>/domain` | Entities and the interfaces the usecase needs | Standard library only, no `net/http` |
  | `internal/<module>/usecase` | Business logic | Standard library and its own `domain` |
  | `internal/<module>/adapter/http` | Handlers for the module's operations in `api/gen` | domain, usecase, `platform`, `api/gen` |
  | `internal/<module>/adapter/postgres`, `adapter/redis` | Implementations of the domain interfaces | domain, `platform`, pgx, go-redis |
  | `internal/platform` | Config, logging, database pool, Redis client, object storage, HTTP server, middleware, error shape | Libraries and `api/gen`; never a module |
  | `internal/api/gen` | Code generated from `docs/openapi.yaml` (do not edit) | |
  | `cmd/api` | The only place that wires modules and platform together | Everything |

  The dependency rule points inward: domain knows nothing outside the standard library, a
  usecase knows only its own domain, and adapters depend on the inner layers, never the
  reverse. depguard in `backend/.golangci.yml` enforces it, so `make lint` fails on a
  violation. A new module adds a `usecase-<module>` depguard rule that allows only
  `$gostd` and its own domain. Do not create empty layers: add `adapter/postgres` only when
  the module has a domain interface that PostgreSQL implements. A module never imports
  another: when one needs another's data, its domain declares an interface and `cmd/api`
  implements it with the other module's usecase (example: `cmd/api/connections.go` gives
  profiles their connection status from the `social` module). When two modules must write
  together (a connection change and its notification), the usecase calls `InTx` on a
  `Transactor` its domain declares, `cmd/api` passes `postgres.NewTransactor(pool)`, and
  each store runs its queries on `postgres.Conn(ctx, pool)` (`internal/platform/postgres`),
  which is the context's transaction when there is one. Example: the `social` usecase
  stores a request and calls its `Notifier`, which `cmd/api/notifications.go` implements
  with the `notification` usecase, in one transaction. Real-time events are published after
  the commit: a `cmd/api` publisher wraps its work in `postgres.AfterCommit`, which waits for
  the transaction the context carries, drops the work on a rollback and runs at once outside a
  transaction, so a usecase may call its `Publisher` even inside another module's transaction
  (`cmd/api/realtime.go`). `internal/realtime/adapter/http` is the only code that imports the
  WebSocket library.
- **Frontend:** Next.js App Router, TypeScript in strict mode, Tailwind CSS, npm. The Node.js
  major version is pinned in `frontend/.nvmrc`. Layout of `frontend/src`:

  | Path | Holds |
  | --- | --- |
  | `app/` | Routes only: pages and layouts, kept thin |
  | `components/` | Shared components used by several features (header, footer, placeholders) |
  | `lib/api/` | `client.ts`, the one API client (openapi-fetch), and `schema.gen.ts`, generated from `docs/openapi.yaml` (do not edit; run `make generate`) |
  | `lib/` | Other helpers that belong to no feature |
  | `features/<module>/` | Code for one feature, named after the backend module (health, auth, profile, social, project, event, notification, messaging); created when the module has code |
  | `features/realtime/` | The one WebSocket client of a tab (`socket.ts`), its event types and React hooks; the stores of each feature listen to it |
  | `features/moderation/` | Warnings (banner and page) and the administrator's reports, actions and user history. It spans the backend's `moderation` module and the reports of `messaging` |
| `features/search/` | The search page, which spans modules: its People tab uses the profile module's search API; Projects and Posts tabs join it with their slices |

  Every API call goes through `lib/api/client.ts`, never a hand-written `fetch`. The browser
  calls `/api/v1` on its own origin and `next.config.ts` forwards it to `API_URL`, which is
  read on the server only (never `NEXT_PUBLIC_`). The one exception is the WebSocket, which goes
  straight to the API at `NEXT_PUBLIC_WS_URL`, because a rewrite is not known to carry it
  (`docs/decisions.md`). A second exception is a file upload with progress, which needs
  `XMLHttpRequest`: `uploadFile` in `lib/api/upload.ts` is the only code that uses it (ESLint refuses it
  elsewhere) and keeps the client's refresh and lost-session handling. Every session refresh goes through `refreshSession` in `lib/api/client.ts`,
  so tabs take turns. Jest tests (`*.test.ts(x)`) sit beside the
  code they test and mock the API module, not the network. `frontend/jest.setup.ts` fails any test that
  calls `console.error`; its allowlist is only for documented third-party noise. The default
  multi-file run does not show console output, so use `npx jest --verbose` to read it. Pages except the workspace must
  work at 768 pixels wide (SRS 5.1).
- **Layout widths:** Use Container for page width; never set max-width on a section.
- **UI:** follow `docs/ui-guidelines.md` (colors, cards, pill buttons, signed-out and signed-in
  layouts, forms). Use the design tokens from `globals.css`, never raw colors.
  `design/screens/` is the source of truth for look and spacing; restyled pages use the `rs-*`
  tokens. `cd frontend && npm run ui:compare -- <route> <design-file>` (dev only, needs
  `make web`) screenshots a route beside its design and prints the pixel difference.
- **API:** JSON under `/api/v1`. Errors have the shape `{"code": "...", "message": "..."}`;
  a `bad_request` about one body field or parameter adds `"field"`, and never repeats the value.
  Lists are paginated and the server caps the page size at 50 (a conversation's messages cap at 100).
- **SQL:** each module's queries are in `internal/<module>/adapter/postgres/queries.sql`; sqlc
  generates `sqlcgen/` beside it (`backend/sqlc.yaml`, run by `make generate`). Repository tests
  that need the database carry the build tag `integration`.
- **Integration tests never touch development data.** They get their connections from
  `internal/platform/testenv`: `testenv.DB(t)` connects to `TEST_DATABASE_URL` and empties every
  table, `testenv.Redis(t)` connects to `TEST_REDIS_URL` (DB 15) and flushes that DB only. Both
  refuse to run unless the database name ends in `_test` and the Redis DB is not 0 or the one
  `REDIS_URL` uses. Call each once per test, before opening any transaction (the truncate waits
  for open transactions and fails after 10 seconds). Do not use `t.Parallel()` in integration
  tests, and do not add row or key cleanups: the next test starts empty. Only objects written
  to MinIO need cleaning up. Use `testenv.Word`, `testenv.Email` and `testenv.ORCIDiD` for
  names, addresses and ORCID iDs rather than fixed values.
- **Database:** snake_case names, plural table names, uuid primary keys, `timestamptz` for
  times. Rules that must always hold are constraints in the database.
- **Git:** one branch per slice, named `slice-NN-short-name`. Small commits with messages in
  the imperative ("Add connection requests").
