"""Check a running AI service with the real model (nightly CI job, or by hand).

    AI_SERVICE_TOKEN=... uv run python scripts/smoke.py [http://127.0.0.1:8090]

Standard library only. Exits 1 with a message when a check fails.
"""

from __future__ import annotations

import json
import math
import os
import sys
import urllib.error
import urllib.request

EXPECTED_MODEL = "intfloat/multilingual-e5-base@d128750597153bb5987e10b1c3493a34e5a4502a"


def call(base: str, path: str, body: dict | None = None, token: str = "") -> tuple[int, dict]:
    data = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request(base + path, data=data, method="POST" if data else "GET")
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            return resp.status, json.load(resp)
    except urllib.error.HTTPError as err:
        return err.code, json.load(err)


def dot(a: list[float], b: list[float]) -> float:
    return sum(x * y for x, y in zip(a, b, strict=True))


def main() -> int:
    base = (sys.argv[1] if len(sys.argv) > 1 else "http://127.0.0.1:8090").rstrip("/")
    token = os.environ.get("AI_SERVICE_TOKEN", "")
    failures: list[str] = []

    def check(ok: bool, what: str) -> None:
        print(("ok   " if ok else "FAIL ") + what)
        if not ok:
            failures.append(what)

    status, _ = call(base, "/ready")
    check(status == 200, "GET /ready is 200 (model loaded)")

    status, _ = call(base, "/embed", {"kind": "query", "texts": ["x"]})
    check(status == 401, "POST /embed without the token is 401")

    passages = [
        "Deep learning methods for detecting diseases in rice leaves from photographs.",
        "ছবি থেকে ধানের পাতার রোগ শনাক্ত করার জন্য ডিপ লার্নিং পদ্ধতি।",  # the same, in Bangla
        "A history of Baroque church architecture in southern Italy.",
    ]
    status, body = call(base, "/embed", {"kind": "passage", "texts": passages}, token)
    check(status == 200, "POST /embed with the token is 200")
    if status != 200:
        print(body)
        return 1
    check(body["model"] == EXPECTED_MODEL, f"model is {EXPECTED_MODEL}")
    check(body["dimensions"] == 768, "dimensions is 768")
    vecs = body["vectors"]
    check(all(len(v) == 768 for v in vecs), "every vector has 768 numbers")
    check(all(abs(math.sqrt(dot(v, v)) - 1) < 1e-4 for v in vecs), "every vector has length 1")

    en_bn, en_other = dot(vecs[0], vecs[1]), dot(vecs[0], vecs[2])
    print(f"     similarity English-Bangla (same meaning) {en_bn:.3f}, unrelated {en_other:.3f}")
    check(en_bn > en_other, "the Bangla translation is closer than an unrelated English text")

    _, q = call(base, "/embed", {"kind": "query", "texts": ["rice disease detection"]}, token)
    _, p = call(base, "/embed", {"kind": "passage", "texts": ["rice disease detection"]}, token)
    check(q["vectors"][0] != p["vectors"][0], "query and passage prefixes give different vectors")
    check(
        dot(q["vectors"][0], vecs[1]) > dot(q["vectors"][0], vecs[2]),
        "an English query ranks the Bangla passage above the unrelated one",
    )

    long_text = "word " * 1600  # 8,000 characters, far past 512 tokens: truncated, not refused
    status, _ = call(base, "/embed", {"kind": "passage", "texts": [long_text] * 32}, token)
    check(status == 200, "32 texts of 8,000 characters are accepted (truncated to 512 tokens)")

    if failures:
        print(f"{len(failures)} check(s) failed")
        return 1
    print("all checks passed")
    return 0


if __name__ == "__main__":
    sys.exit(main())
