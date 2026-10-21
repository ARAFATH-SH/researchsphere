# ResearchSphere: instructions for AI coding assistants

ResearchSphere is a web platform where researchers build a profile, find collaborators
through AI matching, write papers together in a shared LaTeX editor and get venue
recommendations. One developer builds it in 30 slices over 12 weeks.

## Read first

- `docs/SRS.md` says what to build. Requirement IDs such as FR-7.2, AI-2.1 and NFR-4
  refer to it. When code and the SRS disagree, stop and ask which one changes.
- `docs/openapi.yaml` is the API contract (added in slice 4). Change it before a handler.
- `docs/decisions.md` lists decisions already made. Do not reopen one without asking.

## Layout

| Folder | Holds | Stack |
| --- | --- | --- |
| `frontend/` | Web client | Next.js, React, TypeScript, Tailwind CSS |
| `backend/` | API server; migrations in `backend/db/migrations` | Go 1.25+, sqlc, oapi-codegen, golang-migrate |
| `ai/` | Embeddings, matching, search, venue ranking, writing assistant | Python 3.12 with uv, FastAPI, Sentence Transformers |
| `sync/` | Real-time editing server (slice 22) | Node.js, Hocuspocus, Yjs |
| `compile/` | LaTeX compile service (slice 23) | TeX Live in a Docker sandbox |
| `docs/` | SRS, API contract, decisions | Markdown, OpenAPI 3 |

Data stores: PostgreSQL 16 with pgvector, Redis, and S3-compatible object storage (MinIO
locally, console at http://localhost:9001). All run in Docker, with Mailpit for local email.

## Commands

Run these from the repository root. `make help` prints the current list.

| Command | What it does |
| --- | --- |
| `make up` / `make down` | Start or stop PostgreSQL, Redis, Mailpit (local email inbox at http://localhost:8025) and MinIO; `make up` also creates the private bucket |
| `make check` | Confirm that PostgreSQL, pgvector, Redis and storage respond, and that the bucket refuses anonymous access |
| `make psql` | Open a psql prompt in the database |
| `make migrate` / `make migrate-down` | Apply all migrations, or undo the latest one |
| `make migrate-new name=add_users` | Create a new migration pair |
| `make admin email=...` | Give that account the admin role (the only way to make an administrator; there is no API for it). Prints a message and fails if no account has the address. Takes effect at the user's next token refresh or login |
| `make unadmin email=...` | Set that account back to the researcher role |
| `make api` | Run the API server (needs `make up`; settings from `.env`) |
| `make web` | Run the web client dev server on port 3000; it forwards `/api/v1` to `API_URL` |
| `make web-clean` | Delete the web client build cache (`frontend/.next`). Stop `make web` first; run it after switching branches if styles look broken |
| `make generate` | Regenerate Go code in `backend/internal/api` and TypeScript types in `frontend/src/lib/api` from `docs/openapi.yaml`, and the sqlc query code in `backend/internal/*/adapter/postgres/sqlcgen` |
| `make check-generated` | Regenerate and fail if the generated code differs from what is committed |
| `make test` | Run all tests: `test-db`, `test-go`, `test-integration` and `test-web` (needs `make up` and `make migrate`) |
| `make test-db` | Run the database constraint tests (needs `make up` and `make migrate`) |
| `make test-go` | Run the backend Go tests (`go test -race`); needs no database |
| `make test-integration` | Run the Go repository tests and the end-to-end tests in `cmd/api` (build tag `integration`) against the database, Redis and MinIO (needs `make up` and `make migrate`) |
| `make test-web` | Run the frontend tests (Jest) |
| `make lint` | Run all linters: `lint-go` and `lint-web` |
| `make lint-go` | Run golangci-lint for the backend |
| `make lint-web` | Run ESLint and the TypeScript type check for the frontend |

The frontend targets need its packages: run `cd frontend && npm ci` once, and again after
`package-lock.json` changes. When a slice adds a command to the Makefile, add it to this
table in the same change.

CI (`.github/workflows/ci.yml`) runs on every push and pull request: it starts the stack (MinIO included) and runs
`make check`, applies all migrations, undoes them, checks that nothing is left, applies them
again, and runs `make test-db` and the Go repository tests (`make test-integration`), then lints and tests each of `backend/`, `frontend/` and `ai/`
once that folder has its manifest (`go.mod`, `package.json`, `pyproject.toml`). The backend job
also fails when the code generated from `docs/openapi.yaml` is not up to date. The frontend
job runs `npm ci`, checks that the generated API types are up to date, then runs lint, type
check, tests and a production build.

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
  then auth, profile, media, social, project, event and notification. Return errors, do not panic.
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
  the module has a domain interface that PostgreSQL implements.
- **Frontend:** Next.js App Router, TypeScript in strict mode, Tailwind CSS, npm. The Node.js
  major version is pinned in `frontend/.nvmrc`. Layout of `frontend/src`:

  | Path | Holds |
  | --- | --- |
  | `app/` | Routes only: pages and layouts, kept thin |
  | `components/` | Shared components used by several features (header, footer, placeholders) |
  | `lib/api/` | `client.ts`, the one API client (openapi-fetch), and `schema.gen.ts`, generated from `docs/openapi.yaml` (do not edit; run `make generate`) |
  | `lib/` | Other helpers that belong to no feature |
  | `features/<module>/` | Code for one feature, named after the backend module (health, auth, profile, social, project, event, notification); created when the module has code |

  Every API call goes through `lib/api/client.ts`, never a hand-written `fetch`. The browser
  calls `/api/v1` on its own origin and `next.config.ts` forwards it to `API_URL`, which is
  read on the server only (never `NEXT_PUBLIC_`). Jest tests (`*.test.ts(x)`) sit beside the
  code they test and mock the API module, not the network. Pages except the workspace must
  work at 768 pixels wide (SRS 5.1).
- **Layout widths:** Use Container for page width; never set max-width on a section.
- **UI:** follow `docs/ui-guidelines.md` (colors, cards, pill buttons, signed-out and signed-in
  layouts, forms). Use the design tokens from `globals.css`, never raw colors.
- **API:** JSON under `/api/v1`. Errors have the shape `{"code": "...", "message": "..."}`.
  Lists are paginated and the server caps the page size at 50.
- **SQL:** each module's queries are in `internal/<module>/adapter/postgres/queries.sql`; sqlc
  generates `sqlcgen/` beside it (`backend/sqlc.yaml`, run by `make generate`). Repository tests
  that need the database carry the build tag `integration`.
- **Database:** snake_case names, plural table names, uuid primary keys, `timestamptz` for
  times. Rules that must always hold are constraints in the database.
- **Git:** one branch per slice, named `slice-NN-short-name`. Small commits with messages in
  the imperative ("Add connection requests").
