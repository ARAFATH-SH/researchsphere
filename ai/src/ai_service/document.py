"""One vector for a long document (AI-1.1): split by real tokens, embed, average.

e5 reads at most 512 tokens, and Bangla takes far more tokens per character than English,
so a character count cannot say where a chunk must end. The split uses the model's own
tokenizer: every chunk, with the "passage: " prefix and the two special tokens, fits in
MAX_TOKENS. Chunks end at a line break, else at the end of a sentence, else between words,
and only cut a word when one is longer than a chunk. The start of the text always lands in
the first chunk, so callers put what matters most first.

Each chunk is embedded and L2-normalised as POST /embed does, the vectors are averaged and
the mean is L2-normalised again. Text past max_chunks chunks is left out and reported as
truncated.
"""

from __future__ import annotations

from collections.abc import Sequence
from dataclasses import dataclass
from typing import Protocol

from .config import DIMENSIONS, MAX_TOKENS
from .encoder import PREFIXES, EmbeddingError, Kind, Model, embed, normalise

# <s> and </s>, which the tokenizer adds around every encoded text.
SPECIAL_TOKENS = 2

# Ends of sentences, Bangla's daari (।) included.
SENTENCE_ENDS = (".", "!", "?", ";", "।", "॥")


class Tokenizer(Protocol):
    """The model's tokenizer, without special tokens."""

    def token_spans(self, text: str) -> list[tuple[int, int]]:
        """The (start, end) character offsets of each token of text, in order."""
        ...

    def count_tokens(self, text: str) -> int:
        """How many tokens text has."""
        ...


class DocumentModel(Model, Tokenizer, Protocol):
    """A model that can also tokenize."""


@dataclass(frozen=True)
class DocumentEmbedding:
    vector: list[float]
    chunks: int
    truncated: bool


def split(
    tokenizer: Tokenizer, text: str, max_chunks: int, max_tokens: int = MAX_TOKENS
) -> tuple[list[str], bool]:
    """Split text into at most max_chunks chunks that each fit in max_tokens with the
    passage prefix. Returns the chunks and whether text was left over."""
    prefix = PREFIXES[Kind.PASSAGE]
    budget = max_tokens - SPECIAL_TOKENS - tokenizer.count_tokens(prefix)
    if budget < 1:
        raise ValueError("max_tokens leaves no room for text")
    spans = [s for s in tokenizer.token_spans(text) if s[1] > s[0]]
    chunks: list[str] = []
    i, n = 0, len(spans)
    while i < n and len(chunks) < max_chunks:
        end = min(i + budget, n)
        if end < n:
            end = _cut(text, spans, i, end)
        chunk = text[spans[i][0] : spans[end - 1][1]].strip()
        # Text cut out of a longer one can tokenize a little differently; shrink until the
        # chunk itself fits.
        while end - i > 1:
            over = tokenizer.count_tokens(prefix + chunk) + SPECIAL_TOKENS - max_tokens
            if over <= 0:
                break
            end = max(i + 1, end - over)
            chunk = text[spans[i][0] : spans[end - 1][1]].strip()
        if chunk:
            chunks.append(chunk)
        i = end
    return chunks, i < n


def _cut(text: str, spans: Sequence[tuple[int, int]], start: int, end: int) -> int:
    """The token index at which the chunk spans[start:end] should end: the last line break,
    else the last sentence end, else the last gap between words in its second half."""
    lowest = start + max(1, (end - start) // 2)
    word = sentence = 0
    for j in range(end, lowest - 1, -1):
        gap = text[spans[j - 1][1] : spans[j][0]]
        if "\n" in gap:
            return j
        if gap and not gap.strip():
            before = text[spans[j - 1][0] : spans[j - 1][1]]
            if not sentence and before.endswith(SENTENCE_ENDS):
                sentence = j
            if not word:
                word = j
    return sentence or word or end


def embed_document(model: DocumentModel, text: str, max_chunks: int) -> DocumentEmbedding:
    """One unit-length vector for text. Raises ValueError when text has no tokens."""
    chunks, truncated = split(model, text, max_chunks)
    if not chunks:
        raise ValueError("text has no words")
    vectors = embed(model, Kind.PASSAGE, chunks)
    mean = [sum(v[d] for v in vectors) / len(vectors) for d in range(DIMENSIONS)]
    try:
        vector = normalise(mean, DIMENSIONS)
    except EmbeddingError:
        raise EmbeddingError("the chunk vectors cancel out") from None
    return DocumentEmbedding(vector=vector, chunks=len(chunks), truncated=truncated)
