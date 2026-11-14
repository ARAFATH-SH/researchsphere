from .conftest import AUTH, StubModel, wait_ready

BODY = {"kind": "passage", "texts": ["t"]}


def test_503_while_the_model_loads_then_ready(make_client, gate):
    model = StubModel()

    def slow_loader():
        assert gate.wait(5)
        return model

    client = make_client(slow_loader)

    health = client.get("/health").json()
    assert health["status"] == "loading"
    assert health["model_loaded"] is False

    ready = client.get("/ready")
    assert ready.status_code == 503
    assert ready.json()["code"] == "model_loading"

    embed = client.post("/embed", json=BODY, headers=AUTH)
    assert embed.status_code == 503
    assert embed.json()["code"] == "model_loading"

    gate.set()
    wait_ready(client)
    health = client.get("/health").json()
    assert health["status"] == "ready"
    assert health["model_loaded"] is True
    assert client.post("/embed", json=BODY, headers=AUTH).status_code == 200


def test_token_is_checked_before_loading_state(make_client, gate):
    client = make_client(lambda: gate.wait(5) and StubModel())
    assert client.post("/embed", json=BODY).status_code == 401


def test_a_failed_load_stays_503(make_client):
    def broken():
        raise OSError("no space left on device")

    client = make_client(broken)
    for _ in range(500):
        if client.get("/health").json()["status"] == "failed":
            break
    health = client.get("/health").json()
    assert health == {**health, "status": "failed", "model_loaded": False}
    ready = client.get("/ready")
    assert ready.status_code == 503
    assert ready.json()["code"] == "model_failed"
    assert client.post("/embed", json=BODY, headers=AUTH).status_code == 503
