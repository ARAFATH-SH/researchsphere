"""A request body size limit, as plain ASGI middleware (no dependency)."""

from __future__ import annotations

import json
from typing import Any

Scope = dict[str, Any]
Message = dict[str, Any]


class BodySizeLimit:
    """Answer 413 when a request body is larger than max_bytes.

    A Content-Length above the limit is refused before reading anything. A body without
    one (chunked) is read up to the limit and refused as soon as it goes past it; a body
    within the limit is replayed to the app unchanged.
    """

    def __init__(self, app: Any, max_bytes: int) -> None:
        self.app = app
        self.max_bytes = max_bytes

    async def __call__(self, scope: Scope, receive: Any, send: Any) -> None:
        if scope["type"] != "http":
            await self.app(scope, receive, send)
            return

        for name, value in scope.get("headers", []):
            if name == b"content-length":
                try:
                    length = int(value)
                except ValueError:
                    await self._reply(send, 400, "bad_request", "invalid Content-Length")
                    return
                if length > self.max_bytes:
                    await self._too_large(send)
                    return

        chunks: list[bytes] = []
        size = 0
        while True:
            message = await receive()
            if message["type"] == "http.disconnect":
                return
            chunk = message.get("body", b"")
            size += len(chunk)
            if size > self.max_bytes:
                await self._too_large(send)
                return
            chunks.append(chunk)
            if not message.get("more_body", False):
                break

        body = b"".join(chunks)
        replayed = False

        async def replay() -> Message:
            nonlocal replayed
            if not replayed:
                replayed = True
                return {"type": "http.request", "body": body, "more_body": False}
            return await receive()

        await self.app(scope, replay, send)

    async def _too_large(self, send: Any) -> None:
        await self._reply(
            send, 413, "payload_too_large", f"the body must be at most {self.max_bytes} bytes"
        )

    @staticmethod
    async def _reply(send: Any, status: int, code: str, message: str) -> None:
        body = json.dumps({"code": code, "message": message}).encode()
        await send(
            {
                "type": "http.response.start",
                "status": status,
                "headers": [
                    (b"content-type", b"application/json"),
                    (b"content-length", str(len(body)).encode()),
                    (b"connection", b"close"),
                ],
            }
        )
        await send({"type": "http.response.body", "body": body})
