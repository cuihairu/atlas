"""Atlas async client — httpx.AsyncClient + asyncio (TODO v0.1.8)."""

from __future__ import annotations

import asyncio
import random
from typing import Any, Dict, List, Optional

import httpx

from .client import AtlasError, _Config, _safe_json
from .types import (
    Character,
    CharacterFilter,
    CharacterPage,
    CharacterWriteResult,
    CreateCharacterRequest,
    CreateMigrationRequest,
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

__all__ = ["AtlasAsyncClient"]


class AtlasAsyncClient:
    """Async twin of :class:`atlas_client.client.AtlasClient` (same API,
    coroutine methods)."""

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
    ) -> None:
        self._t = _Config(
            registry_token, admin_api_key, timeout, max_retries, base_backoff
        )
        # httpx merges absolute paths onto the base host.
        self._http = httpx.AsyncClient(base_url=base_url, timeout=timeout, headers=self._t.headers)
        self._registry_http = (
            self._http
            if not registry_base_url
            else httpx.AsyncClient(
                base_url=registry_base_url, timeout=timeout, headers=self._t.headers
            )
        )

    async def aclose(self) -> None:
        await self._http.aclose()
        if self._registry_http is not self._http:
            await self._registry_http.aclose()

    async def __aenter__(self) -> "AtlasAsyncClient":
        return self

    async def __aexit__(self, *exc: Any) -> None:
        await self.aclose()

    async def _request(
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
                resp = await http.request(
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
                if not _Config.is_transient_status(resp.status_code):
                    raise self._t.parse_error(resp.status_code, _safe_json(resp))
                if attempt >= max(self._t.max_retries, 0):
                    raise self._t.parse_error(resp.status_code, _safe_json(resp))
            delay = min(self._t.base_backoff * (2**attempt), 10.0)
            await asyncio.sleep(random.uniform(0, delay))
            attempt += 1

    # ── Registry ──

    async def register(self, req: RegisterRequest) -> RegisterResult:
        body = await self._request(
            "POST", "/v1/registry/servers/register", json_body=req.to_dict(),
            bearer=self._t.registry_token, registry=True,
        ) or {}
        return RegisterResult(
            server_id=body.get("server_id", ""), status=body.get("status", "")
        )

    async def heartbeat(self, server_id: str, req: HeartbeatRequest) -> HeartbeatResult:
        body = await self._request(
            "POST", f"/v1/registry/servers/{server_id}/heartbeat",
            json_body={"players": req.players, "load": req.load},
            bearer=self._t.registry_token, registry=True,
        ) or {}
        return HeartbeatResult(
            server_id=body.get("server_id", server_id),
            status=body.get("status", ""),
            next_heartbeat_in=int(body.get("next_heartbeat_in", 0)),
        )

    async def unregister(self, server_id: str) -> StatusResult:
        body = await self._request(
            "POST", f"/v1/registry/servers/{server_id}/unregister",
            bearer=self._t.registry_token, registry=True,
        ) or {}
        return StatusResult(server_id=body.get("server_id"), status=body.get("status", ""))

    # ── Discovery ──

    async def list_servers(self, filter_: Optional[ServerFilter] = None) -> List[Server]:
        body = await self._request(
            "GET", "/v1/discovery/servers", params=(filter_ or ServerFilter()).params()
        ) or {}
        return [Server.from_dict(s) for s in (body.get("servers") or [])]

    async def get_server(self, server_id: str) -> Server:
        body = await self._request("GET", f"/v1/discovery/servers/{server_id}")
        return Server.from_dict(body or {})

    # ── Directory ──

    async def create_character(self, req: CreateCharacterRequest) -> CharacterWriteResult:
        body = await self._request(
            "POST", "/v1/directory/characters", json_body=req.to_dict()
        )
        return _Config.parse_character_write(body, "created")

    async def get_character(self, character_id: int) -> Character:
        body = await self._request("GET", f"/v1/directory/characters/{character_id}")
        return Character.from_dict(body or {})

    async def list_characters_by_account(self, account_id: int) -> List[Character]:
        body = await self._request(
            "GET", f"/v1/directory/accounts/{account_id}/characters"
        ) or {}
        return [Character.from_dict(c) for c in (body.get("characters") or [])]

    async def list_characters_by_server(
        self, server_id: str, limit: int = 0, cursor: str = ""
    ) -> CharacterPage:
        params: Dict[str, str] = {}
        if limit > 0:
            params["limit"] = str(limit)
        if cursor:
            params["cursor"] = cursor
        body = await self._request(
            "GET", f"/v1/directory/servers/{server_id}/characters", params=params
        ) or {}
        return CharacterPage(
            characters=[Character.from_dict(c) for c in (body.get("characters") or [])],
            next_cursor=body.get("next_cursor", ""),
        )

    async def update_character(
        self, character_id: int, req: UpdateCharacterRequest
    ) -> CharacterWriteResult:
        body = await self._request(
            "PATCH", f"/v1/directory/characters/{character_id}", json_body=req.to_dict()
        )
        return _Config.parse_character_write(body, "updated")

    async def delete_character(self, character_id: int) -> CharacterWriteResult:
        body = await self._request(
            "DELETE", f"/v1/directory/characters/{character_id}"
        ) or {}
        return CharacterWriteResult(status=body.get("status", "deleted"))

    # ── Routing ──

    async def recommend(
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
        body = await self._request("GET", "/v1/routing/recommended", params=params) or {}
        return Recommendation(
            server=Server.from_dict(body.get("server") or {}),
            reason=body.get("reason", ""),
        )

    # ── Admin ──

    async def _lifecycle(self, action: str, server_id: str) -> StatusResult:
        body = await self._request(
            "POST", f"/v1/admin/servers/{server_id}/{action}",
            bearer=self._t.admin_api_key,
        ) or {}
        return StatusResult(server_id=body.get("server_id"), status=body.get("status", ""))

    async def set_maintenance(self, server_id: str) -> StatusResult:
        return await self._lifecycle("maintenance", server_id)

    async def set_drain(self, server_id: str) -> StatusResult:
        return await self._lifecycle("drain", server_id)

    async def enable(self, server_id: str) -> StatusResult:
        return await self._lifecycle("enable", server_id)

    async def disable(self, server_id: str) -> StatusResult:
        return await self._lifecycle("disable", server_id)

    async def get_stats(self) -> Stats:
        body = await self._request(
            "GET", "/v1/admin/stats", bearer=self._t.admin_api_key
        ) or {}
        return Stats.from_dict(body)

    async def search_characters(self, filter_: CharacterFilter) -> CharacterPage:
        body = await self._request(
            "GET", "/v1/admin/characters/search", params=filter_.params(),
            bearer=self._t.admin_api_key,
        ) or {}
        return CharacterPage(
            characters=[Character.from_dict(c) for c in (body.get("characters") or [])],
            next_cursor=body.get("next_cursor", ""),
        )

    async def create_migration(self, req: CreateMigrationRequest) -> Migration:
        body = await self._request(
            "POST", "/v1/admin/migrations", json_body=req.to_dict(),
            bearer=self._t.admin_api_key,
        ) or {}
        return Migration.from_dict(body.get("migration") or {})

    async def get_migration(self, migration_id: str) -> Migration:
        body = await self._request(
            "GET", f"/v1/admin/migrations/{migration_id}",
            bearer=self._t.admin_api_key,
        ) or {}
        return Migration.from_dict(body.get("migration") or {})

    async def list_migrations(self, limit: int = 0) -> List[Migration]:
        params = {"limit": str(limit)} if limit > 0 else {}
        body = await self._request(
            "GET", "/v1/admin/migrations", params=params,
            bearer=self._t.admin_api_key,
        ) or {}
        return [Migration.from_dict(m) for m in (body.get("migrations") or [])]

    async def rollback_migration(self, migration_id: str) -> Migration:
        body = await self._request(
            "POST", f"/v1/admin/migrations/{migration_id}/rollback",
            bearer=self._t.admin_api_key,
        ) or {}
        return Migration.from_dict(body.get("migration") or {})

    # ── Auto heartbeat ──

    async def start_heartbeat(
        self,
        server_id: str,
        interval: float = 10.0,
        players: int = 0,
        load: float = 0.0,
    ) -> "AsyncHeartbeatTask":
        if interval <= 0:
            interval = 10.0
        task = AsyncHeartbeatTask(self, server_id, interval, HeartbeatRequest(players, load))
        task._start()
        return task


class AsyncHeartbeatTask:
    """asyncio heartbeat loop: immediate first report, then interval."""

    def __init__(
        self,
        client: AtlasAsyncClient,
        server_id: str,
        interval: float,
        initial: HeartbeatRequest,
    ) -> None:
        self._client = client
        self._server_id = server_id
        self._interval = interval
        self._payload = initial
        self.on_error: Optional[Any] = None  # Callable[[AtlasError], None] (sync fn ok)
        self._task: Optional[asyncio.Task[None]] = None
        self._stopping = asyncio.Event()

    def _start(self) -> None:
        self._task = asyncio.get_running_loop().create_task(self._run())

    def set(self, players: int, load: float) -> None:
        self._payload = HeartbeatRequest(players, load)

    async def stop(self) -> None:
        self._stopping.set()
        if self._task is not None:
            await self._task

    async def _run(self) -> None:
        await self._beat()
        while not self._stopping.is_set():
            try:
                await asyncio.wait_for(self._stopping.wait(), timeout=self._interval)
            except asyncio.TimeoutError:
                pass
            else:
                return
            await self._beat()

    async def _beat(self) -> None:
        try:
            await self._client.heartbeat(self._server_id, self._payload)
        except AtlasError as exc:
            if self.on_error is not None:
                self.on_error(exc)
