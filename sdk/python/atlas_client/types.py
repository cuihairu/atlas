"""Atlas SDK data types — mirrors the Go/C++ SDKs and docs/api.md JSON."""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime
from typing import Any, Dict, List, Optional


def _parse_time(value: Any) -> Optional[datetime]:
    if not value:
        return None
    try:
        return datetime.fromisoformat(str(value).replace("Z", "+00:00"))
    except ValueError:
        return None


@dataclass
class Endpoint:
    host: str = ""
    port: int = 0

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Endpoint":
        d = d or {}
        return cls(host=d.get("host", ""), port=int(d.get("port", 0)))


@dataclass
class Server:
    id: str = ""
    name: str = ""
    type: str = ""
    region: str = ""
    realm_id: Optional[str] = None
    shard_id: Optional[str] = None
    version: str = ""
    platform: str = ""
    endpoint: Endpoint = field(default_factory=Endpoint)
    capacity: int = 0
    metadata: Dict[str, str] = field(default_factory=dict)
    status: str = ""
    players: int = 0
    load: float = 0.0
    last_seen_at: Optional[datetime] = None
    created_at: Optional[datetime] = None
    updated_at: Optional[datetime] = None

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Server":
        return cls(
            id=d.get("id", ""),
            name=d.get("name", ""),
            type=d.get("type", ""),
            region=d.get("region", ""),
            realm_id=d.get("realm_id"),
            shard_id=d.get("shard_id"),
            version=d.get("version", ""),
            platform=d.get("platform", ""),
            endpoint=Endpoint.from_dict(d.get("endpoint")),
            capacity=int(d.get("capacity", 0)),
            metadata=d.get("metadata") or {},
            status=d.get("status", ""),
            players=int(d.get("players", 0)),
            load=float(d.get("load", 0.0)),
            last_seen_at=_parse_time(d.get("last_seen_at")),
            created_at=_parse_time(d.get("created_at")),
            updated_at=_parse_time(d.get("updated_at")),
        )


@dataclass
class ServerFilter:
    region: Optional[str] = None
    version: Optional[str] = None
    platform: Optional[str] = None
    status: Optional[str] = None
    limit: Optional[int] = None

    def params(self) -> Dict[str, str]:
        out: Dict[str, str] = {}
        for key in ("region", "version", "platform", "status"):
            value = getattr(self, key)
            if value:
                out[key] = value
        if self.limit and self.limit > 0:
            out["limit"] = str(self.limit)
        return out


@dataclass
class RegisterRequest:
    server_id: str
    name: str
    region: str
    endpoint: Endpoint
    capacity: int
    type: str = "game"
    version: str = ""
    platform: str = ""
    realm_id: Optional[str] = None
    shard_id: Optional[str] = None
    metadata: Dict[str, str] = field(default_factory=dict)

    def to_dict(self) -> Dict[str, Any]:
        d: Dict[str, Any] = {
            "server_id": self.server_id,
            "name": self.name,
            "type": self.type,
            "region": self.region,
            "version": self.version,
            "platform": self.platform,
            "endpoint": {"host": self.endpoint.host, "port": self.endpoint.port},
            "capacity": self.capacity,
        }
        if self.realm_id:
            d["realm_id"] = self.realm_id
        if self.shard_id:
            d["shard_id"] = self.shard_id
        if self.metadata:
            d["metadata"] = self.metadata
        return d


@dataclass
class RegisterResult:
    server_id: str = ""
    status: str = ""


@dataclass
class HeartbeatRequest:
    players: int = 0
    load: float = 0.0


@dataclass
class HeartbeatResult:
    server_id: str = ""
    status: str = ""
    next_heartbeat_in: int = 0


@dataclass
class StatusResult:
    server_id: Optional[str] = None
    status: str = ""


@dataclass
class Character:
    account_id: int = 0
    server_id: str = ""
    character_id: int = 0
    name: str = ""
    level: int = 0
    class_id: int = 0
    avatar: str = ""
    metadata: Dict[str, str] = field(default_factory=dict)
    last_login_at: Optional[datetime] = None
    created_at: Optional[datetime] = None
    updated_at: Optional[datetime] = None

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Character":
        return cls(
            account_id=int(d.get("account_id", 0)),
            server_id=d.get("server_id", ""),
            character_id=int(d.get("character_id", 0)),
            name=d.get("name", ""),
            level=int(d.get("level", 0)),
            class_id=int(d.get("class_id", 0)),
            avatar=d.get("avatar", ""),
            metadata=d.get("metadata") or {},
            last_login_at=_parse_time(d.get("last_login_at")),
            created_at=_parse_time(d.get("created_at")),
            updated_at=_parse_time(d.get("updated_at")),
        )


