// Atlas REST client (fetch) — same API surface as the Go/C++/Python
// SDKs: five API groups (Registry / Discovery / Directory / Routing /
// Admin), transient-failure retry with full-jitter backoff, and an
// auto-heartbeat loop. Runs in Node.js 18+ and modern browsers.

import { AtlasError, parseError } from "./errors.ts";
import type {
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
} from "./types.ts";
import {
  characterFilterParams,
  createCharacterBody,
  heartbeatBody,
  migrationBody,
  parseCharacter,
  parseCharacterWrite,
  parseMigration,
  parseServer,
  parseStats,
  registerBody,
  serverFilterParams,
  updateCharacterBody,
} from "./types.ts";

export interface AtlasClientOptions {
  /** Public + Admin base, default "http://localhost:8080". */
  baseUrl?: string;
  /** Registry split port override (ATLAS_REGISTRY_ADDR, default :8081). */
  registryBaseUrl?: string;
  /** Bearer token for the Registry scope. */
  registryToken?: string;
  /** API key for the Admin scope. */
  adminApiKey?: string;
  /** Per-attempt timeout in ms (default 10000; 0 disables). */
  timeoutMs?: number;
  /** Retries for transient failures — network errors and 5xx (default 3). */
  maxRetries?: number;
  /** Delay ceiling before the first retry; doubles each attempt with
   * full jitter, capped at 10s (default 100ms). */
  baseBackoffMs?: number;
}

interface RequestOptions {
  json?: unknown;
  bearer?: string;
  registry?: boolean;
  params?: Record<string, string>;
}

function isTransientStatus(status: number): boolean {
  return status >= 500;
}

function backoffMs(base: number, attempt: number): number {
  const ceiling = Math.min(base * 2 ** attempt, 10_000);
  return Math.random() * ceiling;
}

function isNetworkError(exc: unknown): boolean {
  // fetch rejects with TypeError on network failure and with a
  // DOMException ("TimeoutError" / "AbortError") on timeout. Anything
  // else (e.g. a JSON SyntaxError on a malformed success body) is a
  // protocol problem, not transient — let it propagate.
  if (exc instanceof TypeError) return true;
  return (
    exc instanceof DOMException &&
    (exc.name === "TimeoutError" || exc.name === "AbortError")
  );
}

export class AtlasClient {
  private readonly baseUrl: string;
  private readonly registryBaseUrl: string;
  private readonly registryToken: string;
  private readonly adminApiKey: string;
  private readonly timeoutMs: number;
  private readonly maxRetries: number;
  private readonly baseBackoffMs: number;

  constructor(options: AtlasClientOptions = {}) {
    this.baseUrl = (options.baseUrl ?? "http://localhost:8080").replace(/\/+$/, "");
    this.registryBaseUrl = (options.registryBaseUrl ?? this.baseUrl).replace(/\/+$/, "");
    this.registryToken = options.registryToken ?? "";
    this.adminApiKey = options.adminApiKey ?? "";
    this.timeoutMs = options.timeoutMs ?? 10_000;
    this.maxRetries = options.maxRetries ?? 3;
    this.baseBackoffMs = options.baseBackoffMs ?? 100;
  }

  /** Lifecycle hook for parity with the Go/Python SDKs — fetch manages
   * its own connection pool, so there is nothing to release here. Does
   * NOT stop heartbeat loops; call ``AutoHeartbeat.stop()`` yourself. */
  close(): void {}

  // ── core request path ──

  private async request(method: string, path: string, opts: RequestOptions = {}): Promise<unknown> {
    const root = opts.registry ? this.registryBaseUrl : this.baseUrl;
    const url = new URL(path, root);
    for (const [key, value] of Object.entries(opts.params ?? {})) {
      url.searchParams.set(key, value);
    }
    const headers: Record<string, string> = { Accept: "application/json" };
    if (opts.json !== undefined) headers["Content-Type"] = "application/json";
    if (opts.bearer) headers.Authorization = `Bearer ${opts.bearer}`;

    let attempt = 0;
    for (;;) {
      let exc: unknown;
      try {
        const signal =
          this.timeoutMs > 0 ? AbortSignal.timeout(this.timeoutMs) : undefined;
        const resp = await fetch(url, {
          method,
          headers,
          body: opts.json !== undefined ? JSON.stringify(opts.json) : undefined,
          signal,
        });
        if (resp.status < 400) {
          if (resp.status === 204) return undefined;
          const text = await resp.text();
          if (!text) return undefined;
          return JSON.parse(text) as unknown;
        }
        const body: unknown = await resp.json().catch(() => resp.statusText);
        if (!isTransientStatus(resp.status) || attempt >= Math.max(this.maxRetries, 0)) {
          throw parseError(resp.status, body);
        }
      } catch (e) {
        if (e instanceof AtlasError) throw e;
        if (!isNetworkError(e)) throw e;
        exc = e;
        if (attempt >= Math.max(this.maxRetries, 0)) {
          throw new AtlasError(0, "NETWORK", `${method} ${path}: ${String(exc)}`);
        }
      }
      await sleep(backoffMs(this.baseBackoffMs, attempt));
      attempt += 1;
    }
  }

