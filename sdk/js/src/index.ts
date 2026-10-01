// Atlas JavaScript/TypeScript SDK (TODO v0.1.9).
//
// REST client for Atlas — server registry, discovery, character
// directory, routing and admin. Node.js 18+ and browsers. Docs:
// docs/sdk-js.md.

export { AtlasClient } from "./client.ts";
export type { AtlasClientOptions, HeartbeatOptions } from "./client.ts";
export { AutoHeartbeat } from "./client.ts";
export { AtlasError } from "./errors.ts";
export type {
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
} from "./types.ts";