@dataclass
class CreateCharacterRequest:
    account_id: int
    server_id: str
    character_id: int
    name: str
    level: Optional[int] = None
    class_id: Optional[int] = None
    avatar: Optional[str] = None

    def to_dict(self) -> Dict[str, Any]:
        d: Dict[str, Any] = {
            "account_id": self.account_id,
            "server_id": self.server_id,
            "character_id": self.character_id,
            "name": self.name,
        }
        if self.level is not None:
            d["level"] = self.level
        if self.class_id is not None:
            d["class_id"] = self.class_id
        if self.avatar is not None:
            d["avatar"] = self.avatar
        return d


@dataclass
class UpdateCharacterRequest:
    """PATCH body — unset fields are left unchanged."""

    name: Optional[str] = None
    level: Optional[int] = None
    class_id: Optional[int] = None
    avatar: Optional[str] = None

    def to_dict(self) -> Dict[str, Any]:
        d: Dict[str, Any] = {}
        if self.name is not None:
            d["name"] = self.name
        if self.level is not None:
            d["level"] = self.level
        if self.class_id is not None:
            d["class_id"] = self.class_id
        if self.avatar is not None:
            d["avatar"] = self.avatar
        return d


@dataclass
class CharacterWriteResult:
    """Directory write reply; ``character`` is None when status == "queued"."""

    character: Optional[Character] = None
    status: str = ""  # created | updated | deleted | queued


@dataclass
class CharacterPage:
    characters: List[Character] = field(default_factory=list)
    next_cursor: str = ""


@dataclass
class CharacterFilter:
    name: Optional[str] = None
    server_id: Optional[str] = None
    class_id: Optional[int] = None
    min_level: Optional[int] = None
    max_level: Optional[int] = None
    limit: Optional[int] = None
    cursor: Optional[str] = None

    def params(self) -> Dict[str, str]:
        out: Dict[str, str] = {}
        for key in ("name", "server_id", "cursor"):
            value = getattr(self, key)
            if value:
                out[key] = value
        for key in ("class_id", "min_level", "max_level", "limit"):
            value = getattr(self, key)
            if value is not None and value > 0:
                out[key] = str(value)
        return out


@dataclass
class Recommendation:
    server: Server = field(default_factory=Server)
    reason: str = ""  # lowest_load | highest_capacity | has_character | fallback


@dataclass
class Stats:
    total_servers: int = 0
    servers_by_status: Dict[str, int] = field(default_factory=dict)
    servers_by_region: Dict[str, int] = field(default_factory=dict)
    servers_by_version: Dict[str, int] = field(default_factory=dict)
    total_players: int = 0
    total_capacity: int = 0
    total_characters: int = 0

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Stats":
        return cls(
            total_servers=int(d.get("total_servers", 0)),
            servers_by_status=d.get("servers_by_status") or {},
            servers_by_region=d.get("servers_by_region") or {},
            servers_by_version=d.get("servers_by_version") or {},
            total_players=int(d.get("total_players", 0)),
            total_capacity=int(d.get("total_capacity", 0)),
            total_characters=int(d.get("total_characters", 0)),
        )


@dataclass
class Migration:
    id: str = ""
    source_servers: List[str] = field(default_factory=list)
    target_server: str = ""
    status: str = ""
    started_at: Optional[datetime] = None
    completed_at: Optional[datetime] = None

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Migration":
        return cls(
            id=d.get("id", ""),
            source_servers=d.get("source_servers") or [],
            target_server=d.get("target_server", ""),
            status=d.get("status", ""),
            started_at=_parse_time(d.get("started_at")),
            completed_at=_parse_time(d.get("completed_at")),
        )


@dataclass
class CreateMigrationRequest:
    source_servers: List[str]
    target_server: str

    def to_dict(self) -> Dict[str, Any]:
        return {"source_servers": self.source_servers, "target_server": self.target_server}
