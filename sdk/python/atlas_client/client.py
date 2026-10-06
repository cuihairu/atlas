"""Atlas sync client (TODO v0.1.8) — httpx transport, REST API.

Same API surface as the Go/C++ SDKs: five API groups (Registry /
Discovery / Directory / Routing / Admin), transient-failure retry with
full-jitter backoff, and an auto-heartbeat background thread.
"""

from __future__ import annotations

import random
import threading
import time
from typing import Any, Callable, Dict, List, Optional

import httpx

from .types import (
    Character,
    CharacterFilter,
    CharacterPage,
    CharacterWriteResult,
    CreateCharacterRequest,
    CreateMigrationRequest,
    Endpoint,
    HeartbeatRequest,
    HeartbeatResult,
    Migration,
    Recommendation,
    RegisterRequest,
    RegisterResult,
    Server,
    ServerFilter,
    Stats,
    StatusResult,
    UpdateCharacterRequest,
)

__all__ = ["AtlasError", "AtlasClient", "AutoHeartbeat"]


class AtlasError(Exception):
    """Atlas API error. ``code`` mirrors the REST error codes
    ("SERVER_NOT_FOUND", ...) or "HTTP_<status>" / "NETWORK" / "PROTOCOL"."""

    def __init__(self, status: int, code: str, message: str) -> None:
        super().__init__(f"{code}: {message}")
        self.status = status
        self.code = code
        self.message = message


class _Config:
    """Shared options and parsing helpers for the sync and async clients.

    Each client builds its own httpx pair (sync or async) on top of this.
    """

    def __init__(
        self,
        registry_token: Optional[str],
        admin_api_key: Optional[str],
        timeout: float,
        max_retries: int,
        base_backoff: float,
        default_headers: Optional[Dict[str, str]] = None,
    ) -> None:
        self.registry_token = registry_token or ""
        self.admin_api_key = admin_api_key or ""
        self.max_retries = max_retries
        self.base_backoff = base_backoff
        self.headers = {"Accept": "application/json"}
        if default_headers:
            # Static headers sent on every call (docs/api.md 请求追踪) —
            # e.g. a process-level X-Request-ID correlation id.
            self.headers.update(default_headers)

    @staticmethod
    def is_transient_status(status: int) -> bool:
        return status >= 500

    @staticmethod
    def backoff(base: float, attempt: int) -> float:
        ceiling = min(base * (2**attempt), 10.0)
        return random.uniform(0, ceiling)

    @staticmethod
    def parse_error(status: int, body: Any) -> "AtlasError":
        if isinstance(body, dict) and isinstance(body.get("error"), dict):
            err = body["error"]
            return AtlasError(status, err.get("code", ""), err.get("message", ""))
        return AtlasError(status, f"HTTP_{status}", str(body)[:200])

    @staticmethod
    def parse_character_write(body: Any, sync_status: str) -> CharacterWriteResult:
        """Normalize the three reply shapes: nested envelope, flat
        synchronous character object, and status-only queued/deleted."""
        if not isinstance(body, dict):
            return CharacterWriteResult(status=sync_status)
        nested = body.get("character")
        if isinstance(nested, dict):
            return CharacterWriteResult(
                character=Character.from_dict(nested),
                status=body.get("status") or sync_status,
            )
        if "character_id" in body:
            return CharacterWriteResult(
                character=Character.from_dict(body), status=sync_status
            )
        return CharacterWriteResult(status=body.get("status", ""))


