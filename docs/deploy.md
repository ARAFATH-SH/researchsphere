# Deploy checklist

Steps to run by hand when a deployment applies the migrations named below. Each command is
safe to run again: a second run changes nothing. Run them from the repository root with the
environment's `.env` (or its variables) loaded, after `make migrate` has applied the
migrations, and before announcing the release.

| After migration | Run | What it does | Why |
| --- | --- | --- | --- |
| 000028 (embedding pipeline) | `make embed-backfill` | Queues every verified, not suspended profile and every project for its AI embedding once (AI-1.5); the running API embeds them | Profiles and projects saved before the pipeline have no embedding until they change |
| 000035 (project files) | `make backfill-main-tex` | Gives the starting `main.tex` (FR-10.4) to every project that has no file, with its title and its owner's name; prints how many | Projects created before slice 21 have no files; new projects get `main.tex` when they are created |

## Adding a step

A slice that needs a one-off step after its migration adds a row here, a `make` target that
is idempotent, and the target to the command table in `AGENTS.md`.
