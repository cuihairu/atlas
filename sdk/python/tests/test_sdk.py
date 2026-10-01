"""Python SDK tests — a fake Atlas on a real socket (mirrors sdk_test.go):
asserting paths, auth headers, query building, error mapping, retry and
the auto-heartbeat loop, for both the sync and async clients."""

from __future__ import annotations

import asyncio
import json
import re
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any, Callable, Dict, List, Optional, Tuple
from urllib.parse import parse_qs, urlparse

import pytest

from atlas_client import (
    AtlasAsyncClient,
    AtlasClient,
    AtlasError,
    CharacterFilter,
    CreateCharacterRequest,
    CreateMigrationRequest,
    Endpoint,
    HeartbeatRequest,
    RegisterRequest,
    ServerFilter,
    UpdateCharacterRequest,
)

Route = Tuple[str, re.Pattern, Callable[[Dict[str, Any]], Tuple[int, Any]]]


class FakeAtlas:
    """Minimal Atlas stand-in: regex routes → (status, json body)."""

    def __init__(self) -> None:
        self.routes: List[Route] = []
        self.requests: List[Dict[str, Any]] = []
        self._server = ThreadingHTTPServer(("127.0.0.1", 0), self._make_handler())
        self._thread = threading.Thread(target=self._server.serve_forever, daemon=True)
        self._thread.start()

    @property
    def url(self) -> str:
        return f"http://127.0.0.1:{self._server.server_port}"

    def close(self) -> None:
        self._server.shutdown()
        self._server.server_close()

    def _make_handler(self) -> type:
        outer = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args: Any) -> None:  # silence test output
                pass

            def _dispatch(self) -> None:
                parsed = urlparse(self.path)
                length = int(self.headers.get("Content-Length") or 0)
                raw = self.rfile.read(length) if length else b""
                try:
                    body = json.loads(raw) if raw else None
                except ValueError:
                    body = None
                req = {
                    "method": self.command,
                    "path": parsed.path,
                    "query": {k: v[0] for k, v in parse_qs(parsed.query).items()},
                    "headers": self.headers,
                    "body": body,
                }
                outer.requests.append(req)
                for method, pattern, fn in outer.routes:
                    if method != self.command or not pattern.fullmatch(parsed.path):
                        continue
                    status, payload = fn(req)
                    data = json.dumps(payload).encode()
                    self.send_response(status)
                    self.send_header("Content-Type", "application/json")
                    self.send_header("Content-Length", str(len(data)))
                    self.end_headers()
                    self.wfile.write(data)
                    return
                self.send_response(404)
                self.end_headers()

            do_GET = _dispatch
            do_POST = _dispatch
            do_PATCH = _dispatch
            do_DELETE = _dispatch

        return Handler


def client_for(server: FakeAtlas, **kwargs: Any) -> AtlasClient:
    kwargs.setdefault("base_backoff", 0.001)
    kwargs.setdefault("registry_token", "reg-token")
    kwargs.setdefault("admin_api_key", "adm-key")
    return AtlasClient(server.url, **kwargs)


def route(
    method: str, pattern: str, fn: Callable[[Dict[str, Any]], Tuple[int, Any]]
) -> Route:
    return (method, re.compile(pattern), fn)


def body_of(server: FakeAtlas, method: str, path: str) -> Dict[str, Any]:
    for req in server.requests:
        if req["method"] == method and re.fullmatch(path, req["path"]):
            return req
    raise AssertionError(f"no {method} {path} recorded")


# ── Registry lifecycle ──


