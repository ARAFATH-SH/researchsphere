# ResearchSphere: instructions for AI coding assistants

ResearchSphere is a web platform where researchers build a profile, find collaborators
through AI matching, write papers together in a shared LaTeX editor and get venue
recommendations. One developer builds it in 30 slices over 12 weeks.

## Read first

- `docs/SRS.md` says what to build. Requirement IDs such as FR-7.2, AI-2.1 and NFR-4
  refer to it. When code and the SRS disagree, stop and ask which one changes.
- `docs/openapi.yaml` is the API contract (added in slice 4). Change it before a handler.
- `docs/decisions.md` lists decisions already made. Do not reopen one without asking.
- `docs/email-delivery.md` says how mail is sent and how to switch provider (FR-17.1).
- `docs/evaluation.md` defines the AI quality and speed measurements (NFR-1, NFR-6), the
  recommendation score calibration (AI-2.2), the real-time editing load test (NFR-2, NFR-10.1,
  NFR-12) and their latest results.
- `docs/deploy.md` lists the one-off commands to run after a deployment applies certain migrations.
- `docs/realtime.md` is the WebSocket contract: events, payloads, close codes and what a client
  must do after a reconnect. OpenAPI covers only the upgrade.
- `docs/sync.md` is the real-time editing contract (slice 22): the ticket, the token, the
  `session` message, refusal and close reasons, epochs and offline edits, saving, limits.
- `docs/venues-import.md` is the venue import file (FR-14.5, interim): columns, rules, time zones
  (AoE), matching by key and the research fields.
- `docs/compile.md` is the compile contract (slice 23): jobs and their states, retention, the
  worker's internal operations, the sandbox's settings, error parsing, downloads and the threat
  model.

## Layout

| Folder | Holds | Stack |
| --- | --- | --- |
| `frontend/` | Web client | Next.js, React, TypeScript, Tailwind CSS |
| `backend/` | API server; migrations in `backend/db/migrations` | Go 1.25+, sqlc, oapi-codegen, golang-migrate |
| `ai/` | Internal AI service: embeddings now (`POST /embed`, and `POST /embed-document`, which splits a long text by real tokens and averages; intfloat/multilingual-e5-base, CPU), later matching, search, venue ranking and the writing assistant. Never public: the API calls it with `AI_SERVICE_TOKEN` (`internal/platform/aiclient`) | Python 3.12 with uv, FastAPI, Sentence Transformers |
| `sync/` | Real-time editing server (slice 22, `docs/sync.md`): one Yjs document per text file; reaches the database only through the API's internal operations (`/api/v1/internal/sync/*` on `INTERNAL_API_ADDR`, `SYNC_SERVICE_TOKEN`) | Node.js 22 (TypeScript run directly), Hocuspocus 4, Yjs, ioredis |
| `compile/` | Compile worker (slice 23, `docs/compile.md`): claims compile jobs on the API's internal listener (`/api/v1/internal/compile/*`, `COMPILE_SERVICE_TOKEN`) and runs each in its own sandbox container (`compile/image`: TeX Live full, pinned by digest, `FROM scratch` with a minimal `/etc`; every compile runs under `rs-landlock`, which limits it to its project and TeX Live with Landlock ABI 8, and the worker refuses to start without it); holds the Docker socket and no other secret | Go 1.26, standard library only; Docker Engine API; TeX Live 2026, latexmk |
| `docs/` | SRS, API contract, decisions | Markdown, OpenAPI 3 |
| `design/` | Reference design: one HTML artboard per screen, desktop and `_m` mobile (not served, not imported) | HTML, CSS |

