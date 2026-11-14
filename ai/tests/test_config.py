from pathlib import Path

import pytest

from ai_service import config

TOKEN = "t" * 32


def test_defaults():
    s = config.load({"AI_SERVICE_TOKEN": TOKEN})
    assert s.model_id == "intfloat/multilingual-e5-base@d128750597153bb5987e10b1c3493a34e5a4502a"
    assert s.model_dir == Path("/models")
    assert s.offline is False
    assert s.max_body_bytes == 1024 * 1024
    assert s.embed_timeout_seconds == 30.0


@pytest.mark.parametrize("token", ["", "   ", "t" * 31])
def test_short_or_missing_token_is_refused(token):
    with pytest.raises(config.ConfigError) as exc:
        config.load({"AI_SERVICE_TOKEN": token})
    assert "AI_SERVICE_TOKEN" in str(exc.value)


def test_token_never_appears_in_repr():
    s = config.load({"AI_SERVICE_TOKEN": "secret-" + "s" * 32})
    assert "secret-" not in repr(s)


@pytest.mark.parametrize("revision", ["main", "d1287505", "X" * 40])
def test_revision_must_be_a_full_commit_hash(revision):
    with pytest.raises(config.ConfigError):
        config.load({"AI_SERVICE_TOKEN": TOKEN, "AI_MODEL_REVISION": revision})


@pytest.mark.parametrize(
    "name,value",
    [
        ("AI_MAX_BODY_BYTES", "0"),
        ("AI_MAX_BODY_BYTES", "big"),
        ("AI_EMBED_TIMEOUT_SECONDS", "-1"),
        ("AI_EMBED_TIMEOUT_SECONDS", "nan"),
        ("AI_MODEL_OFFLINE", "maybe"),
    ],
)
def test_invalid_numbers_and_flags(name, value):
    with pytest.raises(config.ConfigError):
        config.load({"AI_SERVICE_TOKEN": TOKEN, name: value})


def test_overrides():
    s = config.load(
        {
            "AI_SERVICE_TOKEN": TOKEN,
            "AI_MODEL_DIR": "/tmp/m",  # noqa: S108
            "AI_MODEL_OFFLINE": "true",
            "AI_MAX_BODY_BYTES": "2048",
            "AI_EMBED_TIMEOUT_SECONDS": "2.5",
        }
    )
    assert (s.model_dir, s.offline, s.max_body_bytes, s.embed_timeout_seconds) == (
        Path("/tmp/m"),  # noqa: S108
        True,
        2048,
        2.5,
    )
