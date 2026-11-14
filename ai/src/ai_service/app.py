"""The HTTP API of the AI service. Internal only: the Go API calls it, nothing else.

GET  /health  whether the model is loaded (open, no data)
GET  /ready   200 once the model is loaded, else 503 (open; the container health check)
POST /embed   vectors for up to 32 texts (needs the shared token)
POST /embed-document  one vector for a long text, split by tokens and averaged (needs the token)

Errors use the API's shape, {"code": "...", "message": "...", "field"?: "..."}, and never
repeat the text that was sent.
"""

from __future__ import annotations

import asyncio
import hmac
import logging
import threading
from collections.abc import Callable
from contextlib import asynccontextmanager
from enum import StrEnum
from functools import partial
from typing import Annotated, Any

from fastapi import FastAPI, Request
from fastapi.responses import JSONResponse
from pydantic import BaseModel, ConfigDict, Field, StringConstraints, ValidationError

from .config import (
    DIMENSIONS,
    MAX_DOCUMENT_CHARS,
    MAX_DOCUMENT_CHUNKS,
    MAX_TEXT_CHARS,
    MAX_TEXTS,
    Settings,
)
from .document import embed_document
from .encoder import EmbeddingError, Kind, Model, embed
from .limits import BodySizeLimit

log = logging.getLogger(__name__)

Text = Annotated[str, StringConstraints(min_length=1, max_length=MAX_TEXT_CHARS)]


class EmbedRequest(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)

    kind: Kind
    texts: Annotated[list[Text], Field(min_length=1, max_length=MAX_TEXTS)]


class EmbedDocumentRequest(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)

    text: Annotated[str, StringConstraints(min_length=1, max_length=MAX_DOCUMENT_CHARS)]
    max_chunks: Annotated[int, Field(ge=1, le=MAX_DOCUMENT_CHUNKS, alias="maxChunks")] = (
        MAX_DOCUMENT_CHUNKS
    )


class Status(StrEnum):
    LOADING = "loading"
    READY = "ready"
    FAILED = "failed"


class ModelHolder:
    """The model, loaded once in a background thread, and the lock that lets one request
    use the CPU at a time."""

    def __init__(self, loader: Callable[[], Model]) -> None:
        self._loader = loader
        self.status = Status.LOADING
        self.model: Model | None = None
        self.lock = asyncio.Lock()

    def start(self) -> threading.Thread:
        thread = threading.Thread(target=self._load, name="model-loader", daemon=True)
        thread.start()
        return thread

    def _load(self) -> None:
        try:
            model = self._loader()
        except Exception:
            log.exception("loading the model failed")
            self.status = Status.FAILED
            return
        self.model = model
        self.status = Status.READY
        log.info("model loaded")


def error(status: int, code: str, message: str, field: str | None = None) -> JSONResponse:
    body: dict[str, Any] = {"code": code, "message": message}
    if field is not None:
        body["field"] = field
    return JSONResponse(body, status_code=status)


def not_ready(holder: ModelHolder) -> JSONResponse:
    if holder.status is Status.FAILED:
        return error(503, "model_failed", "the model could not be loaded; see the service log")
    return error(503, "model_loading", "the model is still loading; try again shortly")