def test_registry_lifecycle() -> None:
    s = FakeAtlas()
    s.routes.append(route(
        "POST", r"/v1/registry/servers/register",
        lambda q: (200, {"server_id": "game-1", "status": "online"})))
    beats: List[Dict[str, Any]] = []

    def beat(req: Dict[str, Any]) -> Tuple[int, Any]:
        beats.append(req["body"])
        return 200, {"server_id": "game-1", "status": "online", "next_heartbeat_in": 10}

    s.routes.append(route("POST", r"/v1/registry/servers/game-1/heartbeat", beat))
    s.routes.append(route(
        "POST", r"/v1/registry/servers/game-1/unregister",
        lambda q: (200, {"server_id": "game-1", "status": "offline"})))
    c = client_for(s)
    try:
        reg = c.register(RegisterRequest(
            server_id="game-1", name="Test", region="cn-east", capacity=100,
            endpoint=Endpoint(host="10.0.0.1", port=30001)))
        assert (reg.server_id, reg.status) == ("game-1", "online")
        sent = body_of(s, "POST", r"/v1/registry/servers/register")
        assert sent["headers"]["Authorization"] == "Bearer reg-token"
        assert sent["body"]["capacity"] == 100

        hb = c.heartbeat("game-1", HeartbeatRequest(players=7, load=0.3))
        assert hb.next_heartbeat_in == 10
        assert beats == [{"players": 7, "load": 0.3}]

        off = c.unregister("game-1")
        assert off.status == "offline"
    finally:
        c.close()
        s.close()


def test_registry_split_port() -> None:
    main, registry = FakeAtlas(), FakeAtlas()
    main.routes.append(route(
        "GET", r"/v1/discovery/servers",
        lambda q: (200, {"servers": [{"id": "game-1", "status": "online"}]})))
    registry.routes.append(route(
        "POST", r"/v1/registry/servers/register",
        lambda q: (200, {"server_id": "game-1", "status": "online"})))
    c = client_for(main, registry_base_url=registry.url)
    try:
        c.register(RegisterRequest(
            server_id="game-1", name="T", region="r", capacity=1,
            endpoint=Endpoint(host="h", port=1)))
        assert body_of(registry, "POST", r"/v1/registry/servers/register")
        assert len(c.list_servers()) == 1
        assert body_of(main, "GET", r"/v1/discovery/servers")
        # split port ⇒ no registry traffic leaked to the public port
        assert not any(r["path"].startswith("/v1/registry") for r in main.requests)
    finally:
        c.close()
        main.close()
        registry.close()


# ── Discovery ──


def test_discovery_and_error_mapping() -> None:
    s = FakeAtlas()
    seen: Dict[str, Any] = {}

    def servers(req: Dict[str, Any]) -> Tuple[int, Any]:
        seen.update(req["query"])
        return 200, {"servers": [{"id": "game-1", "status": "online", "players": 12}]}

    s.routes.append(route("GET", r"/v1/discovery/servers", servers))
    s.routes.append(route("GET", r"/v1/discovery/servers/nope", lambda q: (
        404, {"error": {"code": "SERVER_NOT_FOUND", "message": "server nope does not exist"}})))
    c = client_for(s)
    try:
        got = c.list_servers(ServerFilter(region="cn-east", status="online", limit=20))
        assert len(got) == 1 and got[0].id == "game-1" and got[0].players == 12
        assert seen == {"region": "cn-east", "status": "online", "limit": "20"}

        with pytest.raises(AtlasError) as exc:
            c.get_server("nope")
        assert exc.value.code == "SERVER_NOT_FOUND"
        assert exc.value.status == 404
    finally:
        c.close()
        s.close()


# ── Directory write shapes (nested / flat / queued) ──


