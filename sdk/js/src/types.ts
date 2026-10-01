// Atlas SDK data types — mirrors the Go/C++/Python SDKs and docs/api.md
// JSON. The wire format is snake_case; the SDK surface is camelCase.

export interface Endpoint {
  host: string;
  port: number;
}

export interface Server {
  id: string;
  name: string;
  type: string;
  region: string;
  realmId?: string;
  shardId?: string;
  version: string;
  platform: string;
  endpoint: Endpoint;
  capacity: number;
  metadata: Record<string, string>;
  status: string;
  players: number;
  load: number;
  lastSeenAt?: Date;
  createdAt?: Date;
  updatedAt?: Date;
}

export interface ServerFilter {
  region?: string;
  version?: string;
  platform?: string;
  status?: string;
  limit?: number;
}

export interface RegisterRequest {
  serverId: string;
  name: string;
  region: string;
  endpoint: Endpoint;
  capacity: number;
  /** defaults to "game" */
  type?: string;
  version?: string;
  platform?: string;
  realmId?: string;
  shardId?: string;
  metadata?: Record<string, string>;
}

export interface RegisterResult {
  serverId: string;
  status: string;
}

export interface HeartbeatRequest {
  players?: number;
  load?: number;
}

export interface HeartbeatResult {
  serverId: string;
  status: string;
  nextHeartbeatIn: number;
}

export interface StatusResult {
  serverId?: string;
  status: string;
}

export interface Character {
  accountId: number;
  serverId: string;
  characterId: number;
  name: string;
  level: number;
  classId: number;
  avatar: string;
  metadata: Record<string, string>;
  lastLoginAt?: Date;
  createdAt?: Date;
  updatedAt?: Date;
}

export interface CreateCharacterRequest {
  accountId: number;
  serverId: string;
  characterId: number;
  name: string;
  level?: number;
  classId?: number;
  avatar?: string;
}

/** PATCH body — unset fields are left unchanged. */
export interface UpdateCharacterRequest {
  name?: string;
  level?: number;
  classId?: number;
  avatar?: string;
}

/** Directory write reply; ``character`` is undefined when status === "queued". */
export interface CharacterWriteResult {
  character?: Character;
  status: string; // created | updated | deleted | queued
}

export interface CharacterPage {
  characters: Character[];
  nextCursor: string;
}

export interface CharacterFilter {
  name?: string;
  serverId?: string;
  classId?: number;
  minLevel?: number;
  maxLevel?: number;
  limit?: number;
  cursor?: string;
}

export interface Recommendation {
  server: Server;
  reason: string; // lowest_load | highest_capacity | has_character | fallback
}

export interface Stats {
  totalServers: number;
  serversByStatus: Record<string, number>;
  serversByRegion: Record<string, number>;
  serversByVersion: Record<string, number>;
  totalPlayers: number;
  totalCapacity: number;
  totalCharacters: number;
}

export interface Migration {
  id: string;
  sourceServers: string[];
  targetServer: string;
  status: string;
  startedAt?: Date;
  completedAt?: Date;
}

export interface CreateMigrationRequest {
  sourceServers: string[];
  targetServer: string;
}

// ── wire ⇄ SDK conversion ──

type Raw = Record<string, any>;

function parseDate(value: unknown): Date | undefined {
  if (!value) return undefined;
  const d = new Date(String(value));
  return Number.isNaN(d.getTime()) ? undefined : d;
}

export function parseEndpoint(d?: Raw): Endpoint {
  return { host: d?.host ?? "", port: d?.port ?? 0 };
}

export function parseServer(d?: Raw): Server {
  return {
    id: d?.id ?? "",
    name: d?.name ?? "",
    type: d?.type ?? "",
    region: d?.region ?? "",
    realmId: d?.realm_id ?? undefined,
    shardId: d?.shard_id ?? undefined,
    version: d?.version ?? "",
    platform: d?.platform ?? "",
    endpoint: parseEndpoint(d?.endpoint),
    capacity: d?.capacity ?? 0,
    metadata: d?.metadata ?? {},
    status: d?.status ?? "",
    players: d?.players ?? 0,
    load: d?.load ?? 0,
    lastSeenAt: parseDate(d?.last_seen_at),
    createdAt: parseDate(d?.created_at),
    updatedAt: parseDate(d?.updated_at),
  };
}

