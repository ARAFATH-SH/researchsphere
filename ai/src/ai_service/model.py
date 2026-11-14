"""The real model: fetching its files at the pinned revision and loading it on the CPU.

Imports sentence-transformers and huggingface_hub only inside the functions, so the service
and its tests run without the `model` extra.
"""

from __future__ import annotations

import logging
from collections.abc import Sequence
from pathlib import Path

from .config import DIMENSIONS, MAX_TOKENS, Settings

log = logging.getLogger(__name__)

# Only what sentence-transformers needs to load the model from safetensors. The repository
# also holds pytorch_model.bin, ONNX and OpenVINO copies (about 3 GB more) that are skipped.
ALLOW_PATTERNS = [
    "*.json",
    "model.safetensors",
    "sentencepiece.bpe.model",
    "1_Pooling/*",
]


def fetch(settings: Settings) -> Path:
    """Return the model's local folder, downloading it at the pinned revision if needed.

    The files go into settings.model_dir (a Docker volume), so they are downloaded once,
    on the first start or by `make ai-fetch-model`, and survive image rebuilds.
    """
    from huggingface_hub import snapshot_download

    common = {
        "repo_id": settings.model_name,
        "revision": settings.model_revision,
        "cache_dir": settings.model_dir,
        "allow_patterns": ALLOW_PATTERNS,
    }
    try:
        return Path(snapshot_download(local_files_only=True, **common))
    except Exception:
        if settings.offline:
            raise
    log.info(
        "model not in %s yet; downloading %s (about 1.1 GB, once)",
        settings.model_dir,
        settings.model_id,
    )
    return Path(snapshot_download(**common))


class E5Model:
    """multilingual-e5-base through sentence-transformers, on the CPU."""

    dimensions = DIMENSIONS

    def __init__(self, path: Path) -> None:
        from sentence_transformers import SentenceTransformer

        self._model = SentenceTransformer(str(path), device="cpu", local_files_only=True)
        # Longer input is truncated, never refused (AI-1).
        self._model.max_seq_length = MAX_TOKENS
        got = self._model.get_sentence_embedding_dimension()
        if got != DIMENSIONS:
            raise RuntimeError(f"model has {got} dimensions, want {DIMENSIONS}")

    def encode(self, texts: Sequence[str]) -> list[list[float]]:
        # Normalisation happens in encoder.embed, where the tests can check it.
        vectors = self._model.encode(
            list(texts), batch_size=len(texts), normalize_embeddings=False, convert_to_numpy=True
        )
        return vectors.tolist()

    # The tokenizer, for splitting long documents by real tokens (document.py). No special
    # tokens and no truncation; verbose=False keeps the "longer than 512" warning out of the log.
    def token_spans(self, text: str) -> list[tuple[int, int]]:
        enc = self._model.tokenizer(
            text, add_special_tokens=False, return_offsets_mapping=True, verbose=False
        )
        return [(int(a), int(b)) for a, b in enc["offset_mapping"]]

    def count_tokens(self, text: str) -> int:
        enc = self._model.tokenizer(text, add_special_tokens=False, verbose=False)
        return len(enc["input_ids"])


def load(settings: Settings) -> E5Model:
    return E5Model(fetch(settings))
