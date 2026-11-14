"""POST /embed-document and the token-based split behind it (AI-1.1)."""

import math
import re

import pytest

from ai_service.config import MAX_DOCUMENT_CHARS, MAX_TOKENS
from ai_service.document import SPECIAL_TOKENS, split
from ai_service.encoder import normalise

from .conftest import AUTH, StubModel

PREFIX = "passage: "

ENGLISH = (
    "We study graph neural networks for predicting crop yield from satellite images. "
    "The method combines weather records with soil maps and is tested on five districts. "
)
BANGLA = (
    "আমরা উপগ্রহ চিত্র থেকে ফসলের ফলন অনুমানের জন্য গ্রাফ নিউরাল নেটওয়ার্ক নিয়ে কাজ করি। "
    "পদ্ধতিটি আবহাওয়ার তথ্য ও মাটির মানচিত্র একসাথে ব্যবহার করে। "
)
HEADLINE = "Headline: Remote sensing for agriculture\nInterests: crop yield, GNN\nSkills: Python\n"


def post(client, body, headers=AUTH):
    return client.post("/embed-document", json=body, headers=headers)


def tokens_with_prefix(model: StubModel, chunk: str) -> int:
    return model.count_tokens(PREFIX + chunk) + SPECIAL_TOKENS


def words(text: str) -> list[str]:
    return re.findall(r"\S+", text)


@pytest.mark.parametrize(
    "body",
    [ENGLISH * 60, BANGLA * 40, (ENGLISH + BANGLA) * 30],
    ids=["english", "bangla", "mixed"],
)
def test_every_chunk_fits_the_model_with_the_prefix(body):
    model = StubModel()
    chunks, _ = split(model, HEADLINE + body, max_chunks=8)
    assert len(chunks) > 1
    for c in chunks:
        assert tokens_with_prefix(model, c) <= MAX_TOKENS


@pytest.mark.parametrize(
    "body", [ENGLISH * 12, BANGLA * 6, (ENGLISH + BANGLA) * 5], ids=["english", "bangla", "mixed"]
)
def test_nothing_is_lost_when_the_text_fits_in_8_chunks(body):
    model = StubModel()
    text = HEADLINE + body
    chunks, truncated = split(model, text, max_chunks=8)
    assert not truncated
    assert words(" ".join(chunks)) == words(text)


def test_bangla_needs_more_chunks_than_english_of_the_same_length():
    model = StubModel()
    english = (ENGLISH * 40)[:4000]
    bangla = (BANGLA * 40)[:4000]
    en, _ = split(model, english, max_chunks=8)
    bn, _ = split(model, bangla, max_chunks=8)
    assert len(bn) > len(en)


def test_a_character_count_would_overflow_for_bangla():
    # The reason chunking lives here: 1,200 Bangla characters are more than 512 tokens.
    model = StubModel()
    assert model.count_tokens(PREFIX + (BANGLA * 20)[:1200]) + SPECIAL_TOKENS > MAX_TOKENS


def test_the_start_of_the_text_is_in_the_first_chunk():
    model = StubModel()
    chunks, _ = split(model, HEADLINE + BANGLA * 40, max_chunks=8)
    assert chunks[0].startswith(HEADLINE.strip().split("\n")[0])
    assert "Skills: Python" in chunks[0]


def test_at_most_max_chunks_and_the_rest_is_reported():
    model = StubModel()
    chunks, truncated = split(model, BANGLA * 200, max_chunks=8)
    assert len(chunks) == 8
    assert truncated
    chunks, truncated = split(model, BANGLA * 200, max_chunks=3)
    assert len(chunks) == 3
    assert truncated


def test_chunks_end_at_line_breaks_then_sentences():
    model = StubModel()
    lines = "\n".join(f"Paper {i}. " + ENGLISH for i in range(40))
    chunks, _ = split(model, lines, max_chunks=8)
    # Each chunk but the last ends where a line ended.
    for c in chunks[:-1]:
        assert c.endswith(ENGLISH.strip())
    bangla_chunks, _ = split(model, BANGLA * 40, max_chunks=8)
    for c in bangla_chunks[:-1]:
        assert c.endswith("।")


def test_a_word_longer_than_a_chunk_is_cut():
    model = StubModel()
    chunks, truncated = split(model, "ক" * 2000, max_chunks=8)
    assert not truncated
    assert "".join(chunks) == "ক" * 2000
    for c in chunks:
        assert tokens_with_prefix(model, c) <= MAX_TOKENS


def test_the_vector_is_the_normalised_mean_of_the_chunk_vectors(client, model):
    text = HEADLINE + BANGLA * 20
    res = post(client, {"text": text, "maxChunks": 8})
    assert res.status_code == 200, res.text
    body = res.json()
    chunks, _ = split(model, text, max_chunks=8)
    assert body["chunks"] == len(chunks) > 1
    assert body["truncated"] is False
    assert body["dimensions"] == 768
    # The model saw each chunk with the passage prefix, in one call.
    assert model.seen[-1] == [PREFIX + c for c in chunks]
    per_chunk = [normalise(v, 768) for v in model.encode([PREFIX + c for c in chunks])]
    want = normalise([sum(v[d] for v in per_chunk) / len(per_chunk) for d in range(768)], 768)
    assert body["vector"] == pytest.approx(want)
    assert math.isclose(math.sqrt(sum(x * x for x in body["vector"])), 1.0, rel_tol=1e-9)


def test_a_short_text_is_one_chunk(client):
    body = post(client, {"text": "graph neural networks"}).json()
    assert body["chunks"] == 1
    assert body["truncated"] is False


def test_truncated_long_text(client):
    body = post(client, {"text": BANGLA * 200, "maxChunks": 2}).json()
    assert body["chunks"] == 2
    assert body["truncated"] is True


def test_the_longest_text_is_accepted(client):
    res = post(client, {"text": "ক" * MAX_DOCUMENT_CHARS})
    assert res.status_code == 200, res.text
    assert res.json()["chunks"] == 8


def test_needs_the_token(client, model):
    assert post(client, {"text": "t"}, headers={}).status_code == 401
    assert model.seen == []


@pytest.mark.parametrize(
    "body,field",
    [
        ({"text": ""}, "text"),
        ({"text": "t" * (MAX_DOCUMENT_CHARS + 1)}, "text"),
        ({"text": 1}, "text"),
        ({}, "text"),
        ({"text": "   "}, "text"),
        ({"text": "t", "maxChunks": 0}, "maxChunks"),
        ({"text": "t", "maxChunks": 9}, "maxChunks"),
        ({"text": "t", "maxChunks": "8"}, "maxChunks"),
        ({"text": "t", "max_chunks": 8}, "max_chunks"),
    ],
)
def test_bad_requests_name_the_field(client, model, body, field):
    res = post(client, body)
    assert res.status_code == 400, res.text
    got = res.json()
    assert got["code"] == "bad_request"
    assert got["field"] == field
    assert model.seen == []


def test_bad_request_never_repeats_the_text(client):
    res = post(client, {"text": "private abstract " + "z" * MAX_DOCUMENT_CHARS})
    assert res.status_code == 400
    assert "private abstract" not in res.text


def test_503_while_loading(make_client, gate):
    client = make_client(lambda: gate.wait(5) and StubModel())
    res = post(client, {"text": "t"})
    assert res.status_code == 503
    assert res.json()["code"] == "model_loading"