Data stores: PostgreSQL 16 with pgvector, Redis, and S3-compatible object storage (MinIO
locally, console at http://localhost:9001). All run in Docker, with Mailpit for local email
and ClamAV (clamd), which scans message attachments for malware. The AI service (`ai/`) runs
in Docker too; its model (about 1.1 GB) is downloaded into the `aimodels` volume on the first
start, not baked into the image.

## Commands

Run these from the repository root. `make help` prints the current list.

| Command | What it does |
| --- | --- |
| `make up` / `make down` | Start or stop PostgreSQL, Redis, Mailpit (local email inbox at http://localhost:8025), MinIO and ClamAV; `make up` also creates the private bucket, and starts ClamAV last without waiting for it (clamd takes 30 to 90 seconds and 1.5 to 3 GB of memory to load its signatures; attachments stay pending until it answers). `make up CLAMAV=0` leaves ClamAV out, as the CI stack job does. It also starts the AI service without waiting (building its image the first time; the first start downloads the model, and `GET /health` says `degraded` until it is loaded); `AI=0` leaves it out |
| `make check` | Confirm that PostgreSQL, pgvector, Redis and storage respond, that the bucket refuses anonymous access and that storage accepts uploads from the web client (CORS for `http://localhost:3000`, set by `make up`); show whether ClamAV and the AI service answer yet (not a failure). When the compile image is built, also run the sandbox's Landlock probe and fail if this kernel cannot enforce it (no compile would run) |
| `make psql` | Open a psql prompt in the database |
| `make migrate` / `make migrate-down` | Apply all migrations, or undo the latest one |
| `make migrate-new name=add_users` | Create a new migration pair |
| `make admin email=...` | Give that account the admin role (the only way to make an administrator; there is no API for it). Prints a message and fails if no account has the address. Takes effect at the user's next token refresh or login |
| `make unadmin email=...` | Set that account back to the researcher role |
| `make seed-ui-compare` | Create or refresh the fake accounts `npm run ui:compare` signs in with: Compare Alpha and Compare Beta, connected, with about 40 messages over several days and Beta's open report of a message from Alpha, Compare Gamma's pending message request to Alpha, and Compare Admin, an administrator (`@compare.researchsphere.local`). Needs `make migrate` and `UI_COMPARE_PASSWORD`. Refuses a database name ending in `_test`, `APP_ENV=production` and any host but this machine |
| `make seed-dev` | Create or repair the development accounts `test1@example.com` to `test4@example.com` (researchers) and `admin@example.com` (administrator), all verified, with the password `DEV_SEED_PASSWORD` from `.env`. Running it again resets their password, role and suspension and touches no other account. Needs `make migrate`. Refuses a database name ending in `_test`, `APP_ENV=production` and any host but this machine. The API accepts these addresses only while `TEST_EMAIL_DOMAINS` names `example.com` |
| `make sample-data` | Load deterministic sample profiles into the development database (slice 16): `n=10000` sets how many there should be (default 1,000), `wait=1` then waits until the running API (`make api`, AI service up) has embedded them all, printing progress. `projects=1` also makes one sample project per 10 sample profiles (slice 19), owned by profile 10k and in its cluster, with its labels in `sample_projects`. `venues=1` also makes 200 fictional venues (slice 24), 10 per cluster, with deadlines from 90 days before the run to 365 after, marked in `sample_venues`. The same command makes the same data (seed 16); a second run tops up to `n` and never duplicates. Accounts are `sample-NNNNN@sample.researchsphere.local`, verified, with the one password `SAMPLE_PASSWORD` from `.env`; their research clusters and tuning/test split are in `sample_profiles`. Needs `make migrate`. Refuses unless `APP_ENV=development`, and refuses a database name ending in `_test` and any host but this machine. Never in production |
| `make sample-purge` | Delete every sample account (a `sample_profiles` row and an address at `sample.researchsphere.local`, both) through the account deletion of FR-2.8, one transaction each, then every sample venue (a `sample_venues` row) for good; touches no other account or venue |
| `make purge-unverified` | Run the FR-17.4 purge once now, with the `.env` settings: delete the accounts still unverified `UNVERIFIED_ACCOUNT_DAYS` (7) after sign-up, except those at `TEST_EMAIL_DOMAINS` outside production, and log what it deleted. The API also runs it every hour |
| `make send-digests` | Run the FR-16.11 unread message digest once now, with the `.env` settings: email each verified, not suspended user with the digest on who has messages unread for 24 hours that no digest counted yet and got no digest in the last 24 hours (at most 200 users a run), and log how many it sent. The API also runs it every hour. Mail goes to Mailpit locally (test-domain accounts included); needs `make migrate` and `UNSUBSCRIBE_SECRET` |
| `make embed-backfill` | Queue every verified, not suspended profile, every project and every venue not removed, for its AI embedding once (AI-1.5), at low priority, then exit; the running API's worker embeds them, skipping a profile whose text and model have not changed. Run it once after migration 000028 on a database with existing profiles. Needs `make migrate`; the API needs the AI service (`AI_SERVICE_TOKEN`) to embed |
| `make backfill-main-tex` | Give the starting `main.tex` (FR-10.4) to every project that has no file, one transaction per project, and print how many. Idempotent: a project with any file is left alone. Run it once in every environment after migration 000035 (`docs/deploy.md`). Needs `make migrate` |
| `make calibrate-recs` | Fit the AI-2.2 recommendation score map on the sample TUNING split only (2,000 queries, seed 17) and print `RECS_SCORE_MODEL`, `RECS_SCORE_SLOPE` and `RECS_SCORE_INTERCEPT` with a calibration table; then check that map on profile-project pairs of the tuning split, both directions (slice 19), and print `RECS_PROJECT_SCORE_SLOPE` and `RECS_PROJECT_SCORE_INTERCEPT` when it does not hold; put them into the config defaults and `.env.example`. Needs the sample projects too (`make sample-data projects=1`). Run it again whenever the embedding model changes (`docs/evaluation.md`). Needs the 10,000 embedded sample profiles and `APP_ENV=development`; reads only |
| `make eval-recs` | Measure NFR-6 collaborator matching on the held-out sample TEST split: recall@10 (capped), precision@10 and standard recall@10 against "relevant = same main cluster" and, beside it, "shares the main or second cluster", ranked as the endpoint ranks, with the score percentiles of random and top-10 pairs per group; then the same for project matching in both directions (AI-3.1 profile -> project, AI-2.3 project -> profile). Write the results into `docs/evaluation.md`. Never tune on its output. Needs `APP_ENV=development`; reads only |
| `make bench-recs` | Measure NFR-1: the recommendation endpoint's uncached work for 200 random sample users (seed 17), printing p50, p95 and max (target p95 under 2 s), and EXPLAIN ANALYZE of the ranking query; then the same for both project matching endpoints (200 users, 200 projects). Needs `APP_ENV=development`; reads only |
| `make eval-search` | Measure AI-5.2 semantic researcher search on the held-out sample TEST split: precision@10 ("relevant = same main cluster") of the 40 queries in `backend/cmd/recseval/search_queries.json` (one English, one Bangla per cluster, sharing no word with the cluster's vocabulary), hybrid against keyword mode. Write the results into `docs/evaluation.md`. Never change a query after seeing its result. Needs the 10,000 embedded sample profiles, `APP_ENV=development` and the AI service (`AI_SERVICE_TOKEN`); reads only |
| `make bench-search` | Measure NFR-1 for hybrid researcher search: the usecase's work for 200 search texts (seed 17), p50, p95 and max with a cold and a cached query vector (target p95 under 2 s, embedding included), and EXPLAIN ANALYZE of the meaning query. Needs `APP_ENV=development` and the AI service; reads only |
| `make purge-compile-output before=<time> [apply=1]` | Delete the PDF and the log of every compile that started before `before` (RFC 3339 with its zone, such as `2026-11-23T21:10:27+06:00`): the PDF's `stored_files` row and its object in storage (drained at once), and the log column; the records, status and errors stay. Without `apply=1` it only lists them. Run it after a sandbox fix with the time the fixed image went live, in every environment (`docs/deploy.md`). Reads the API's settings from `.env` |
| `make update-disposable-domains` | Download the latest list of disposable email domains (FR-17.2, CC0) into `backend/internal/auth/adapter/emaildomain/disposable_domains.txt`; review and commit the result |
| `make api` | Run the API server (needs `make up`; settings from `.env`) |
| `make web` | Run the web client dev server on port 3000; it forwards `/api/v1` to `API_URL` |
| `make stop` | Stop this project's dev servers: the API on `API_PORT` (8080) and the Next.js dev server on ports 3000-3002. Stops only your own processes running inside this repository, prints what it stopped, and removes a stale `frontend/.next/dev/lock` when no dev server is left. Leaves the Docker containers running (use `make down` for those) |
| `make web-clean` | Delete the web client build cache (`frontend/.next`). Stop `make web` first (`make stop`); run it after switching branches if styles look broken |
| `make ai-build` | Build the AI service image (CPU only, about 2 GB, without the model) |
| `make ai-up` | Start the AI service and wait until its model is loaded. The first start downloads the model at its pinned revision (about 1.1 GB) into the `aimodels` volume; later starts and rebuilds reuse it. Needs `AI_SERVICE_TOKEN` in `.env` |
| `make ai-fetch-model` | Download the model into the `aimodels` volume ahead of time, then exit |
| `make ai-logs` | Follow the AI service's log |
| `make sync` | Run the real-time editing server on `SYNC_PORT` (8091). Needs `cd sync && npm ci`, `make up`, and `make api` with `SYNC_SERVICE_TOKEN` set (its internal listener on `INTERNAL_API_ADDR`) |
| `make sync-build` | Build the sync server's image (compose profile `sync`) |
| `make compile-image` | Build the compile sandbox image (`compile/image`; TeX Live full pinned by digest: 2.8 GB to pull the first time, about 9 GB on disk) as `COMPILE_IMAGE` (default `researchsphere-compile:dev`) |
| `make compile-worker` | Run the compile worker on this host with your Docker (`docs/compile.md`). Needs `make compile-image`, and `make api` with `COMPILE_SERVICE_TOKEN` set (its internal listener on `INTERNAL_API_ADDR`); without a worker every compile answers `compile_unavailable` |
| `make compile-trace dir=<folder> [main=main.tex] [out=<folder>]` | Development only: compile one project folder in the sandbox under strace (`compile/image/debug`: the sandbox image plus strace, built on demand; the worker's own container settings) and list every path refused (Landlock's EACCES, EROFS), every write outside `/work` and `/tmp`, the writes under `/tmp` by folder and the devices used; `out=` keeps the raw trace, the log and the PDF. Use it when a document fails in the sandbox but not elsewhere. Needs `make compile-image` |
| `make bench-compile [runs=5]` | Measure NFR-3: compile the 10-page sample paper (`compile/bench/sample`) in the real sandbox and print the times (target under 20 s). Needs `make compile-image` |
| `make sync-load [seconds=60]` | Measure NFR-2, NFR-10.1 and NFR-12: 10 files x 4 simulated editors typing, against the sync server in its own process and an in-memory fake API; prints update latency and save delay percentiles and fails above p95 1 s, a save over 5 s or a lost edit. Needs `make up`; empties `TEST_REDIS_URL` |
| `make generate` | Regenerate Go code in `backend/internal/api` and TypeScript types in `frontend/src/lib/api` from `docs/openapi.yaml`, and the sqlc query code in `backend/internal/*/adapter/postgres/sqlcgen` |
| `make check-generated` | Regenerate and fail if the generated code differs from what is committed |
| `make test` | Run all tests: `test-prepare`, `test-db`, `test-go`, `test-integration`, `test-web`, `test-ai`, `test-sync` and `test-compile` (needs `make up`) |
| `make test-prepare` | Create the test database `researchsphere_test` (`TEST_POSTGRES_DB`) in the same container if it is missing and apply all migrations to it. Refuses a name that does not end in `_test` |
| `make test-db` | Run the database constraint tests on the test database (runs `test-prepare` first; needs `make up`) |
| `make test-go` | Run the backend Go tests (`go test -race`); needs no database |
| `make test-integration` | Run the Go repository tests and the end-to-end tests in `cmd/api` (build tag `integration`) against the test database (`TEST_DATABASE_URL`), Redis DB 15 (`TEST_REDIS_URL`) and MinIO, one package at a time (`-p 1`). Runs `test-prepare` first; needs `make up` |
| `make test-one pkg=./internal/x/ run=TestName [count=1]` | Run one integration test, or the tests `run` matches (a `go test -run` pattern), with the same settings as `test-integration`: `-race`, `-p 1`, the build tag and the test environment. `pkg` is relative to `backend/`; `count` repeats it to find a flaky test. Runs `test-prepare` first; needs `make up` |
| `make test-web` | Run the frontend tests (Jest) |
| `make test-ai` | Run the AI service tests (pytest) with a stub model: no torch, no model download (needs uv) |
| `make test-sync` | Run the sync server tests (`node:test`): real Hocuspocus servers and providers against Redis `TEST_REDIS_URL` (never DB 0 or `REDIS_URL`'s; emptied) and an in-memory fake of the API's internal operations; one suite restarts the server as a child process. Needs `make up` and `cd sync && npm ci` |
| `make test-compile` | Run the compile worker's tests (`go test -race`) with a fake Docker daemon; needs no image |
| `make test-compile-sandbox` | Run the sandbox tests against real Docker and the image (build tag `sandbox`): the regression corpus (`compile/testdata/corpus`: a RenderCV-style CV, a paper with BibTeX and TikZ, a beamer deck, PNG and PDF figures, biblatex with biber, a METAFONT font) compiles cleanly; no network, no shell escape, no file outside `/work` read (`\input`, `\openin`, `\includegraphics`, `\lstinputlisting` and the pdfTeX file primitives on `/etc/passwd`, `/proc/self/environ` and `../` paths, checked in the log and the PDF's text, with a positive control) or written, the TeX and Landlock settings inside the sandbox, the 60 s kill, output caps, a write bomb, cleanup after every run, a multi-file BibTeX paper. About 90 seconds. Needs `make compile-image`; not in `make test` (CI runs it nightly, by hand and on compile changes) |
| `make lint` | Run all linters: `lint-go`, `lint-web`, `lint-ai`, `lint-sync` and `lint-compile` |
| `make lint-go` | Run golangci-lint for the backend |
| `make lint-web` | Run ESLint and the TypeScript type check for the frontend |
| `make lint-ai` | Run ruff (lint and format check) for the AI service |
| `make lint-sync` | Run ESLint and the TypeScript type check for the sync server |
| `make lint-compile` | Run golangci-lint for the compile worker (`compile/.golangci.yml`) |

The frontend and sync targets need their packages: run `cd frontend && npm ci` and
`cd sync && npm ci` once, and again after their `package-lock.json` changes. When a slice adds a command to the Makefile, add it to this
table in the same change.

CI (`.github/workflows/ci.yml`) runs on every pull request to `main` and every push to `main`
(NFR-7), not on pushes to other branches. A `detect` job runs first; when a change touches only
`docs/**`, `design/**` or Markdown files (never `docs/openapi.yaml`), every other job shows as
skipped. Next, in parallel, the fast checks lint and test each of `backend/`, `frontend/`,
`ai/`, `sync/` and `compile/` once that folder has its manifest (`go.mod`, `package.json`,
`pyproject.toml`, `package.json`, `go.mod`); the sync job runs its tests against a Redis service container. The
backend job also fails when the code generated from `docs/openapi.yaml` is not up to date; the
frontend job runs `npm ci`, checks that the generated API types are up to date, then runs lint,
type check, tests and a production build. Only when they pass does the stack job start
PostgreSQL, Redis, Mailpit and MinIO (`make up CLAMAV=0 AI=0`), run `make check`, apply all
migrations, undo them, check that nothing is left, apply them again, prepare the test database
(`make test-prepare`) and run `make test-db` and the Go repository tests (`make test-integration`).
The `clamav` job scans the EICAR string with a real clamd only on push to `main`, nightly and
by hand. The `ai` job tests the AI service with a stub model; the `ai-model` job builds its
image, downloads the real model and runs `ai/scripts/smoke.py` only nightly and by hand. The
`compile-sandbox` job builds the TeX Live sandbox image (cached by the hash of `compile/image`)
and runs `make test-compile-sandbox`'s tests and the NFR-3 benchmark nightly, by hand, and on
pull requests and pushes that change `compile/` or the API's compile code. The
job names (`Detect`, `Backend (Go)`, `Frontend (Node.js)`, `AI (Python, uv)`,
`Sync (Node.js)`, `Compile (Go)`, `Compile sandbox (TeX Live)`, `Stack (integration)`,
`ClamAV (EICAR)`, `AI model (e5)`) are the status check names branch protection uses:
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
  then auth, profile, media, social, project, event, notification, messaging, moderation, realtime,
  embedding (AI-1: the queue, the worker and the stored vectors), event (FR-14, FR-15.1: venues,
  their research fields, the administrators' changes and audit trail, the file import), recommendation (AI-2,
  AI-3: collaborator and project recommendations from the embeddings) and compile (FR-13: the
  compile jobs, their logs and PDFs; the worker that runs them is `compile/`). Return errors, do not panic.
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
  elsewhere) and keeps the client's refresh and lost-session handling. Beside it, `postToStorage`
  sends a project figure straight to object storage with the presigned POST the API signed
  (FR-12.2), without cookies: the one request that leaves the app's origin besides the WebSockets.
  The third is the sync server's WebSocket at `NEXT_PUBLIC_SYNC_URL` (`docs/sync.md`): only
  `features/project/workspace/collab/link.ts` imports `@hocuspocus/provider`, and only
  `features/project/workspace/compile/pdf/load-pdf.ts` imports `pdfjs-dist` (the PDF viewer, slice 23). Every session refresh goes through `refreshSession` in `lib/api/client.ts`,
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
  `cd frontend && npm run e2e:figures` (dev only, needs `make api` and `make web`, signs in with
  `E2E_EMAIL`/`E2E_PASSWORD` or the `UI_COMPARE_` pair) uploads figures in a real Chrome
  through the whole flow, MinIO's CORS and the malware scan included, and deletes them after.
  `cd frontend && npm run e2e:collab` (dev only, needs `make seed-dev`, `make api`, `make sync` and
  `make web`) opens main.tex of a throwaway project in five Chrome contexts, four editors typing at
  once and a viewer, checks that every text matches, the viewer cannot type and an edit reaches the
  others within 1 s (NFR-2), and deletes the project after, also when a check fails.
- **Database version:** the API refuses to start on a database whose migrations are not all
  applied (`requiredSchemaVersion` in `backend/cmd/api/schema.go`; a test keeps it equal to the
  newest file in `backend/db/migrations`, so a new migration raises it).
- **API:** JSON under `/api/v1`. Errors have the shape `{"code": "...", "message": "..."}`;
  a `bad_request` about one body field or parameter adds `"field"`, and never repeats the value.
  Lists are paginated and the server caps the page size at 50 (a conversation's messages cap at 100;
  a project's file tree is one unpaginated answer of at most 200 entries, `docs/decisions.md`).
  A session whose email address is not verified gets 403 `email_not_verified` on every route
  except those in `unverifiedAllowed` (`cmd/api/handlers.go`, FR-17.3): a new route needs a
  verified email unless it is added there, and only within FR-17.3. The sync server's and the
  compile worker's operations (`/api/v1/internal/`, OpenAPI tag `internal`, security `syncToken`
  or `compileToken`) take no session: `cmd/api` serves them only on the internal listener
  (`INTERNAL_API_ADDR`), each with its own service's bearer token and never the other's
  (`httpx.RequireServiceTokens`), and the public port answers 404 for them (`docs/sync.md`,
  `docs/compile.md`). Only the compile worker ever gets the Docker socket.
- **SQL:** each module's queries are in `internal/<module>/adapter/postgres/queries.sql`; sqlc
  generates `sqlcgen/` beside it (`backend/sqlc.yaml`, run by `make generate`). Repository tests
  that need the database carry the build tag `integration`.
- **Integration tests never touch development data.** They get their connections from
  `internal/platform/testenv`: `testenv.DB(t)` connects to `TEST_DATABASE_URL` and empties every
  table but `research_fields` (reference data the migrations seed), `testenv.Redis(t)` connects to `TEST_REDIS_URL` (DB 15) and flushes that DB only. Both
  refuse to run unless the database name ends in `_test` and the Redis DB is not 0 or the one
  `REDIS_URL` uses. Call each once per test, before opening any transaction (the truncate waits
  for open transactions and fails after 10 seconds). Do not use `t.Parallel()` in integration
  tests, and do not add row or key cleanups: the next test starts empty. Only objects written
  to MinIO need cleaning up. Use `testenv.Word`, `testenv.Email` and `testenv.ORCIDiD` for
  names, addresses and ORCID iDs rather than fixed values.
- **Sample data (slice 16) stays out of the application and out of production.** The
  sample accounts of `make sample-data` are at `@sample.researchsphere.local`, which
  `TEST_EMAIL_DOMAINS` lists outside production. `sample_profiles` is their marker and the
  NFR-6 ground truth, `sample_projects` labels the sample projects of `projects=1` the same
  way, and `sample_venues` marks the fictional venues of `venues=1` with their cluster: no code
  under `internal/` reads any of them, only `cmd/seedsample` writes them, and
  only `cmd/seedsample` and `cmd/recseval` (the AI-2.2 calibration on the `tuning` split, the
  NFR-6 and AI-5.2 evaluations on the `test` split) read them. For the NFR-6 collaborator matching
  evaluation (slice 17), a candidate is relevant to a query profile from the `test` split when
  it has the same `main_cluster`; nothing is ever fitted on the `test` split. The auth usecase's
  `DeleteAccountByID` (deletion without a password) is for commands such as the sample purge;
  `TestNoRouteDeletesAccountsByID` fails when `internal/` or `cmd/api` calls it.
- **Database:** snake_case names, plural table names, uuid primary keys, `timestamptz` for
  times. Rules that must always hold are constraints in the database.
- **Git:** one branch per slice, named `slice-NN-short-name`. Small commits with messages in
  the imperative ("Add connection requests").
