import math

import pytest

from ai_service.config import DEFAULT_MODEL_NAME, DEFAULT_MODEL_REVISION, MAX_TEXT_CHARS

from .conftest import AUTH


def post(client, body, headers=AUTH):
    return client.post("/embed", json=body, headers=headers)


@pytest.mark.parametrize("kind,prefix", [("passage", "passage: "), ("query", "query: ")])
def test_prefix_for_each_kind(client, model, kind, prefix):
    res = post(client, {"kind": kind, "texts": ["graph neural networks", "গ্রাফ নিউরাল নেটওয়ার্ক"]})
    assert res.status_code == 200, res.text
    assert model.seen[-1] == [prefix + "graph neural networks", prefix + "গ্রাফ নিউরাল নেটওয়ার্ক"]


def test_vectors_are_normalised_with_768_dimensions(client):
    res = post(client, {"kind": "passage", "texts": ["a", "b", "c"]})
    body = res.json()
    assert body["dimensions"] == 768
    assert len(body["vectors"]) == 3
    for v in body["vectors"]:
        assert len(v) == 768
        assert math.isclose(math.sqrt(sum(x * x for x in v)), 1.0, rel_tol=1e-9)


def test_query_and_passage_of_the_same_text_differ(client):
    p = post(client, {"kind": "passage", "texts": ["climate models"]}).json()["vectors"][0]
    q = post(client, {"kind": "query", "texts": ["climate models"]}).json()["vectors"][0]
    assert p != q


def test_vectors_keep_the_order_of_the_texts(client):
    both = post(client, {"kind": "passage", "texts": ["x", "y"]}).json()["vectors"]
    y = post(client, {"kind": "passage", "texts": ["y"]}).json()["vectors"][0]
    assert both[1] == y


def test_model_name_carries_the_pinned_revision(client):
    body = post(client, {"kind": "query", "texts": ["q"]}).json()
    assert body["model"] == f"{DEFAULT_MODEL_NAME}@{DEFAULT_MODEL_REVISION}"
    assert body["model"] == "intfloat/multilingual-e5-base@d128750597153bb5987e10b1c3493a34e5a4502a"


def test_batch_limits_accept_32_and_8000_chars(client, model):
    res = post(client, {"kind": "passage", "texts": ["t" * MAX_TEXT_CHARS] * 32})
    assert res.status_code == 200, res.text
    assert len(res.json()["vectors"]) == 32


def test_8000_bangla_characters_are_accepted(client):
    # Characters, not bytes: 8,000 Bangla characters are 24,000 bytes of UTF-8.
    res = post(client, {"kind": "passage", "texts": ["ক" * MAX_TEXT_CHARS]})
    assert res.status_code == 200, res.text


@pytest.mark.parametrize(
    "body,field",
    [
        ({"kind": "passage", "texts": []}, "texts"),
        ({"kind": "passage", "texts": ["t"] * 33}, "texts"),
        ({"kind": "passage", "texts": ["t" * (MAX_TEXT_CHARS + 1)]}, "texts"),
        ({"kind": "passage", "texts": [""]}, "texts"),
        ({"kind": "passage", "texts": [1]}, "texts"),
        ({"kind": "passage", "texts": "not a list"}, "texts"),
        ({"kind": "passage"}, "texts"),
        ({"kind": "document", "texts": ["t"]}, "kind"),
        ({"texts": ["t"]}, "kind"),
        ({"kind": "passage", "texts": ["t"], "extra": 1}, "extra"),
    ],
)
def test_bad_requests_name_the_field(client, model, body, field):
    res = post(client, body)
    assert res.status_code == 400, res.text
    got = res.json()
    assert got["code"] == "bad_request"
    assert got["field"] == field
    assert model.seen == []  # the model was never called


def test_bad_request_never_repeats_the_text(client):
    secret_text = "private abstract " + "z" * MAX_TEXT_CHARS
    res = post(client, {"kind": "passage", "texts": [secret_text]})
    assert res.status_code == 400
    assert "private abstract" not in res.text


def test_body_that_is_not_json(client):
    res = client.post("/embed", content=b"{not json", headers=AUTH)
    assert res.status_code == 400
    assert res.json()["code"] == "bad_request"


def test_get_on_embed_is_not_allowed(client):
    assert client.get("/embed", headers=AUTH).status_code == 405


def test_no_public_docs(client):
    for path in ("/docs", "/redoc", "/openapi.json"):
        assert client.get(path).status_code == 404