  // ── Registry (service token, split port) ──

  async register(req: RegisterRequest): Promise<RegisterResult> {
    const body = (await this.request("POST", "/v1/registry/servers/register", {
      json: registerBody(req),
      bearer: this.registryToken,
      registry: true,
    })) as Record<string, any>;
    return { serverId: body?.server_id ?? "", status: body?.status ?? "" };
  }

  async heartbeat(serverId: string, req: HeartbeatRequest): Promise<HeartbeatResult> {
    const body = (await this.request(
      "POST",
      `/v1/registry/servers/${encodeURIComponent(serverId)}/heartbeat`,
      {
        json: heartbeatBody(req),
        bearer: this.registryToken,
        registry: true,
      },
    )) as Record<string, any>;
    return {
      serverId: body?.server_id ?? serverId,
      status: body?.status ?? "",
      nextHeartbeatIn: body?.next_heartbeat_in ?? 0,
    };
  }

  async unregister(serverId: string): Promise<StatusResult> {
    const body = (await this.request(
      "POST",
      `/v1/registry/servers/${encodeURIComponent(serverId)}/unregister`,
      { bearer: this.registryToken, registry: true },
    )) as Record<string, any>;
    return { serverId: body?.server_id, status: body?.status ?? "" };
  }

  // ── Discovery (public) ──

  async listServers(filter: ServerFilter = {}): Promise<Server[]> {
    const body = (await this.request("GET", "/v1/discovery/servers", {
      params: serverFilterParams(filter),
    })) as Record<string, any>;
    return (body?.servers ?? []).map(parseServer);
  }

  async getServer(serverId: string): Promise<Server> {
    const body = await this.request(
      "GET",
      `/v1/discovery/servers/${encodeURIComponent(serverId)}`,
    );
    return parseServer(body as Record<string, any>);
  }

  // ── Directory (public) ──

  async createCharacter(req: CreateCharacterRequest): Promise<CharacterWriteResult> {
    const body = await this.request("POST", "/v1/directory/characters", {
      json: createCharacterBody(req),
    });
    return parseCharacterWrite(body, "created");
  }

  async getCharacter(characterId: number): Promise<Character> {
    const body = await this.request("GET", `/v1/directory/characters/${characterId}`);
    return parseCharacter(body as Record<string, any>);
  }

  async listCharactersByAccount(accountId: number): Promise<Character[]> {
    const body = (await this.request(
      "GET",
      `/v1/directory/accounts/${accountId}/characters`,
    )) as Record<string, any>;
    return (body?.characters ?? []).map(parseCharacter);
  }

  async listCharactersByServer(
    serverId: string,
    limit = 0,
    cursor = "",
  ): Promise<CharacterPage> {
    const params: Record<string, string> = {};
    if (limit > 0) params.limit = String(limit);
    if (cursor) params.cursor = cursor;
    const body = (await this.request(
      "GET",
      `/v1/directory/servers/${encodeURIComponent(serverId)}/characters`,
      { params },
    )) as Record<string, any>;
    return {
      characters: (body?.characters ?? []).map(parseCharacter),
      nextCursor: body?.next_cursor ?? "",
    };
  }

  async updateCharacter(
    characterId: number,
    req: UpdateCharacterRequest,
  ): Promise<CharacterWriteResult> {
    const body = await this.request("PATCH", `/v1/directory/characters/${characterId}`, {
      json: updateCharacterBody(req),
    });
    return parseCharacterWrite(body, "updated");
  }

  async deleteCharacter(characterId: number): Promise<CharacterWriteResult> {
    const body = (await this.request(
      "DELETE",
      `/v1/directory/characters/${characterId}`,
    )) as Record<string, any>;
    return { status: body?.status ?? "deleted" };
  }

  // ── Routing (public) ──

  async recommend(opts: {
    accountId?: number;
    region?: string;
    version?: string;
    platform?: string;
  } = {}): Promise<Recommendation> {
    const params: Record<string, string> = {};
    if (opts.accountId && opts.accountId > 0) params.account_id = String(opts.accountId);
    for (const key of ["region", "version", "platform"] as const) {
      const v = opts[key];
      if (v) params[key] = v;
    }
    const body = (await this.request("GET", "/v1/routing/recommended", {
      params,
    })) as Record<string, any>;
    return { server: parseServer(body?.server), reason: body?.reason ?? "" };
  }

  // ── Admin (API key) ──

