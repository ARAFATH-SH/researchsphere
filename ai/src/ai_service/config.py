"""Settings of the AI service, read from environment variables."""

from __future__ import annotations

import os
from collections.abc import Mapping
from dataclasses import dataclass
from pathlib import Path

# The embedding model (AI-1, docs/decisions.md): multilingual (Bangla and English), 768
# dimensions, MIT licence. The revision is a commit of the model repository on Hugging
# Face, so every download gets the same weights and the same vectors.
DEFAULT_MODEL_NAME = "intfloat/multilingual-e5-base"
DEFAULT_MODEL_REVISION = "d128750597153bb5987e10b1c3493a34e5a4502a"
DIMENSIONS = 768
# e5 reads at most 512 tokens; longer text is cut off at the end (prefix included).
MAX_TOKENS = 512

# Request limits of POST /embed.
MAX_TEXTS = 32
MAX_TEXT_CHARS = 8_000
# 32 texts of 8,000 characters is 768 KB of Bangla in UTF-8 (3 bytes a character), plus the
# JSON around them.
DEFAULT_MAX_BODY_BYTES = 1024 * 1024
DEFAULT_EMBED_TIMEOUT_SECONDS = 30.0

MIN_TOKEN_LEN = 32
DEFAULT_MODEL_DIR = "/models"


class ConfigError(ValueError):
    """A setting is missing or invalid. The message never includes a secret's value."""


@dataclass(frozen=True)
class Settings:
    token: str
    model_name: str = DEFAULT_MODEL_NAME
    model_revision: str = DEFAULT_MODEL_REVISION
    model_dir: Path = Path(DEFAULT_MODEL_DIR)
    # False: download the model into model_dir when it is not there yet (the first start).
    offline: bool = False
    max_body_bytes: int = DEFAULT_MAX_BODY_BYTES
    embed_timeout_seconds: float = DEFAULT_EMBED_TIMEOUT_SECONDS

    @property
    def model_id(self) -> str:
        """The name stored with each embedding (AI-1.6): name@revision."""
        return f"{self.model_name}@{self.model_revision}"

    def __repr__(self) -> str:  # keeps the token out of logs and tracebacks
        return (
            f"Settings(model_id={self.model_id!r}, model_dir={str(self.model_dir)!r}, "
            f"offline={self.offline}, max_body_bytes={self.max_body_bytes}, "
            f"embed_timeout_seconds={self.embed_timeout_seconds})"
        )


def load(env: Mapping[str, str] | None = None) -> Settings:
    """Read the settings from env (os.environ by default)."""
    env = os.environ if env is None else env

    token = env.get("AI_SERVICE_TOKEN", "").strip()
    if len(token) < MIN_TOKEN_LEN:
        raise ConfigError(
            f"AI_SERVICE_TOKEN must be at least {MIN_TOKEN_LEN} characters; "
            "generate one with: openssl rand -base64 48"
        )

    revision = env.get("AI_MODEL_REVISION", "").strip() or DEFAULT_MODEL_REVISION
    if len(revision) != 40 or any(c not in "0123456789abcdef" for c in revision):
        raise ConfigError("AI_MODEL_REVISION must be a full 40-character commit hash")

    return Settings(
        token=token,
        model_name=env.get("AI_MODEL_NAME", "").strip() or DEFAULT_MODEL_NAME,
        model_revision=revision,
        model_dir=Path(env.get("AI_MODEL_DIR", "").strip() or DEFAULT_MODEL_DIR),
        offline=_bool(env, "AI_MODEL_OFFLINE"),
        max_body_bytes=_positive_int(env, "AI_MAX_BODY_BYTES", DEFAULT_MAX_BODY_BYTES),
        embed_timeout_seconds=_positive_float(
            env, "AI_EMBED_TIMEOUT_SECONDS", DEFAULT_EMBED_TIMEOUT_SECONDS
        ),
    )


def _bool(env: Mapping[str, str], name: str) -> bool:
    raw = env.get(name, "").strip().lower()
    if raw in ("", "0", "false"):
        return False
    if raw in ("1", "true"):
        return True
    raise ConfigError(f"{name} must be true or false, got {raw!r}")


def _positive_int(env: Mapping[str, str], name: str, default: int) -> int:
    raw = env.get(name, "").strip()
    if not raw:
        return default
    try:
        value = int(raw)
    except ValueError:
        value = 0
    if value < 1:
        raise ConfigError(f"{name} must be a whole number of at least 1, got {raw!r}")
    return value


def _positive_float(env: Mapping[str, str], name: str, default: float) -> float:
    raw = env.get(name, "").strip()
    if not raw:
        return default
    try:
        value = float(raw)
    except ValueError:
        value = 0.0
    if not value > 0:
        raise ConfigError(f"{name} must be a number of seconds above 0, got {raw!r}")
    return value
