# Compile: the LaTeX compile worker (slice 23)

The compile worker claims compile jobs on the API's internal listener and runs each in a
sandbox container of its own (FR-13, NFR-3, NFR-4). `docs/compile.md` is the contract: job
states, retention, the internal operations, the sandbox's settings and the threat model.

| Path | Holds |
| --- | --- |
| `cmd/worker` | The worker: settings from the environment (`.env.example`, "Compile") |
| `cmd/bench` | `make bench-compile`: NFR-3 on `bench/sample`, a 10-page paper |
| `internal/apiclient` | The four internal operations, written by hand (no dependency) |
| `internal/docker` | The part of the Docker Engine API the worker needs, over the socket |
| `internal/sandbox` | One compile in one container: the isolation settings, files in and out, the 60 s kill, cleanup; `real_test.go` (build tag `sandbox`) runs against real Docker |
| `internal/worker` | The loop: claim, run, store the PDF, report |
| `image/` | The sandbox image: TeX Live full pinned by digest, the entrypoint `rs-compile`, `latexmkrc`, `texmf.cnf` |
| `Dockerfile` | The worker's own image (compose profile `compile`) |

Standard library only: `go.mod` has no requirement, and `compile/.golangci.yml` refuses one.

```sh
make compile-image          # once: the sandbox image (2.8 GB to pull, about 9 GB on disk)
make api                    # with COMPILE_SERVICE_TOKEN in .env
make compile-worker         # on this host, with your Docker
make test-compile           # fake Docker, no image (part of make test)
make test-compile-sandbox   # real Docker and the image, about a minute
make bench-compile          # NFR-3
```
