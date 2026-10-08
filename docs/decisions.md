# Decisions

One entry per decision: the date, what was decided and why. Add to the end; do not rewrite
old entries. To change a decision, add a new entry that replaces the old one.

| Date | Decision | Reason |
| --- | --- | --- |
| 2026-10-05 | The API server is one Go application divided into modules (a modular monolith). Only the sync, compile and AI services are separate. | One developer; fewer moving parts to deploy and debug. A module can become its own service later. |
| 2026-10-05 | Embeddings are stored in PostgreSQL with pgvector, not in a separate vector database. | One database to run and back up; enough for 10,000 profiles (NFR-1). |
| 2026-10-07 | Requirements live in `docs/SRS.md`, and every slice names the requirement IDs it covers. | Keeps code traceable to requirements. |
| 2026-10-07 | The local PostgreSQL container listens on port 5433. | Port 5432 may already be used by a PostgreSQL installed on Ubuntu. |
| 2026-10-08 | Top-level folders are `frontend/`, `backend/`, `ai/`, `sync/`, `compile/` and `docs/`. | Each service is easy to find, and each can be built and deployed on its own. |
| 2026-10-08 | Project instructions for AI assistants are in `AGENTS.md`. The repository has no `CLAUDE.md`. | Claude Code reads `AGENTS.md` when no `CLAUDE.md` exists; one file avoids two sets of rules. |
| 2026-10-08 | CI is one GitHub Actions workflow, `.github/workflows/ci.yml`. A `detect` job runs the backend, frontend and ai jobs only when `go.mod`, `package.json` or `pyproject.toml` exists, and fails when a folder has source files but no manifest. | Jobs stay green before any code exists, and a missing manifest cannot make real code skip its checks. |
| 2026-10-08 | CI uses golangci-lint v2.14.0. Actions are pinned to a major version, except `astral-sh/setup-uv`, which is pinned to an exact release because it publishes no major tags. | Same linter version as on the developer's machine; major pins get fixes without surprise breaking changes. |
| 2026-10-09 | Embeddings are `vector(768)`. A model with another output size needs a new migration and a full re-embed. | 768 fits SPECTER2 and all-mpnet-base-v2, so either can be chosen in the AI-1 slice; HNSW indexes need a fixed size. |
| 2026-10-09 | Sessions (refresh tokens) and one-time tokens (verification, reset) are PostgreSQL tables, added to SRS section 7. Login lockout (FR-2.7) and rate limits (NFR-8.3) are Redis counters, not tables. | Sessions must survive a Redis restart (30-day tokens); counters are short-lived and expire on their own. |
| 2026-10-09 | "Exactly one owner per project" is enforced in the database: a partial unique index allows at most one, and a deferred constraint trigger requires one at commit. | The SRS requires the rule in the database. Deferring the check lets a project and its owner, or an ownership transfer, be written in one transaction. |
| 2026-10-09 | Down migrations remove everything their up file created, extensions included. CI applies all migrations, undoes them, checks that the public schema is empty, and applies them again. CI installs golang-migrate v4.18.3 and checks its sha256. | NFR-11.1 requires migrations that can be undone; the check catches a down file that misses something. |