  private async lifecycle(action: string, serverId: string): Promise<StatusResult> {
    const body = (await this.request(
      "POST",
      `/v1/admin/servers/${encodeURIComponent(serverId)}/${action}`,
      { bearer: this.adminApiKey },
    )) as Record<string, any>;
    return { serverId: body?.server_id, status: body?.status ?? "" };
  }

  async setMaintenance(serverId: string): Promise<StatusResult> {
    return this.lifecycle("maintenance", serverId);
  }

  async setDrain(serverId: string): Promise<StatusResult> {
    return this.lifecycle("drain", serverId);
  }

  async enable(serverId: string): Promise<StatusResult> {
    return this.lifecycle("enable", serverId);
  }

  async disable(serverId: string): Promise<StatusResult> {
    return this.lifecycle("disable", serverId);
  }

  async getStats(): Promise<Stats> {
    const body = await this.request("GET", "/v1/admin/stats", {
      bearer: this.adminApiKey,
    });
    return parseStats(body as Record<string, any>);
  }

  async searchCharacters(filter: CharacterFilter): Promise<CharacterPage> {
    const body = (await this.request("GET", "/v1/admin/characters/search", {
      params: characterFilterParams(filter),
      bearer: this.adminApiKey,
    })) as Record<string, any>;
    return {
      characters: (body?.characters ?? []).map(parseCharacter),
      nextCursor: body?.next_cursor ?? "",
    };
  }

  async createMigration(req: CreateMigrationRequest): Promise<Migration> {
    const body = (await this.request("POST", "/v1/admin/migrations", {
      json: migrationBody(req),
      bearer: this.adminApiKey,
    })) as Record<string, any>;
    return parseMigration(body?.migration);
  }

  async getMigration(migrationId: string): Promise<Migration> {
    const body = (await this.request(
      "GET",
      `/v1/admin/migrations/${encodeURIComponent(migrationId)}`,
      { bearer: this.adminApiKey },
    )) as Record<string, any>;
    return parseMigration(body?.migration);
  }

  async listMigrations(limit = 0): Promise<Migration[]> {
    const params: Record<string, string> = {};
    if (limit > 0) params.limit = String(limit);
    const body = (await this.request("GET", "/v1/admin/migrations", {
      params,
      bearer: this.adminApiKey,
    })) as Record<string, any>;
    return (body?.migrations ?? []).map(parseMigration);
  }

  async rollbackMigration(migrationId: string): Promise<Migration> {
    const body = (await this.request(
      "POST",
      `/v1/admin/migrations/${encodeURIComponent(migrationId)}/rollback`,
      { bearer: this.adminApiKey },
    )) as Record<string, any>;
    return parseMigration(body?.migration);
  }

  // ── Auto heartbeat ──

  /** Start reporting immediately, then every ``intervalMs`` milliseconds
   * (default 10000). */
  startHeartbeat(
    serverId: string,
    opts: HeartbeatOptions = {},
  ): AutoHeartbeat {
    return new AutoHeartbeat(this, serverId, opts);
  }
}

export interface HeartbeatOptions {
  intervalMs?: number;
  players?: number;
  load?: number;
  onError?: (err: AtlasError) => void;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => {
    setTimeout(resolve, ms);
  });
}

/** Background heartbeat loop (immediate first report, then interval).
 * Update the payload from the game thread with ``set``; failures are
 * surfaced through ``onError`` and the loop keeps running. */
export class AutoHeartbeat {
  /** Called with every failed beat; the loop keeps running. */
  onError?: (err: AtlasError) => void;

  private readonly client: AtlasClient;
  private readonly serverId: string;
  private payload: HeartbeatRequest;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private stopped = false;

  constructor(client: AtlasClient, serverId: string, opts: HeartbeatOptions = {}) {
    this.client = client;
    this.serverId = serverId;
    const intervalMs = opts.intervalMs && opts.intervalMs > 0 ? opts.intervalMs : 10_000;
    this.payload = { players: opts.players ?? 0, load: opts.load ?? 0 };
    this.onError = opts.onError;
    void this.loop(intervalMs); // immediate first report
  }

  set(players: number, load: number): void {
    this.payload = { players, load };
  }

  stop(): void {
    this.stopped = true;
    if (this.timer !== undefined) {
      clearTimeout(this.timer);
      this.timer = undefined;
    }
  }

  private async loop(intervalMs: number): Promise<void> {
    await this.beat();
    while (!this.stopped) {
      await new Promise<void>((resolve) => {
        this.timer = setTimeout(resolve, intervalMs);
      });
      if (this.stopped) return;
      await this.beat();
    }
  }

  private async beat(): Promise<void> {
    try {
      await this.client.heartbeat(this.serverId, this.payload);
    } catch (e) {
      if (e instanceof AtlasError) this.onError?.(e);
    }
  }
}
