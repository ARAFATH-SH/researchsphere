# Deploy checklist

Steps to run by hand when a deployment applies the migrations named below. Each command is
safe to run again: a second run changes nothing. Run them from the repository root with the
environment's `.env` (or its variables) loaded, after `make migrate` has applied the
migrations, and before announcing the release.

| After migration | Run | What it does | Why |
| --- | --- | --- | --- |
| 000028 (embedding pipeline) | `make embed-backfill` | Queues every verified, not suspended profile and every project for its AI embedding once (AI-1.5); the running API embeds them | Profiles and projects saved before the pipeline have no embedding until they change |
| 000035 (project files) | `make backfill-main-tex` | Gives the starting `main.tex` (FR-10.4) to every project that has no file, with its title and its owner's name; prints how many | Projects created before slice 21 have no files; new projects get `main.tex` when they are created |

## Settings and services for direct figure uploads (from migration 000036)

Figures go from the browser straight to the bucket with a presigned POST (FR-12.2,
`docs/decisions.md` 2026-11-18). Before the release that applies 000036:

- **CORS on the bucket.** The browser POSTs to `S3_ENDPOINT` from the web app's origin, so the
  bucket must allow `POST` from `APP_BASE_URL` (and only from it), with the request header
  `content-type`; no credentials. Locally `make up` sets MinIO's `api cors_allow_origin` to the
  web client's origins (this MinIO build has no per-bucket CORS); AWS S3, R2 and B2 need a CORS
  rule on the bucket, MinIO the same server setting. `make check` checks it locally. `S3_ENDPOINT` must be reachable from
  users' browsers, as presigned downloads already require.
- **clamd's limits.** Production clamd needs `StreamMaxLength` and `MaxFileSize` of at least
  `FIGURE_MAX_BYTES` (210M as in `docker-compose.yml`), `MaxScanSize` 420M and
  `AlertExceedsMax yes`, and `CLAMAV_MAX_BYTES` set to the same stream limit; the API refuses
  to start with `FIGURE_MAX_BYTES` above `CLAMAV_MAX_BYTES`.
- **Limits.** `FIGURE_MAX_BYTES` (default 50M, at most 200M) and `PROJECT_STORAGE_MAX_BYTES`
  (default 500M) are optional.

Migration 000036 needs no command. Its down migration deletes figures over 5 MB, as the old
limit cannot hold them.

## Adding a step

A slice that needs a one-off step after its migration adds a row here, a `make` target that
is idempotent, and the target to the command table in `AGENTS.md`.
