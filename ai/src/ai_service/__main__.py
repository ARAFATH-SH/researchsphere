"""Entry point.

python -m ai_service          serve on 0.0.0.0:8000 (inside the container)
python -m ai_service fetch    download the model into AI_MODEL_DIR and exit
"""

from __future__ import annotations

import logging
import os
import sys

from . import config, model


def main(argv: list[str]) -> int:
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s")
    if argv[1:] == ["fetch"]:
        # Fetching needs no token: build settings from the model variables only.
        settings = config.load({**os.environ, "AI_SERVICE_TOKEN": "x" * config.MIN_TOKEN_LEN})
        path = model.fetch(settings)
        logging.getLogger("ai_service").info("model %s is in %s", settings.model_id, path)
        return 0
    if argv[1:]:
        print("usage: python -m ai_service [fetch]", file=sys.stderr)
        return 2

    try:
        settings = config.load()
    except config.ConfigError as exc:
        print(f"ai service: {exc}", file=sys.stderr)
        return 1

    import uvicorn

    from .app import create_app

    app = create_app(settings, lambda: model.load(settings))
    # One worker: the model takes about 1.1 GB, a second copy would double it.
    port = int(os.environ.get("AI_LISTEN_PORT", "8000"))
    uvicorn.run(app, host="0.0.0.0", port=port, workers=1, access_log=False)  # noqa: S104
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
