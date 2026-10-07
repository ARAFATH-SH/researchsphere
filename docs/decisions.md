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