def create_app(settings: Settings, loader: Callable[[], Model]) -> FastAPI:
    holder = ModelHolder(loader)

    @asynccontextmanager
    async def lifespan(_: FastAPI):
        holder.start()
        yield

    # No /docs or /openapi.json: the service is internal and has no public contract.
    app = FastAPI(lifespan=lifespan, docs_url=None, redoc_url=None, openapi_url=None)
    app.state.holder = holder
    app.add_middleware(BodySizeLimit, max_bytes=settings.max_body_bytes)
    token = settings.token.encode()

    @app.get("/health")
    async def health() -> dict[str, Any]:
        return {
            "status": holder.status.value,
            "model_loaded": holder.status is Status.READY,
            "model": settings.model_id,
        }

    @app.get("/ready")
    async def ready() -> JSONResponse:
        if holder.status is not Status.READY:
            return not_ready(holder)
        return JSONResponse({"status": "ready", "model": settings.model_id})

    @app.post("/embed")
    async def embed_texts(request: Request) -> JSONResponse:
        # Order: token, then model state, then the body, so nothing about the input is
        # looked at for a caller without the token.
        if not authorised(request.headers.get("authorization", ""), token):
            return error(401, "unauthorized", "a valid service token is required")
        if holder.status is not Status.READY or holder.model is None:
            return not_ready(holder)

        try:
            req = EmbedRequest.model_validate_json(await request.body())
        except ValidationError as exc:
            return bad_request(exc)

        try:
            vectors = await run_with_timeout(
                holder,
                settings.embed_timeout_seconds,
                partial(embed, holder.model, req.kind, req.texts),
            )
        except TimeoutError:
            return error(504, "timeout", "embedding took too long; try fewer or shorter texts")
        except EmbeddingError:
            log.exception("the model returned an unusable embedding")
            return error(500, "internal", "the model returned an unusable embedding")

        return JSONResponse(
            {"model": settings.model_id, "dimensions": DIMENSIONS, "vectors": vectors}
        )

    @app.post("/embed-document")
    async def embed_doc(request: Request) -> JSONResponse:
        # The same order as /embed.
        if not authorised(request.headers.get("authorization", ""), token):
            return error(401, "unauthorized", "a valid service token is required")
        if holder.status is not Status.READY or holder.model is None:
            return not_ready(holder)

        try:
            req = EmbedDocumentRequest.model_validate_json(await request.body())
        except ValidationError as exc:
            return bad_document_request(exc)

        try:
            doc = await run_with_timeout(
                holder,
                settings.embed_timeout_seconds,
                partial(embed_document, holder.model, req.text, req.max_chunks),
            )
        except TimeoutError:
            return error(504, "timeout", "embedding took too long; try a shorter text")
        except ValueError:
            return error(400, "bad_request", "text must contain words", "text")
        except EmbeddingError:
            log.exception("the model returned an unusable embedding")
            return error(500, "internal", "the model returned an unusable embedding")

        return JSONResponse(
            {
                "model": settings.model_id,
                "dimensions": DIMENSIONS,
                "vector": doc.vector,
                "chunks": doc.chunks,
                "truncated": doc.truncated,
            }
        )

    return app


def authorised(header: str, token: bytes) -> bool:
    scheme, _, value = header.partition(" ")
    if scheme.lower() != "bearer" or not value:
        return False
    return hmac.compare_digest(value.strip().encode(), token)


async def run_with_timeout[T](holder: ModelHolder, timeout: float, work_fn: Callable[[], T]) -> T:
    """Run work_fn (tokenizing and encoding) on a worker thread, one request at a time,
    within timeout seconds in all (waiting for the lock included). A thread cannot be
    stopped, so after a timeout the lock stays held until the work finishes; the next
    request waits rather than overloading the CPU."""
    loop = asyncio.get_running_loop()
    deadline = loop.time() + timeout
    async with asyncio.timeout_at(deadline):
        await holder.lock.acquire()
    work = asyncio.ensure_future(asyncio.to_thread(work_fn))
    work.add_done_callback(lambda _: holder.lock.release())
    async with asyncio.timeout_at(deadline):
        return await asyncio.shield(work)


def bad_request(exc: ValidationError) -> JSONResponse:
    """400 naming the first invalid field, without repeating its value."""
    first = exc.errors(include_input=False, include_url=False)[0]
    loc = [p for p in first["loc"] if isinstance(p, str)]
    field = loc[0] if loc else None
    messages = {
        "kind": "kind must be passage or query",
        "texts": f"texts must be a list of 1 to {MAX_TEXTS} texts, "
        f"each 1 to {MAX_TEXT_CHARS:,} characters",
    }
    if first["type"] == "json_invalid":
        return error(400, "bad_request", "the body must be JSON")
    if field not in messages:
        return error(400, "bad_request", "the body must hold only kind and texts", field)
    return error(400, "bad_request", messages[field], field)


def bad_document_request(exc: ValidationError) -> JSONResponse:
    """400 for /embed-document, naming the first invalid field without its value."""
    first = exc.errors(include_input=False, include_url=False)[0]
    loc = [p for p in first["loc"] if isinstance(p, str)]
    field = loc[0] if loc else None
    messages = {
        "text": f"text must be 1 to {MAX_DOCUMENT_CHARS:,} characters",
        "maxChunks": f"maxChunks must be a whole number from 1 to {MAX_DOCUMENT_CHUNKS}",
    }
    if first["type"] == "json_invalid":
        return error(400, "bad_request", "the body must be JSON")
    if field not in messages:
        return error(400, "bad_request", "the body must hold only text and maxChunks", field)
    return error(400, "bad_request", messages[field], field)