export function parseCharacter(d?: Raw): Character {
  return {
    accountId: d?.account_id ?? 0,
    serverId: d?.server_id ?? "",
    characterId: d?.character_id ?? 0,
    name: d?.name ?? "",
    level: d?.level ?? 0,
    classId: d?.class_id ?? 0,
    avatar: d?.avatar ?? "",
    metadata: d?.metadata ?? {},
    lastLoginAt: parseDate(d?.last_login_at),
    createdAt: parseDate(d?.created_at),
    updatedAt: parseDate(d?.updated_at),
  };
}

export function parseStats(d?: Raw): Stats {
  return {
    totalServers: d?.total_servers ?? 0,
    serversByStatus: d?.servers_by_status ?? {},
    serversByRegion: d?.servers_by_region ?? {},
    serversByVersion: d?.servers_by_version ?? {},
    totalPlayers: d?.total_players ?? 0,
    totalCapacity: d?.total_capacity ?? 0,
    totalCharacters: d?.total_characters ?? 0,
  };
}

export function parseMigration(d?: Raw): Migration {
  return {
    id: d?.id ?? "",
    sourceServers: d?.source_servers ?? [],
    targetServer: d?.target_server ?? "",
    status: d?.status ?? "",
    startedAt: parseDate(d?.started_at),
    completedAt: parseDate(d?.completed_at),
  };
}

/** Normalize the three reply shapes: nested envelope, flat synchronous
 * character object, and status-only queued/deleted. */
export function parseCharacterWrite(body: unknown, syncStatus: string): CharacterWriteResult {
  const d = body as Raw;
  if (!d || typeof d !== "object") {
    return { status: syncStatus };
  }
  const nested = d.character;
  if (nested && typeof nested === "object") {
    return { character: parseCharacter(nested), status: d.status || syncStatus };
  }
  if ("character_id" in d) {
    return { character: parseCharacter(d), status: syncStatus };
  }
  return { status: d.status ?? "" };
}

export function registerBody(req: RegisterRequest): Raw {
  const d: Raw = {
    server_id: req.serverId,
    name: req.name,
    type: req.type ?? "game",
    region: req.region,
    version: req.version ?? "",
    platform: req.platform ?? "",
    endpoint: { host: req.endpoint.host, port: req.endpoint.port },
    capacity: req.capacity,
  };
  if (req.realmId) d.realm_id = req.realmId;
  if (req.shardId) d.shard_id = req.shardId;
  if (req.metadata) d.metadata = req.metadata;
  return d;
}

export function heartbeatBody(req: HeartbeatRequest): Raw {
  return { players: req.players ?? 0, load: req.load ?? 0 };
}

export function createCharacterBody(req: CreateCharacterRequest): Raw {
  const d: Raw = {
    account_id: req.accountId,
    server_id: req.serverId,
    character_id: req.characterId,
    name: req.name,
  };
  if (req.level !== undefined) d.level = req.level;
  if (req.classId !== undefined) d.class_id = req.classId;
  if (req.avatar !== undefined) d.avatar = req.avatar;
  return d;
}

export function updateCharacterBody(req: UpdateCharacterRequest): Raw {
  const d: Raw = {};
  if (req.name !== undefined) d.name = req.name;
  if (req.level !== undefined) d.level = req.level;
  if (req.classId !== undefined) d.class_id = req.classId;
  if (req.avatar !== undefined) d.avatar = req.avatar;
  return d;
}

export function migrationBody(req: CreateMigrationRequest): Raw {
  return { source_servers: req.sourceServers, target_server: req.targetServer };
}

export function serverFilterParams(f: ServerFilter): Record<string, string> {
  const out: Record<string, string> = {};
  for (const key of ["region", "version", "platform", "status"] as const) {
    const v = f[key];
    if (v) out[key] = v;
  }
  if (f.limit && f.limit > 0) out.limit = String(f.limit);
  return out;
}

export function characterFilterParams(f: CharacterFilter): Record<string, string> {
  const out: Record<string, string> = {};
  for (const key of ["name", "cursor"] as const) {
    const v = f[key];
    if (v) out[key] = v;
  }
  if (f.serverId) out.server_id = f.serverId;
  if (f.classId !== undefined && f.classId > 0) out.class_id = String(f.classId);
  if (f.minLevel !== undefined && f.minLevel > 0) out.min_level = String(f.minLevel);
  if (f.maxLevel !== undefined && f.maxLevel > 0) out.max_level = String(f.maxLevel);
  if (f.limit !== undefined && f.limit > 0) out.limit = String(f.limit);
  return out;
}