def test_directory_write_shapes() -> None:
    s = FakeAtlas()
    replies = iter([
        # flat synchronous create (REST 201 body)
        (201, {"account_id": 7, "server_id": "game-1", "character_id": 1001, "name": "Hero"}),
        # nested envelope with status
        (200, {"character": {"character_id": 1001, "name": "Hero"},
               "status": "updated"}),
        # queued — status only
        (202, {"status": "queued"}),
        # delete — status only
        (200, {"status": "deleted"}),
    ])
    captured: List[Any] = []

    def write(req: Dict[str, Any]) -> Tuple[int, Any]:
        captured.append(req["body"])
        return next(replies)

    s.routes.append(route("POST", r"/v1/directory/characters", write))
    s.routes.append(route("PATCH", r"/v1/directory/characters/1001", write))
    s.routes.append(route("DELETE", r"/v1/directory/characters/1001", write))
    c = client_for(s)
    try:
        wr = c.create_character(CreateCharacterRequest(
            account_id=7, server_id="game-1", character_id=1001, name="Hero"))
        assert wr.status == "created" and wr.character is not None
        assert wr.character.character_id == 1001
        assert captured[-1]["name"] == "Hero"

        wr = c.update_character(1001, UpdateCharacterRequest(level=10))
        assert wr.status == "updated" and wr.character is not None
        assert wr.character.name == "Hero"
        assert captured[-1] == {"level": 10}  # PATCH omits unset fields

        wr = c.update_character(1001, UpdateCharacterRequest())  # no-op patch
        assert captured[-1] == {}
        assert wr.status == "queued"  # status-only reply → no character

        wr = c.delete_character(1001)
        assert wr.status == "deleted" and wr.character is None
    finally:
        c.close()
        s.close()


def test_directory_listing() -> None:
    s = FakeAtlas()
    chars = [{"account_id": 7, "server_id": "game-1", "character_id": n,
              "name": f"c{n}"} for n in range(3)]
    seen: Dict[str, Any] = {}
    s.routes.append(route(
        "GET", r"/v1/directory/accounts/7/characters",
        lambda q: (200, {"characters": chars})))

    def by_server(req: Dict[str, Any]) -> Tuple[int, Any]:
        seen.update(req["query"])
        return 200, {"characters": chars[:2], "next_cursor": "cursor-2"}

    s.routes.append(route("GET", r"/v1/directory/servers/game-1/characters", by_server))
    c = client_for(s)
    try:
        assert [ch.character_id for ch in c.list_characters_by_account(7)] == [0, 1, 2]
        page = c.list_characters_by_server("game-1", limit=2, cursor="cursor-1")
        assert len(page.characters) == 2 and page.next_cursor == "cursor-2"
        assert seen == {"limit": "2", "cursor": "cursor-1"}
    finally:
        c.close()
        s.close()


# ── Routing ──


def test_recommend() -> None:
    s = FakeAtlas()
    seen: Dict[str, Any] = {}

    def rec(req: Dict[str, Any]) -> Tuple[int, Any]:
        seen.update(req["query"])
        return 200, {"server": {"id": "game-1", "status": "online"}, "reason": "lowest_load"}

    s.routes.append(route("GET", r"/v1/routing/recommended", rec))
    c = client_for(s)
    try:
        out = c.recommend(account_id=42, region="cn-east", version="1.0.0", platform="pc")
        assert out.server.id == "game-1" and out.reason == "lowest_load"
        assert seen == {"account_id": "42", "region": "cn-east",
                        "version": "1.0.0", "platform": "pc"}

        seen.clear()
        c.recommend()
        assert seen == {}  # zero-value args are omitted
    finally:
        c.close()
        s.close()


# ── Admin ──


