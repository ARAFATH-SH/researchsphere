# AI: Python service for embeddings, matching and the writing assistant

Internal service: only the API server calls it (`backend/internal/platform/aiclient`). It is
never exposed through the public API or the Next.js app. Decision record: `docs/decisions.md`,
2026-11-14.

## What it does now (AI-1, slice 15.1)

Text embeddings with `intfloat/multilingual-e5-base` (MIT, 768 dimensions, Bangla and English)
at a pinned revision, on the CPU.

| Route | Auth | Answer |
| --- | --- | --- |
| `GET /health` | none | `{"status": "loading" \| "ready" \| "failed", "model_loaded": bool, "model": "<name>@<revision>"}` |
| `GET /ready` | none | 200 once the model is loaded, else 503 `model_loading` / `model_failed` |
| `POST /embed` | `Authorization: Bearer $AI_SERVICE_TOKEN` | `{"model", "dimensions": 768, "vectors": [[...]]}` |

`POST /embed` takes `{"kind": "passage" | "query", "texts": [...]}`: 1 to 32 texts of 1 to
8,000 characters. Stored text is prefixed `passage: `, queries `query: `; vectors are
L2-normalised; text past 512 tokens is truncated. Errors: 400 `bad_request` (with `field`),
401, 413 (body over 1 MiB), 503 while loading, 504 after 30 s.

## Running

From the repository root, with `AI_SERVICE_TOKEN` in `.env`:

```sh
make ai-build         # the image (CPU only, about 2 GB, no model inside)
make ai-fetch-model   # optional: download the model (about 1.1 GB) into the aimodels volume now
make ai-up            # start and wait until the model is loaded
make ai-logs
```

The model is downloaded once, on the first start or by `make ai-fetch-model`, into the
`aimodels` Docker volume (1.13 GB), and kept across rebuilds. Loaded, the service uses about
0.9 GB of memory, 1.5 GB at peak while embedding a full batch. `make up` starts the service without
waiting; until it is ready the API's `GET /health` says `degraded`. A load that failed (for
example, no network on the first start) stays failed: fix the cause and run
`docker compose restart ai`.

## Settings

| Variable | Default | |
| --- | --- | --- |
| `AI_SERVICE_TOKEN` | (required) | At least 32 characters; the service refuses to start without it |
| `AI_MODEL_NAME`, `AI_MODEL_REVISION` | e5-base, `d128750…` | A new model or revision needs a full re-embed |
| `AI_MODEL_DIR` | `/models` | The volume |
| `AI_MODEL_OFFLINE` | `false` | `true`: never download, fail when the model is missing |
| `AI_MAX_BODY_BYTES` | 1048576 | |
| `AI_EMBED_TIMEOUT_SECONDS` | 30 | |

## Developing

```sh
make lint-ai   # ruff check and ruff format --check
make test-ai   # pytest with a stub model: installs neither torch nor the model
```

`uv sync` installs only FastAPI and uvicorn plus the dev tools. torch and
sentence-transformers are the `model` extra, which only the Dockerfile installs.
`scripts/smoke.py` checks a running service with the real model (CI runs it nightly).
