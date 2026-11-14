"""Turning texts into vectors: the e5 prefixes and L2 normalisation (AI-1).

The prefixes and the normalisation live here, not in the model wrapper, so the unit tests
check them with a stub model and CI never downloads the real one.
"""

from __future__ import annotations

import math
from collections.abc import Sequence
from enum import StrEnum
from typing import Protocol

from .config import DIMENSIONS


class Kind(StrEnum):
    """What a text is for. e5 was trained with a prefix for each and needs it."""

    PASSAGE = "passage"  # stored text: profiles, projects, posts, venues
    QUERY = "query"  # a search query


PREFIXES = {Kind.PASSAGE: "passage: ", Kind.QUERY: "query: "}


class Model(Protocol):
    """A sentence embedding model. encode returns one raw vector per text."""

    dimensions: int

    def encode(self, texts: Sequence[str]) -> list[list[float]]: ...


class EmbeddingError(RuntimeError):
    """The model returned something that is not a usable embedding."""


def embed(model: Model, kind: Kind, texts: Sequence[str]) -> list[list[float]]:
    """Prefix each text for its kind, encode them and L2-normalise every vector.

    Every vector must have DIMENSIONS (768) numbers, the size of the embeddings columns,
    whatever the model says about itself."""
    prefix = PREFIXES[kind]
    raw = model.encode([prefix + t for t in texts])
    if len(raw) != len(texts):
        raise EmbeddingError(f"model returned {len(raw)} vectors for {len(texts)} texts")
    return [normalise(v, DIMENSIONS) for v in raw]


def normalise(vector: Sequence[float], dimensions: int) -> list[float]:
    """Return vector scaled to length 1, so cosine similarity is a dot product."""
    if len(vector) != dimensions:
        raise EmbeddingError(f"model returned {len(vector)} dimensions, want {dimensions}")
    norm = math.sqrt(math.fsum(x * x for x in vector))
    if not math.isfinite(norm) or norm == 0:
        raise EmbeddingError("model returned a zero or non-finite vector")
    return [float(x) / norm for x in vector]