def test_admin() -> None:
    s = FakeAtlas()
    s.routes.append(route("POST", r"/v1/admin/servers/game-1/maintenance", lambda q: (
        200, {"server_id": "game-1", "status": "maintenance"})))
    s.routes.append(route("POST", r"/v1/admin/servers/game-1/enable", lambda q: (
        200, {"server_id": "game-1", "status": "online"})))
    s.routes.append(route("GET", r"/v1/admin/stats", lambda q: (
        200, {"total_servers": 2, "servers_by_status": {"online": 2},
              "servers_by_region": {"cn-east": 2}, "servers_by_version": {"1.0.0": 2},
              "total_players": 50, "total_capacity": 4000, "total_characters": 9})))
    s.routes.append(route("GET", r"/v1/admin/characters/search", lambda q: (
        200, {"characters": [{"character_id": 1001, "name": "Hero"}], "next_cursor": ""})))
    s.routes.append(route("POST", r"/v1/admin/migrations", lambda q: (
        200, {"migration": {"id": "m-1", "source_servers": ["a"], "target_server": "b",
                            "status": "pending"}})))
    s.routes.append(route("GET", r"/v1/admin/migrations/m-1", lambda q: (
        200, {"migration": {"id": "m-1", "status": "completed"}})))
    s.routes.append(route("GET", r"/v1/admin/migrations", lambda q: (
        200, {"migrations": [{"id": "m-1"}], })))
    s.routes.append(route("POST", r"/v1/admin/migrations/m-1/rollback", lambda q: (
        200, {"migration": {"id": "m-1", "status": "rolled_back"}})))
    c = client_for(s)
    try:
        assert c.set_maintenance("game-1").status == "maintenance"
        assert c.enable("game-1").status == "online"
        life = body_of(s, "POST", r"/v1/admin/servers/game-1/maintenance")
        assert life["headers"]["Authorization"] == "Bearer adm-key"

        stats = c.get_stats()
        assert (stats.total_servers, stats.total_characters) == (2, 9)
        assert stats.servers_by_status == {"online": 2}

        page = c.search_characters(CharacterFilter(name="Hero", min_level=10, limit=5))
        assert page.characters[0].name == "Hero"
        search = body_of(s, "GET", r"/v1/admin/characters/search")
        assert search["query"] == {"name": "Hero", "min_level": "10", "limit": "5"}

        mig = c.create_migration(CreateMigrationRequest(["a"], "b"))
        assert (mig.id, mig.status) == ("m-1", "pending")
        assert c.get_migration("m-1").status == "completed"
        assert len(c.list_migrations(limit=10)) == 1
        assert c.rollback_migration("m-1").status == "rolled_back"
    finally:
        c.close()
        s.close()


# ── Retry & network errors ──


def test_retry_on_transient() -> None:
    s = FakeAtlas()
    attempts = {"flaky": 0, "missing": 0}

    def get(req: Dict[str, Any]) -> Tuple[int, Any]:
        sid = req["path"].rsplit("/", 1)[-1]
        attempts[sid] += 1
        if sid == "flaky":
            if attempts["flaky"] < 3:
                return 502, {"error": {"code": "BAD_GATEWAY", "message": "try again"}}
            return 200, {"id": "flaky", "status": "online"}
        return 404, {"error": {"code": "SERVER_NOT_FOUND", "message": "x"}}

    s.routes.append(route("GET", r"/v1/discovery/servers/.*", get))
    c = client_for(s)
    try:
        assert c.get_server("flaky").id == "flaky"
        assert attempts["flaky"] == 3  # 2 failures + success

        with pytest.raises(AtlasError):
            c.get_server("missing")
        assert attempts["missing"] == 1  # 4xx must not be retried
    finally:
        c.close()
        s.close()


def test_network_error() -> None:
    # Port 1 is unroutable — expect a mapped NETWORK error, not an exception leak.
    c = AtlasClient("http://127.0.0.1:1", max_retries=0, base_backoff=0.001)
    try:
        with pytest.raises(AtlasError) as exc:
            c.list_servers()
        assert exc.value.status == 0 and exc.value.code == "NETWORK"
    finally:
        c.close()


def test_http_error_without_json_body() -> None:
    s = FakeAtlas()
    s.routes.append(route(
        "GET", r"/v1/admin/stats", lambda q: (500, "boom")))
    c = client_for(s, max_retries=0)
    try:
        with pytest.raises(AtlasError) as exc:
            c.get_stats()
        assert exc.value.code == "HTTP_500"
    finally:
        c.close()
        s.close()


# ── Auto heartbeat (thread) ──


