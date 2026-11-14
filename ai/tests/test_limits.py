import json

from .conftest import AUTH, StubModel, wait_ready


def test_body_over_the_limit_is_413(make_client):
    client = make_client(StubModel, max_body_bytes=1000)
    wait_ready(client)
    res = client.post("/embed", json={"kind": "passage", "texts": ["t" * 2000]}, headers=AUTH)
    assert res.status_code == 413
    assert res.json()["code"] == "payload_too_large"


def test_body_without_content_length_over_the_limit_is_413(make_client):
    client = make_client(StubModel, max_body_bytes=1000)
    wait_ready(client)
    payload = json.dumps({"kind": "passage", "texts": ["t" * 2000]}).encode()

    def chunks():
        yield payload[:600]
        yield payload[600:]

    res = client.post(
        "/embed", content=chunks(), headers={**AUTH, "Content-Type": "application/json"}
    )
    assert res.status_code == 413


def test_default_limit_fits_the_largest_valid_bangla_batch(client):
    # 32 texts of 8,000 Bangla characters: about 768 KB, under the 1 MiB default.
    res = client.post("/embed", json={"kind": "passage", "texts": ["ক" * 8000] * 32}, headers=AUTH)
    assert res.status_code == 200


def test_slow_embedding_times_out_with_504(make_client):
    client = make_client(lambda: StubModel(delay=0.5), embed_timeout_seconds=0.1)
    wait_ready(client)
    res = client.post("/embed", json={"kind": "passage", "texts": ["t"]}, headers=AUTH)
    assert res.status_code == 504
    assert res.json()["code"] == "timeout"


class BadModel(StubModel):
    def encode(self, texts):
        return [[0.0] * self.dimensions for _ in texts]


def test_unusable_vector_is_500(make_client):
    client = make_client(BadModel)
    wait_ready(client)
    res = client.post("/embed", json={"kind": "passage", "texts": ["t"]}, headers=AUTH)
    assert res.status_code == 500
    assert res.json()["code"] == "internal"
