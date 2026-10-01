"""Atlas Python SDK (TODO v0.1.8).

Sync and async REST clients for Atlas — server registry, discovery,
character directory, routing and admin. Docs: docs/sdk-python.md.
"""

from .client import AtlasClient, AtlasError, AutoHeartbeat
from ._async import AsyncHeartbeatTask, AtlasAsyncClient
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

__version__ = "0.1.8"

__all__ = [
    "AtlasClient",
    "AtlasAsyncClient",
    "AtlasError",
    "AutoHeartbeat",
    "AsyncHeartbeatTask",
    "Character",
    "CharacterFilter",
    "CharacterPage",
    "CharacterWriteResult",
    "CreateCharacterRequest",
    "CreateMigrationRequest",
    "Endpoint",
    "HeartbeatRequest",
    "HeartbeatResult",
    "Migration",
    "Recommendation",
    "RegisterRequest",
    "RegisterResult",
    "Server",
    "ServerFilter",
    "Stats",
    "StatusResult",
    "UpdateCharacterRequest",
]