def test_auto_heartbeat() -> None:
    s = FakeAtlas()
    beats: List[Dict[str, Any]] = []
    lock = threading.Lock()

    def beat(req: Dict[str, Any]) -> Tuple[int, Any]:
        with lock:
            beats.append(req["body"])
        return 200, {"server_id": "game-1", "status": "online"}

    s.routes.append(route("POST", r"/v1/registry/servers/game-1/heartbeat", beat))
    c = client_for(s)
    try:
        loop = c.start_heartbeat("game-1", interval=0.05, players=5)
        loop.set(42, 0.5)  # picked up by the first or second beat
        deadline = time.monotonic() + 2.0
        while len(beats) < 2 and time.monotonic() < deadline:
            time.sleep(0.01)
        loop.stop()
        loop.stop()  # idempotent

        with lock:
            assert len(beats) >= 2
            assert beats[-1] == {"players": 42, "load": 0.5}
    finally:
        c.close()
        s.close()


def test_auto_heartbeat_error_callback() -> None:
    errors: List[AtlasError] = []
    c = AtlasClient("http://127.0.0.1:1", max_retries=0, base_backoff=0.001)
    try:
        loop = c.start_heartbeat("game-1", interval=0.02)
        loop.on_error = errors.append
        deadline = time.monotonic() + 2.0
        while not errors and time.monotonic() < deadline:
            time.sleep(0.01)
        loop.stop()
        assert errors and errors[0].code == "NETWORK"
    finally:
        c.close()


# ── Async client ──


def test_async_lifecycle_and_heartbeat() -> None:
    async def run() -> None:
        s = FakeAtlas()
        s.routes.append(route(
            "POST", r"/v1/registry/servers/register",
            lambda q: (200, {"server_id": "game-9", "status": "online"})))
        beats: List[Dict[str, Any]] = []

        def beat(req: Dict[str, Any]) -> Tuple[int, Any]:
            beats.append(req["body"])
            return 200, {"server_id": "game-9", "status": "online"}

        s.routes.append(route("POST", r"/v1/registry/servers/game-9/heartbeat", beat))
        s.routes.append(route("POST", r"/v1/registry/servers/game-9/unregister", lambda q: (
            200, {"server_id": "game-9", "status": "offline"})))
        s.routes.append(route(
            "GET", r"/v1/discovery/servers",
            lambda q: (200, {"servers": [{"id": "game-9", "status": "online"}]})))
        s.routes.append(route("POST", r"/v1/directory/characters", lambda q: (
            201, {"account_id": 1, "server_id": "game-9", "character_id": 1001,
                  "name": "Hero", "level": 1})))
        s.routes.append(route("GET", r"/v1/routing/recommended", lambda q: (
            200, {"server": {"id": "game-9"}, "reason": "lowest_load"})))
        s.routes.append(route("GET", r"/v1/admin/stats", lambda q: (
            200, {"total_servers": 1})))

        async with AtlasAsyncClient(
            s.url, registry_token="reg-token", admin_api_key="adm-key",
            base_backoff=0.001,
        ) as c:
            reg = await c.register(RegisterRequest(
                server_id="game-9", name="G", region="cn-east", capacity=100,
                endpoint=Endpoint(host="10.0.0.9", port=30009)))
            assert reg.server_id == "game-9"

            loop = await c.start_heartbeat("game-9", interval=0.05, players=7)
            loop.set(8, 0.4)
            deadline = time.monotonic() + 2.0
            while len(beats) < 2 and time.monotonic() < deadline:
                await asyncio.sleep(0.01)
            await loop.stop()

            assert len(await c.list_servers()) == 1
            wr = await c.create_character(CreateCharacterRequest(
                account_id=1, server_id="game-9", character_id=1001, name="Hero"))
            assert wr.status == "created" and wr.character is not None
            rec = await c.recommend(42)
            assert rec.server.id == "game-9"
            assert (await c.get_stats()).total_servers == 1
            await c.unregister("game-9")
        s.close()

        assert len(beats) >= 2 and beats[-1]["players"] == 8

    asyncio.run(run())
