# ResearchSphere AI service (AI-1): embeddings with intfloat/multilingual-e5-base on the CPU.
# The model is NOT in the image: it is downloaded at its pinned revision into the `aimodels`
# volume on the first start (or by `make ai-fetch-model`) and kept across rebuilds
# (docs/decisions.md). Build from the repository root: docker compose build ai.

FROM ghcr.io/astral-sh/uv:0.12.23 AS uv

FROM python:3.12.15-slim-trixie AS build
COPY --from=uv /uv /usr/local/bin/uv
ENV UV_COMPILE_BYTECODE=1 UV_LINK_MODE=copy UV_PYTHON_DOWNLOADS=never
WORKDIR /app
# Dependencies first, so a code change does not reinstall torch. CPU-only torch comes from
# the PyTorch CPU index (pyproject.toml); --extra model adds it and sentence-transformers.
COPY pyproject.toml uv.lock .python-version ./
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --locked --no-dev --extra model --no-install-project

FROM python:3.12.15-slim-trixie
RUN useradd --system --uid 10001 --home-dir /app --shell /usr/sbin/nologin ai \
    && mkdir -p /models && chown ai /models
WORKDIR /app
COPY --from=build /app/.venv /app/.venv
COPY src ./src
# HF_HOME is in the volume too: huggingface_hub keeps its download cache there, and the
# non-root user cannot write to /app.
ENV PATH=/app/.venv/bin:$PATH \
    PYTHONPATH=/app/src \
    PYTHONUNBUFFERED=1 \
    AI_MODEL_DIR=/models \
    HF_HOME=/models/.hf \
    HF_HUB_DISABLE_TELEMETRY=1 \
    TOKENIZERS_PARALLELISM=false
USER ai
EXPOSE 8000
# /ready answers 200 only once the model is loaded. The first start downloads about
# 1.1 GB first, hence the long start period.
HEALTHCHECK --interval=10s --timeout=3s --start-period=600s --retries=3 \
    CMD ["python", "-c", "import sys, urllib.request; sys.exit(urllib.request.urlopen('http://127.0.0.1:8000/ready', timeout=2).status != 200)"]
CMD ["python", "-m", "ai_service"]
