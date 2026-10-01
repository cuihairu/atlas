"""Atlas Python SDK example: register a game server, start auto-heartbeat,
then look up characters and leave cleanly.

Run against a local Atlas (default ports):

    python3 examples/python/main.py

Environment: ATLAS_ADDR, ATLAS_REGISTRY_ADDR, ATLAS_REGISTRY_TOKEN, SERVER_ID.
"""

from __future__ import annotations

import logging
import os
import signal
import threading

from atlas_client import (
    AtlasClient,
    Endpoint,
    RegisterRequest,
)

logging.basicConfig(level=logging.INFO, format="%(levelname)s %(message)s")
logging.getLogger("httpx").setLevel(logging.WARNING)  # hide per-request lines
log = logging.getLogger("atlas-example")

stop = threading.Event()
signal.signal(signal.SIGINT, lambda *_: stop.set())
signal.signal(signal.SIGTERM, lambda *_: stop.set())


def env_or(key: str, fallback: str) -> str:
    return os.getenv(key) or fallback


def main() -> None:
    # RegistryAddr points at the split registry port when not behind a
    # merged proxy (local dev: ATLAS_REGISTRY_ADDR=http://localhost:8081).
    client = AtlasClient(
        env_or("ATLAS_ADDR", "http://localhost:8080"),
        registry_base_url=os.getenv("ATLAS_REGISTRY_ADDR") or None,
        registry_token=os.getenv("ATLAS_REGISTRY_TOKEN") or None,
    )

    # 1. Register this server.
    server_id = env_or("SERVER_ID", "demo-game-1")
    reg = client.register(RegisterRequest(
        server_id=server_id,
        name="Demo Game Server",
        type="game",
        region="cn-east",
        version="1.0.0",
        platform="any",
        endpoint=Endpoint(host="10.0.0.1", port=30001),
        capacity=2000,
    ))
    log.info("registered: %s (status=%s)", reg.server_id, reg.status)

    # 2. Auto-heartbeat: immediate first report, then every 10s. Update
    # the payload as load changes; Atlas suspects at 3x the interval.
    loop = client.start_heartbeat(server_id, interval=10.0)
    loop.on_error = lambda err: log.warning("heartbeat failed: %s", err)

    try:
        # 3. Discovery: where should account 42 play?
        try:
            rec = client.recommend(account_id=42, region="cn-east")
            log.info("account 42 → %s (%s)", rec.server.id, rec.reason)

            # 4. Directory: characters on that server.
            page = client.list_characters_by_server(rec.server.id, limit=10)
            for ch in page.characters:
                log.info("  character %d %s lv%d", ch.character_id, ch.name, ch.level)
        except Exception as exc:  # demo keeps running on lookup failures
            log.warning("lookup failed: %s", exc)

        # 5. Stay up until signalled.
        stop.wait()
    finally:
        log.info("shutting down…")
        loop.stop()
        try:
            client.unregister(server_id)
        except Exception as exc:
            log.warning("unregister: %s", exc)
        client.close()


if __name__ == "__main__":
    main()
