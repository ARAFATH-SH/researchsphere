# Deploy checklist

Steps to run by hand when a deployment applies the migrations named below. Each command is
safe to run again: a second run changes nothing. Run them from the repository root with the
environment's `.env` (or its variables) loaded, after `make migrate` has applied the
migrations, and before announcing the release.

| After migration | Run | What it does | Why |
| --- | --- | --- | --- |
| 000028 (embedding pipeline) | `make embed-backfill` | Queues every verified, not suspended profile and every project for its AI embedding once (AI-1.5); the running API embeds them | Profiles and projects saved before the pipeline have no embedding until they change |
| 000035 (project files) | `make backfill-main-tex` | Gives the starting `main.tex` (FR-10.4) to every project that has no file, with its title and its owner's name; prints how many | Projects created before slice 21 have no files; new projects get `main.tex` when they are created |
| 000041 (title-and-abstract embeddings) | `make embed-backfill` | Queues every profile, project and venue once (AI-1.5); the running API's worker gives each project with an abstract its new title-and-abstract vector and skips every embedding whose text and model have not changed | Venue recommendations (AI-6.1) rank by this vector; a project saved before 000041 has none, so its members see `processing` until it is made or the project changes |

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

## Real-time editing: the sync server (from migration 000037)

Slice 22 adds the sync server (`sync/`, `docs/sync.md`). Migration 000037 needs no command.
Before the release that applies it:

- **Reverse proxy.** Route `/sync` on the web client's domain to the sync server (port 8091),
  as a WebSocket: pass `Upgrade` and `Connection` through, keep connections open (read timeout
  above 60 s), and do not buffer. With nginx:

  ```nginx
  location /sync {
      proxy_pass http://sync:8091;
      proxy_http_version 1.1;
      proxy_set_header Upgrade $http_upgrade;
      proxy_set_header Connection "upgrade";
      proxy_set_header Host $host;
      proxy_read_timeout 120s;
      proxy_buffering off;
  }
  ```

  The browser then connects to `wss://<domain>/sync`. Keep the `Origin` header as the browser
  sent it: the sync server refuses any origin but `APP_BASE_URL`'s.
- **The API's internal listener.** `INTERNAL_API_ADDR` (default `127.0.0.1:8081`) serves only
  the sync server's operations, with `SYNC_SERVICE_TOKEN`. Bind it to the private network the
  two services share, never to a public interface, and do not route it through the proxy (the
  public port answers 404 for `/api/v1/internal/` anyway).
- **Settings.** `SYNC_SERVICE_TOKEN` (at least 32 characters, different from the other secrets;
  `openssl rand -base64 48`) in both the API and the sync server; the API refuses to start in
  production without it. The sync server also needs `REDIS_URL` (the API's Redis, same database:
  the live marks and the events live there), `API_INTERNAL_URL` (the internal listener,
  `http://<api-host>:8081/api/v1`), `APP_BASE_URL`, `SYNC_HOST=0.0.0.0` in a container and
  `SYNC_PORT` (8091).
- **Stopping.** The sync server stores every open document on SIGTERM before it exits. Give it
  at least 30 s (`stop_grace_period`, Kubernetes' `terminationGracePeriodSeconds`); a hard
  kill loses nothing members' clients still hold, but they must reconnect to send it again.
- **One instance.** Run exactly one sync server for now (`docs/sync.md`, Scaling).

## Compile: the worker and its sandbox (from migration 000038)

Slice 23 adds the compile worker (`compile/`, `docs/compile.md`). Migration 000038 needs no
command. Before the release that applies it:

- **A compile host.** The worker needs a Docker daemon, and whoever uses its socket is root on
  that host. Run the worker on a host of its own that holds nothing else (no database, no
  storage credentials, no other service), with rootless Docker, and with gVisor's `runsc`
  installed when the host allows it (`COMPILE_RUNTIME=runsc`). Never give the API, or any other
  service, the Docker socket. Each compile needs 1 CPU and 1 GB besides the worker's own
  memory: size the host for `COMPILE_CONCURRENCY` compiles (default 2).
- **Landlock on the compile host's kernel (required).** Every compile runs under Landlock, so
  that LaTeX reads only its project and TeX Live (TeX Live 2026 made `openin_any` a no-op,
  `docs/decisions.md`). The kernel must offer **Landlock ABI 8 or later**: built with
  `CONFIG_SECURITY_LANDLOCK=y` and with `landlock` in its active LSMs (`cat
  /sys/kernel/security/lsm`; otherwise add it to the `lsm=` boot parameter). ABI 8 is the one
  that added `LANDLOCK_RESTRICT_SELF_TSYNC`; the kernel's Landlock documentation names no
  release for it, so check the host rather than its version number. Linux 7.0 (Ubuntu
  7.0.0-43) with Docker 29.8.1 and its default seccomp profile reports ABI 8. Check with
  `make check` on the compile host (after `make compile-image`), which runs the sandbox's probe
  and fails when the kernel cannot enforce the rules. **Without it no compile runs**: the
  worker refuses to start ("refusing to start: sandbox: the sandbox cannot enforce its file
  rules …"), and `POST /projects/{id}/compiles` answers 503 `compile_unavailable`. gVisor's
  `runsc` is not known to implement Landlock; with `COMPILE_RUNTIME=runsc` expect the same
  refusal (not verified), and use runc with rootless Docker on a dedicated host instead.
- **The image.** `make compile-image` on the compile host (2.8 GB to pull, about 9 GB on disk),
  or build it once and push it to a private registry; set `COMPILE_IMAGE` to it. The base
  image is pinned by digest in `compile/image/Dockerfile`; a new TeX Live is a change to that
  line, reviewed like code.
- **The API's internal listener.** The worker calls `INTERNAL_API_ADDR` with
  `COMPILE_SERVICE_TOKEN`. When it runs on another host, bind the listener to the private
  network the two share (a VPN such as WireGuard, or a private subnet), never to a public
  interface; the bearer token travels over it in plain HTTP otherwise. Allow the compile host
  nothing else on the network.
- **Settings.** `COMPILE_SERVICE_TOKEN` (at least 32 characters, different from every other
  secret; `openssl rand -base64 48`) in both the API and the worker; the API refuses to start
  in production without it. The worker also needs `COMPILE_API_URL` and `COMPILE_IMAGE`; the
  limits (`COMPILE_MAX_PDF_BYTES`, `COMPILE_MAX_INPUT_BYTES`) should match the API's.
- **Stopping.** On SIGTERM the worker takes no new job and lets running ones finish (at most
  60 s each). Give it 90 s. A worker killed mid-compile leaves a container the next start
  removes; its job fails as `worker_lost` after its 120 s lease.
- **After the Landlock fix: purge older output (once, in every environment that compiled
  before it).** Before Landlock a document could typeset any file the container could read
  (`\input{/etc/passwd}` did), and the API keeps serving the newest successful PDF of every
  project, so such a PDF stays downloadable until it is deleted. When the sandbox image with
  `rs-landlock` is live, take the time it went live (the image's creation time when it was
  built there, `docker image inspect -f '{{.Created}}' $COMPILE_IMAGE`, or the deploy time) and
  run `make purge-compile-output before=<that time>` to list, then the same with `apply=1`. It
  deletes the PDF (row and object) and the log of every compile that started before it and
  keeps the records; those projects show "no PDF" until they compile again. Development ran it
  on 2026-11-23 with `before=2026-11-23T21:10:27+06:00` (`docs/decisions.md`).

## Adding a step

A slice that needs a one-off step after its migration adds a row here, a `make` target that
is idempotent, and the target to the command table in `AGENTS.md`.
