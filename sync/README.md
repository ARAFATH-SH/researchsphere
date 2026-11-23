# Sync: real-time editing server (slice 22)

Hocuspocus 4 and Yjs on Node.js 22. Members edit a text file together (FR-12.4); the server
checks who they are with a one-time ticket from the API and reads their role again every 60 s
and on every membership event (FR-12.7), lets at most six edit at once (FR-12.8), merges offline
edits (FR-12.9) and stores the text through the API within 5 s (NFR-10.1). It never reads the
database itself: it calls the API's internal operations with `SYNC_SERVICE_TOKEN`.

The contract (URL, token, messages, close reasons, limits) is `docs/sync.md`; the reasons are in
`docs/decisions.md` (2026-11-19); deployment in `docs/deploy.md`.

## Run

```bash
cd sync && npm ci       # once, and after package-lock.json changes
make up                 # Redis (and the rest)
make api                # with SYNC_SERVICE_TOKEN in .env: starts the internal listener on 8081
make sync               # this server on 8091
```

Node runs the TypeScript sources directly (type stripping, Node 22.18 and later); there is no
build step. `npm run typecheck` checks the types.

## Layout

| File | Holds |
| --- | --- |
| `src/main.ts` | Entry point: settings, Redis, SIGTERM drain |
| `src/server.ts` | The Hocuspocus hooks: tickets, rights, limits, load and store, events, health |
| `src/sessions.ts` | Who has each document open: the six-editor rule, colours, promotions |
| `src/api.ts` | The API's internal operations |
| `src/live.ts` | The Redis live mark that keeps REST saves out while a file is open |
| `src/config.ts`, `src/log.ts`, `src/limits.ts` | Settings, JSON-lines logging, the message rate bucket |
| `test/` | `node:test` suites with real providers, Redis DB 15 and an in-memory fake of the API |
| `scripts/load.ts` | The NFR-2 / NFR-12 load test (`make sync-load`) |

## Check

```bash
make lint-sync test-sync   # ESLint, tsc, tests (Redis from make up; TEST_REDIS_URL, DB 15)
make sync-load             # 10 files x 4 editors for 60 s; prints latency and save delay
```
