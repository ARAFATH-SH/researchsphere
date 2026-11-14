"""A stub model, so the tests never download torch or the real model."""

from __future__ import annotations

import hashlib
import re
import threading
import time
from collections.abc import Callable, Iterator, Sequence

import pytest
from fastapi.testclient import TestClient

from ai_service.app import create_app
from ai_service.config import Settings

TOKEN = "test-token-0123456789-abcdefghijklmnop"
AUTH = {"Authorization": f"Bearer {TOKEN}"}


# The stub tokenizer: an ASCII letter or digit run costs one token per 4 characters, and
# every other visible character one token. So Bangla costs about four times as many tokens
# per character as English, as with the real tokenizer, and a character count cannot say
# where a chunk ends.
_TOKEN = re.compile(r"[A-Za-z0-9]{1,4}|[^\sA-Za-z0-9]")


class StubModel:
    """Deterministic vectors that are deliberately not normalised (length well above 1), so
    the tests prove the service normalises. Records the texts it was given. Tokenizes with
    a stub tokenizer (see _TOKEN)."""

    def __init__(self, dimensions: int = 768, delay: float = 0.0) -> None:
        self.dimensions = dimensions
        self.delay = delay
        self.seen: list[list[str]] = []

    def encode(self, texts: Sequence[str]) -> list[list[float]]:
        self.seen.append(list(texts))
        if self.delay:
            time.sleep(self.delay)
        out = []
        for t in texts:
            seed = hashlib.sha256(t.encode()).digest()
            out.append([float(seed[i % 32]) + 1.0 for i in range(self.dimensions)])
        return out

    def token_spans(self, text: str) -> list[tuple[int, int]]:
        return [m.span() for m in _TOKEN.finditer(text)]

    def count_tokens(self, text: str) -> int:
        return len(self.token_spans(text))


def settings(**kw) -> Settings:
    return Settings(token=TOKEN, **kw)


def wait_ready(client: TestClient, timeout: float = 5.0) -> None:
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        if client.get("/ready").status_code == 200:
            return
        time.sleep(0.01)
    raise AssertionError("model did not become ready")


@pytest.fixture
def model() -> StubModel:
    return StubModel()


@pytest.fixture
def make_client() -> Iterator[Callable[..., TestClient]]:
    clients: list[TestClient] = []

    def make(loader: Callable[[], object], **kw) -> TestClient:
        client = TestClient(create_app(settings(**kw), loader))
        client.__enter__()  # runs the lifespan, which starts loading
        clients.append(client)
        return client

    yield make
    for c in clients:
        c.__exit__(None, None, None)


@pytest.fixture
def client(make_client, model) -> TestClient:
    c = make_client(lambda: model)
    wait_ready(c)
    return c


@pytest.fixture
def gate() -> Iterator[threading.Event]:
    """Holds a loader until set; set at teardown so no thread is left waiting."""
    event = threading.Event()
    yield event
    event.set()
