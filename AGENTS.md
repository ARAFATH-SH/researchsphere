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

Data stores: PostgreSQL 16 with pgvector, and Redis. Both run in Docker.

## Commands

Run these from the repository root. `make help` prints the current list.

| Command | What it does |
| --- | --- |
| `make up` / `make down` | Start or stop PostgreSQL and Redis |
| `make check` | Confirm that PostgreSQL, pgvector and Redis respond |
| `make psql` | Open a psql prompt in the database |
| `make migrate` / `make migrate-down` | Apply all migrations, or undo the latest one |
| `make migrate-new name=add_users` | Create a new migration pair |
| `make api` | Run the API server (needs `make up`; settings from `.env`) |
| `make generate` | Regenerate Go code in `backend/internal/api` from `docs/openapi.yaml` |
| `make check-generated` | Regenerate and fail if the generated code differs from what is committed |
| `make test` | Run all tests: `test-db` and `test-go` (needs `make up` and `make migrate`) |
| `make test-db` | Run the database constraint tests (needs `make up` and `make migrate`) |
| `make test-go` | Run the backend Go tests (`go test -race`); needs no database |
| `make lint` | Run all linters: golangci-lint for the backend |

When a slice adds a command to the Makefile, add it to this table in the same change.

CI (`.github/workflows/ci.yml`) runs on every push and pull request: it starts the stack and runs
`make check`, applies all migrations, undoes them, checks that nothing is left, applies them
again, and runs `make test-db`, then lints and tests each of `backend/`, `frontend/` and `ai/`
once that folder has its manifest (`go.mod`, `package.json`, `pyproject.toml`). The backend job
also fails when the code generated from `docs/openapi.yaml` is not up to date.

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

- **Go:** entry point in `backend/cmd/api`; modules in `backend/internal/<module>`, where the
  modules are auth, profile, social, project, event and notification. Return errors, do not
  panic. Table-driven tests beside the code.
- **API:** JSON under `/api/v1`. Errors have the shape `{"code": "...", "message": "..."}`.
  Lists are paginated and the server caps the page size at 50.
- **Database:** snake_case names, plural table names, uuid primary keys, `timestamptz` for
  times. Rules that must always hold are constraints in the database.
- **Git:** one branch per slice, named `slice-NN-short-name`. Small commits with messages in
  the imperative ("Add connection requests").
