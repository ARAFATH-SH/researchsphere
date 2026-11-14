import pytest

from .conftest import TOKEN

BODY = {"kind": "query", "texts": ["q"]}


@pytest.mark.parametrize(
    "headers",
    [
        {},
        {"Authorization": ""},
        {"Authorization": "Bearer"},
        {"Authorization": "Bearer wrong-token-0123456789-abcdefghijklm"},
        {"Authorization": f"Bearer {TOKEN}x"},
        {"Authorization": f"Basic {TOKEN}"},
        {"Authorization": TOKEN},
        {"X-AI-Service-Token": TOKEN},
    ],
)
def test_embed_refuses_a_missing_or_wrong_token(client, model, headers):
    res = client.post("/embed", json=BODY, headers=headers)
    assert res.status_code == 401
    assert res.json()["code"] == "unauthorized"
    assert model.seen == []


def test_embed_accepts_the_token(client):
    res = client.post("/embed", json=BODY, headers={"Authorization": f"Bearer {TOKEN}"})
    assert res.status_code == 200


def test_scheme_is_case_insensitive(client):
    res = client.post("/embed", json=BODY, headers={"Authorization": f"bearer {TOKEN}"})
    assert res.status_code == 200


def test_token_is_checked_before_the_body(client):
    res = client.post("/embed", json={"kind": "nope"}, headers={})
    assert res.status_code == 401


def test_health_and_ready_need_no_token(client):
    assert client.get("/health").status_code == 200
    assert client.get("/ready").status_code == 200