class AtlasClient:
    """Atlas REST client (sync). Methods raise :class:`AtlasError`."""

    def __init__(
        self,
        base_url: str = "http://localhost:8080",
        *,
        registry_base_url: Optional[str] = None,
        registry_token: Optional[str] = None,
        admin_api_key: Optional[str] = None,
        timeout: float = 10.0,
        max_retries: int = 3,
        base_backoff: float = 0.1,
        default_headers: Optional[Dict[str, str]] = None,
    ) -> None:
        self._t = _Config(
            registry_token, admin_api_key, timeout, max_retries, base_backoff,
            default_headers,
        )
        # httpx merges absolute paths onto the base host.
        self._http = httpx.Client(base_url=base_url, timeout=timeout, headers=self._t.headers)
        self._registry_http = (
            self._http
            if not registry_base_url
            else httpx.Client(
                base_url=registry_base_url, timeout=timeout, headers=self._t.headers
            )
        )

    def close(self) -> None:
        self._http.close()
        if self._registry_http is not self._http:
            self._registry_http.close()

    def __enter__(self) -> "AtlasClient":
        return self

    def __exit__(self, *exc: Any) -> None:
        self.close()

    # ── core request path ──

    def _request(
        self,
        method: str,
        path: str,
        *,
        json_body: Any = None,
        bearer: str = "",
        registry: bool = False,
        params: Optional[Dict[str, str]] = None,
    ) -> Any:
        http = self._registry_http if registry else self._http
        headers = {"Authorization": f"Bearer {bearer}"} if bearer else {}
        attempt = 0
        while True:
            try:
                resp = http.request(
                    method, path, json=json_body, params=params, headers=headers
                )
            except httpx.TransportError as exc:
                if attempt >= max(self._t.max_retries, 0):
                    raise AtlasError(0, "NETWORK", f"{method} {path}: {exc}") from exc
            else:
                if resp.status_code < 400:
                    if resp.status_code == 204 or not resp.content:
                        return None
                    return resp.json()
                if not self._t.is_transient_status(resp.status_code):
                    raise self._t.parse_error(resp.status_code, _safe_json(resp))
                if attempt >= max(self._t.max_retries, 0):
                    raise self._t.parse_error(resp.status_code, _safe_json(resp))
            time.sleep(self._t.backoff(self._t.base_backoff, attempt))
            attempt += 1

    # ── Registry (service token, split port) ──

    def register(self, req: RegisterRequest) -> RegisterResult:
        body = self._request(
            "POST", "/v1/registry/servers/register", json_body=req.to_dict(),
            bearer=self._t.registry_token, registry=True,
        ) or {}
        return RegisterResult(
            server_id=body.get("server_id", ""), status=body.get("status", "")
        )

    def heartbeat(self, server_id: str, req: HeartbeatRequest) -> HeartbeatResult:
        body = self._request(
            "POST", f"/v1/registry/servers/{server_id}/heartbeat",
            json_body={"players": req.players, "load": req.load},
            bearer=self._t.registry_token, registry=True,
        ) or {}
        return HeartbeatResult(
            server_id=body.get("server_id", server_id),
            status=body.get("status", ""),
            next_heartbeat_in=int(body.get("next_heartbeat_in", 0)),
        )

    def unregister(self, server_id: str) -> StatusResult:
        body = self._request(
            "POST", f"/v1/registry/servers/{server_id}/unregister",
            bearer=self._t.registry_token, registry=True,
        ) or {}
        return StatusResult(
            server_id=body.get("server_id"), status=body.get("status", "")
        )

    # ── Discovery (public) ──

    def list_servers(self, filter_: Optional[ServerFilter] = None) -> List[Server]:
        body = self._request(
            "GET", "/v1/discovery/servers",
            params=(filter_ or ServerFilter()).params(),
        ) or {}
        return [Server.from_dict(s) for s in (body.get("servers") or [])]

    def get_server(self, server_id: str) -> Server:
        body = self._request("GET", f"/v1/discovery/servers/{server_id}")
        return Server.from_dict(body or {})

    # ── Directory (public) ──

    def create_character(self, req: CreateCharacterRequest) -> CharacterWriteResult:
        body = self._request("POST", "/v1/directory/characters", json_body=req.to_dict())
        return self._t.parse_character_write(body, "created")

    def get_character(self, character_id: int) -> Character:
        body = self._request("GET", f"/v1/directory/characters/{character_id}")
        return Character.from_dict(body or {})

    def list_characters_by_account(self, account_id: int) -> List[Character]:
        body = self._request(
            "GET", f"/v1/directory/accounts/{account_id}/characters"
        ) or {}
        return [Character.from_dict(c) for c in (body.get("characters") or [])]

    def list_characters_by_server(
        self, server_id: str, limit: int = 0, cursor: str = ""
    ) -> CharacterPage:
        params: Dict[str, str] = {}
        if limit > 0:
            params["limit"] = str(limit)
        if cursor:
            params["cursor"] = cursor
        body = self._request(
            "GET", f"/v1/directory/servers/{server_id}/characters", params=params
        ) or {}
        return CharacterPage(
            characters=[Character.from_dict(c) for c in (body.get("characters") or [])],
            next_cursor=body.get("next_cursor", ""),
        )

    def update_character(
        self, character_id: int, req: UpdateCharacterRequest
    ) -> CharacterWriteResult:
        body = self._request(
            "PATCH", f"/v1/directory/characters/{character_id}", json_body=req.to_dict()
        )
        return self._t.parse_character_write(body, "updated")

    def delete_character(self, character_id: int) -> CharacterWriteResult:
        body = self._request(
            "DELETE", f"/v1/directory/characters/{character_id}"
        ) or {}
        return CharacterWriteResult(status=body.get("status", "deleted"))

    # ── Routing (public) ──

    def recommend(
        self,
        account_id: int = 0,
        region: str = "",
        version: str = "",
        platform: str = "",
    ) -> Recommendation:
        params: Dict[str, str] = {}
        if account_id > 0:
            params["account_id"] = str(account_id)
        for key, value in (("region", region), ("version", version), ("platform", platform)):
            if value:
                params[key] = value
        body = self._request("GET", "/v1/routing/recommended", params=params) or {}
        return Recommendation(
            server=Server.from_dict(body.get("server") or {}),
            reason=body.get("reason", ""),
        )

    # ── Admin (API key) ──

    def _lifecycle(self, action: str, server_id: str) -> StatusResult:
        body = self._request(
            "POST", f"/v1/admin/servers/{server_id}/{action}",
            bearer=self._t.admin_api_key,
        ) or {}
        return StatusResult(
            server_id=body.get("server_id"), status=body.get("status", "")
        )

    def set_maintenance(self, server_id: str) -> StatusResult:
        return self._lifecycle("maintenance", server_id)

    def set_drain(self, server_id: str) -> StatusResult:
        return self._lifecycle("drain", server_id)

    def enable(self, server_id: str) -> StatusResult:
        return self._lifecycle("enable", server_id)

    def disable(self, server_id: str) -> StatusResult:
        return self._lifecycle("disable", server_id)

    def get_stats(self) -> Stats:
        body = self._request(
            "GET", "/v1/admin/stats", bearer=self._t.admin_api_key
        ) or {}
        return Stats.from_dict(body)

    def search_characters(self, filter_: CharacterFilter) -> CharacterPage:
        body = self._request(
            "GET", "/v1/admin/characters/search", params=filter_.params(),
            bearer=self._t.admin_api_key,
        ) or {}
        return CharacterPage(
            characters=[Character.from_dict(c) for c in (body.get("characters") or [])],
            next_cursor=body.get("next_cursor", ""),
        )

    def create_migration(self, req: CreateMigrationRequest) -> Migration:
        body = self._request(
            "POST", "/v1/admin/migrations", json_body=req.to_dict(),
            bearer=self._t.admin_api_key,
        ) or {}
        return Migration.from_dict(body.get("migration") or {})

    def get_migration(self, migration_id: str) -> Migration:
        body = self._request(
            "GET", f"/v1/admin/migrations/{migration_id}",
            bearer=self._t.admin_api_key,
        ) or {}
        return Migration.from_dict(body.get("migration") or {})

    def list_migrations(self, limit: int = 0) -> List[Migration]:
        params = {"limit": str(limit)} if limit > 0 else {}
        body = self._request(
            "GET", "/v1/admin/migrations", params=params,
            bearer=self._t.admin_api_key,
        ) or {}
        return [Migration.from_dict(m) for m in (body.get("migrations") or [])]

    def rollback_migration(self, migration_id: str) -> Migration:
        body = self._request(
            "POST", f"/v1/admin/migrations/{migration_id}/rollback",
            bearer=self._t.admin_api_key,
        ) or {}
        return Migration.from_dict(body.get("migration") or {})

    # ── Auto heartbeat ──

    def start_heartbeat(
        self,
        server_id: str,
        interval: float = 10.0,
        players: int = 0,
        load: float = 0.0,
    ) -> "AutoHeartbeat":
        """Start reporting immediately, then every ``interval`` seconds."""
        return AutoHeartbeat(self, server_id, interval, HeartbeatRequest(players, load))


class AutoHeartbeat:
    """Background heartbeat loop (immediate first report, then interval).

    Update the payload from the game thread with :meth:`set`; failures
    are surfaced through ``on_error`` and the loop keeps running.
    """

    def __init__(
        self,
        client: AtlasClient,
        server_id: str,
        interval: float,
        initial: HeartbeatRequest,
    ) -> None:
        if interval <= 0:
            interval = 10.0
        self._client = client
        self._server_id = server_id
        self._interval = interval
        self._payload = initial
        self._lock = threading.Lock()
        self._stop_event = threading.Event()
        self.on_error: Optional[Callable[[AtlasError], None]] = None
        self._thread = threading.Thread(target=self._run, daemon=True)
        self._thread.start()

    def set(self, players: int, load: float) -> None:
        with self._lock:
            self._payload = HeartbeatRequest(players, load)

    def stop(self) -> None:
        self._stop_event.set()
        if self._thread.is_alive():
            self._thread.join(timeout=self._interval + 5)

    def _run(self) -> None:
        self._beat()
        while not self._stop_event.wait(self._interval):
            self._beat()

    def _beat(self) -> None:
        with self._lock:
            req = self._payload
        try:
            self._client.heartbeat(self._server_id, req)
        except AtlasError as exc:
            if self.on_error is not None:
                self.on_error(exc)


def _safe_json(resp: httpx.Response) -> Any:
    try:
        return resp.json()
    except ValueError:
        return resp.text
