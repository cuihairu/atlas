// JS SDK tests — a fake Atlas on a real socket (mirrors the Go/Python
// suites): asserting paths, auth headers, query building, error mapping,
// retry and the auto-heartbeat loop.

import http from "node:http";
import type { AddressInfo } from "node:net";
import test from "node:test";
import assert from "node:assert/strict";

import {
  AtlasClient,
  AtlasError,
  type CharacterFilter,
  type CreateCharacterRequest,
  type CreateMigrationRequest,
  type RegisterRequest,
  type ServerFilter,
  type UpdateCharacterRequest,
} from "../src/index.ts";

type Reply = [status: number, body: unknown];

interface FakeRequest {
  method: string;
  path: string;
  query: Record<string, string>;
  headers: http.IncomingHttpHeaders;
  body: unknown;
}

type Handler = (req: FakeRequest) => Reply;

class FakeAtlas {
  url = "";
  requests: FakeRequest[] = [];
  routes: [method: string, pattern: RegExp, handler: Handler][] = [];

  private readonly server = http.createServer((req, res) => {
    void this.dispatch(req, res);
  });

  static async start(): Promise<FakeAtlas> {
    const f = new FakeAtlas();
    await new Promise<void>((resolve) => f.server.listen(0, "127.0.0.1", resolve));
    const addr = f.server.address() as AddressInfo;
    f.url = `http://127.0.0.1:${addr.port}`;
    return f;
  }

  close(): void {
    this.server.close();
  }

  private async dispatch(req: http.IncomingMessage, res: http.ServerResponse): Promise<void> {
    const chunks: Buffer[] = [];
    for await (const chunk of req) chunks.push(chunk as Buffer);
    const raw = Buffer.concat(chunks).toString();
    let body: unknown;
    try {
      body = raw ? JSON.parse(raw) : undefined;
    } catch {
      body = raw;
    }
    const url = new URL(req.url ?? "/", "http://localhost");
    const query: Record<string, string> = {};
    for (const [k, v] of url.searchParams) query[k] = v;
    const record: FakeRequest = {
      method: req.method ?? "GET",
      path: url.pathname,
      query,
      headers: req.headers,
      body,
    };
    this.requests.push(record);
    for (const [method, pattern, handler] of this.routes) {
      if (method !== record.method || !pattern.test(record.path)) continue;
      const [status, payload] = handler(record);
      const data = JSON.stringify(payload);
      res.writeHead(status, { "Content-Type": "application/json" });
      res.end(data);
      return;
    }
    res.writeHead(404);
    res.end();
  }
}

function clientFor(s: FakeAtlas, extra: Partial<ConstructorParameters<typeof AtlasClient>[0]> = {}) {
  return new AtlasClient({
    baseUrl: s.url,
    registryToken: "reg-token",
    adminApiKey: "adm-key",
    baseBackoffMs: 1,
    ...extra,
  });
}

function bodyOf(s: FakeAtlas, method: string, path: RegExp): FakeRequest {
  const hit = s.requests.find((r) => r.method === method && path.test(r.path));
  assert.ok(hit, `no ${method} ${path} recorded`);
  return hit;
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

test("registry lifecycle", async () => {
  const s = await FakeAtlas.start();
  s.routes.push(
    ["POST", /\/v1\/registry\/servers\/register$/, () => [200, { server_id: "game-1", status: "online" }]],
  );
  const beats: unknown[] = [];
  s.routes.push([
    "POST",
    /\/v1\/registry\/servers\/game-1\/heartbeat$/,
    (req) => {
      beats.push(req.body);
      return [200, { server_id: "game-1", status: "online", next_heartbeat_in: 10 }];
    },
  ]);
  s.routes.push([
    "POST",
    /\/v1\/registry\/servers\/game-1\/unregister$/,
    () => [200, { server_id: "game-1", status: "offline" }],
  ]);
  const c = clientFor(s);
  try {
    const reg = await c.register({
      serverId: "game-1",
      name: "Test",
      region: "cn-east",
      capacity: 100,
      endpoint: { host: "10.0.0.1", port: 30001 },
    } satisfies RegisterRequest);
    assert.equal(reg.serverId, "game-1");
    assert.equal(reg.status, "online");

    const sent = bodyOf(s, "POST", /\/register$/);
    assert.equal(sent.headers.authorization, "Bearer reg-token");
    assert.deepEqual(sent.body, {
      server_id: "game-1",
      name: "Test",
      type: "game",
      region: "cn-east",
      version: "",
      platform: "",
      endpoint: { host: "10.0.0.1", port: 30001 },
      capacity: 100,
    });

    const hb = await c.heartbeat("game-1", { players: 7, load: 0.3 });
    assert.equal(hb.nextHeartbeatIn, 10);
    assert.deepEqual(beats, [{ players: 7, load: 0.3 }]);

    const off = await c.unregister("game-1");
    assert.equal(off.status, "offline");
  } finally {
    c.close();
    s.close();
  }
});

test("registry split port", async () => {
  const main = await FakeAtlas.start();
  const registry = await FakeAtlas.start();
  main.routes.push([
    "GET",
    /\/v1\/discovery\/servers$/,
    () => [200, { servers: [{ id: "game-1", status: "online" }] }],
  ]);
  registry.routes.push([
    "POST",
    /\/v1\/registry\/servers\/register$/,
    () => [200, { server_id: "game-1", status: "online" }],
  ]);
  const c = clientFor(main, { registryBaseUrl: registry.url });
  try {
    await c.register({
      serverId: "game-1",
      name: "T",
      region: "r",
      capacity: 1,
      endpoint: { host: "h", port: 1 },
    });
    bodyOf(registry, "POST", /\/register$/);
    assert.equal((await c.listServers()).length, 1);
    bodyOf(main, "GET", /\/discovery\/servers$/);
    // split port ⇒ no registry traffic leaked to the public port
    assert.equal(main.requests.filter((r) => r.path.startsWith("/v1/registry")).length, 0);
  } finally {
    c.close();
    main.close();
    registry.close();
  }
});

test("discovery and error mapping", async () => {
  const s = await FakeAtlas.start();
  const seen: Record<string, string> = {};
  s.routes.push([
    "GET",
    /\/v1\/discovery\/servers$/,
    (req) => {
      Object.assign(seen, req.query);
      return [200, { servers: [{ id: "game-1", status: "online", players: 12 }] }];
    },
  ]);
  s.routes.push([
    "GET",
    /\/v1\/discovery\/servers\/nope$/,
    () => [404, { error: { code: "SERVER_NOT_FOUND", message: "server nope does not exist" } }],
  ]);
  const c = clientFor(s);
  try {
    const got = await c.listServers({
      region: "cn-east",
      status: "online",
      limit: 20,
    } satisfies ServerFilter);
    assert.equal(got.length, 1);
    assert.equal(got[0].id, "game-1");
    assert.equal(got[0].players, 12);
    assert.deepEqual(seen, { region: "cn-east", status: "online", limit: "20" });

    await assert.rejects(c.getServer("nope"), (err: unknown) => {
      assert.ok(err instanceof AtlasError);
      assert.equal((err as AtlasError).code, "SERVER_NOT_FOUND");
      assert.equal((err as AtlasError).status, 404);
      return true;
    });
  } finally {
    c.close();
    s.close();
  }
});

test("directory write shapes", async () => {
  const s = await FakeAtlas.start();
  const replies: Reply[] = [
    [201, { account_id: 7, server_id: "game-1", character_id: 1001, name: "Hero" }],
    [200, { character: { character_id: 1001, name: "Hero" }, status: "updated" }],
    [202, { status: "queued" }],
    [200, { status: "deleted" }],
  ];
  const captured: unknown[] = [];
  const handler = (): Reply => replies.shift() ?? [500, {}];
  s.routes.push([
    "POST",
    /\/v1\/directory\/characters$/,
    (req) => {
      captured.push(req.body);
      return handler();
    },
  ]);
  s.routes.push([
    "PATCH",
    /\/v1\/directory\/characters\/1001$/,
    (req) => {
      captured.push(req.body);
      return handler();
    },
  ]);
  s.routes.push(["DELETE", /\/v1\/directory\/characters\/1001$/, () => handler()]);
  const c = clientFor(s);
  try {
    let wr = await c.createCharacter({
      accountId: 7,
      serverId: "game-1",
      characterId: 1001,
      name: "Hero",
    } satisfies CreateCharacterRequest);
    assert.equal(wr.status, "created");
    assert.equal(wr.character?.characterId, 1001);
    assert.deepEqual(captured[0], {
      account_id: 7,
      server_id: "game-1",
      character_id: 1001,
      name: "Hero",
    });

    wr = await c.updateCharacter(1001, { level: 10 } satisfies UpdateCharacterRequest);
    assert.equal(wr.status, "updated");
    assert.equal(wr.character?.name, "Hero");
    assert.deepEqual(captured[1], { level: 10 }); // PATCH omits unset fields

    wr = await c.updateCharacter(1001, {}); // no-op patch
    assert.deepEqual(captured[2], {});
    assert.equal(wr.status, "queued"); // status-only reply → no character
    assert.equal(wr.character, undefined);

    wr = await c.deleteCharacter(1001);
    assert.equal(wr.status, "deleted");
    assert.equal(wr.character, undefined);
  } finally {
    c.close();
    s.close();
  }
});

test("directory listing", async () => {
  const s = await FakeAtlas.start();
  const chars = Array.from({ length: 3 }, (_, n) => ({
    account_id: 7,
    server_id: "game-1",
    character_id: n,
    name: `c${n}`,
  }));
  s.routes.push(["GET", /\/v1\/directory\/accounts\/7\/characters$/, () => [200, { characters: chars }]]);
  const seen: Record<string, string> = {};
  s.routes.push([
    "GET",
    /\/v1\/directory\/servers\/game-1\/characters$/,
    (req) => {
      Object.assign(seen, req.query);
      return [200, { characters: chars.slice(0, 2), next_cursor: "cursor-2" }];
    },
  ]);
  const c = clientFor(s);
  try {
    const byAccount = await c.listCharactersByAccount(7);
    assert.deepEqual(
      byAccount.map((ch) => ch.characterId),
      [0, 1, 2],
    );
    const page = await c.listCharactersByServer("game-1", 2, "cursor-1");
    assert.equal(page.characters.length, 2);
    assert.equal(page.nextCursor, "cursor-2");
    assert.deepEqual(seen, { limit: "2", cursor: "cursor-1" });
  } finally {
    c.close();
    s.close();
  }
});

test("recommend", async () => {
  const s = await FakeAtlas.start();
  const seen: Record<string, string> = {};
  s.routes.push([
    "GET",
    /\/v1\/routing\/recommended$/,
    (req) => {
      Object.assign(seen, req.query);
      return [200, { server: { id: "game-1", status: "online" }, reason: "lowest_load" }];
    },
  ]);
  const c = clientFor(s);
  try {
    const out = await c.recommend({
      accountId: 42,
      region: "cn-east",
      version: "1.0.0",
      platform: "pc",
    });
    assert.equal(out.server.id, "game-1");
    assert.equal(out.reason, "lowest_load");
    assert.deepEqual(seen, {
      account_id: "42",
      region: "cn-east",
      version: "1.0.0",
      platform: "pc",
    });

    for (const k of Object.keys(seen)) delete seen[k];
    await c.recommend();
    assert.deepEqual(seen, {}); // zero-value args are omitted
  } finally {
    c.close();
    s.close();
  }
});

test("admin", async () => {
  const s = await FakeAtlas.start();
  s.routes.push(
    ["POST", /\/v1\/admin\/servers\/game-1\/maintenance$/, () => [200, { server_id: "game-1", status: "maintenance" }]],
    ["POST", /\/v1\/admin\/servers\/game-1\/enable$/, () => [200, { server_id: "game-1", status: "online" }]],
    [
      "GET",
      /\/v1\/admin\/stats$/,
      () => [
        200,
        {
          total_servers: 2,
          servers_by_status: { online: 2 },
          servers_by_region: { "cn-east": 2 },
          servers_by_version: { "1.0.0": 2 },
          total_players: 50,
          total_capacity: 4000,
          total_characters: 9,
        },
      ],
    ],
    [
      "GET",
      /\/v1\/admin\/characters\/search$/,
      () => [200, { characters: [{ character_id: 1001, name: "Hero" }], next_cursor: "" }],
    ],
    [
      "POST",
      /\/v1\/admin\/migrations$/,
      () => [200, { migration: { id: "m-1", source_servers: ["a"], target_server: "b", status: "pending" } }],
    ],
    ["GET", /\/v1\/admin\/migrations\/m-1$/, () => [200, { migration: { id: "m-1", status: "completed" } }]],
    ["GET", /\/v1\/admin\/migrations$/, () => [200, { migrations: [{ id: "m-1" }] }]],
    ["POST", /\/v1\/admin\/migrations\/m-1\/rollback$/, () => [200, { migration: { id: "m-1", status: "rolled_back" } }]],
  );
  const c = clientFor(s);
  try {
    assert.equal((await c.setMaintenance("game-1")).status, "maintenance");
    assert.equal((await c.enable("game-1")).status, "online");
    const life = bodyOf(s, "POST", /\/maintenance$/);
    assert.equal(life.headers.authorization, "Bearer adm-key");

    const stats = await c.getStats();
    assert.equal(stats.totalServers, 2);
    assert.equal(stats.totalCharacters, 9);
    assert.deepEqual(stats.serversByStatus, { online: 2 });

    const page = await c.searchCharacters({
      name: "Hero",
      minLevel: 10,
      limit: 5,
    } satisfies CharacterFilter);
    assert.equal(page.characters[0]?.name, "Hero");
    const search = bodyOf(s, "GET", /\/characters\/search$/);
    assert.deepEqual(search.query, { name: "Hero", min_level: "10", limit: "5" });

    const mig = await c.createMigration({ sourceServers: ["a"], targetServer: "b" } satisfies CreateMigrationRequest);
    assert.equal(mig.id, "m-1");
    assert.equal(mig.status, "pending");
    assert.equal((await c.getMigration("m-1")).status, "completed");
    assert.equal((await c.listMigrations(10)).length, 1);
    assert.equal((await c.rollbackMigration("m-1")).status, "rolled_back");
  } finally {
    c.close();
    s.close();
  }
});

test("retry on transient", async () => {
  const s = await FakeAtlas.start();
  const attempts: Record<string, number> = { flaky: 0, missing: 0 };
  s.routes.push([
    "GET",
    /\/v1\/discovery\/servers\/.*$/,
    (req) => {
      const sid = req.path.split("/").pop() ?? "";
      attempts[sid] = (attempts[sid] ?? 0) + 1;
      if (sid === "flaky") {
        if (attempts.flaky < 3) return [502, { error: { code: "BAD_GATEWAY", message: "try again" } }];
        return [200, { id: "flaky", status: "online" }];
      }
      return [404, { error: { code: "SERVER_NOT_FOUND", message: "x" } }];
    },
  ]);
  const c = clientFor(s);
  try {
    assert.equal((await c.getServer("flaky")).id, "flaky");
    assert.equal(attempts.flaky, 3); // 2 failures + success

    await assert.rejects(c.getServer("missing"), AtlasError);
    assert.equal(attempts.missing, 1); // 4xx must not be retried
  } finally {
    c.close();
    s.close();
  }
});

test("network error", async () => {
  // Port 1 is unroutable — expect a mapped NETWORK error, not a raw throw.
  const c = new AtlasClient({ baseUrl: "http://127.0.0.1:1", maxRetries: 0, baseBackoffMs: 1 });
  try {
    await assert.rejects(c.listServers(), (err: unknown) => {
      assert.ok(err instanceof AtlasError);
      assert.equal((err as AtlasError).status, 0);
      assert.equal((err as AtlasError).code, "NETWORK");
      return true;
    });
  } finally {
    c.close();
  }
});

test("http error without json body", async () => {
  const s = await FakeAtlas.start();
  s.routes.push(["GET", /\/v1\/admin\/stats$/, () => [500, "boom"]]);
  const c = clientFor(s, { maxRetries: 0 });
  try {
    await assert.rejects(c.getStats(), (err: unknown) => {
      assert.ok(err instanceof AtlasError);
      assert.equal((err as AtlasError).code, "HTTP_500");
      return true;
    });
  } finally {
    c.close();
    s.close();
  }
});

test("auto heartbeat", async () => {
  const s = await FakeAtlas.start();
  const beats: Record<string, unknown>[] = [];
  s.routes.push([
    "POST",
    /\/v1\/registry\/servers\/game-1\/heartbeat$/,
    (req) => {
      beats.push(req.body as Record<string, unknown>);
      return [200, { server_id: "game-1", status: "online" }];
    },
  ]);
  const c = clientFor(s);
  try {
    const loop = c.startHeartbeat("game-1", { intervalMs: 50, players: 5 });
    loop.set(42, 0.5); // picked up by the first or second beat
    const deadline = Date.now() + 2000;
    while (beats.length < 2 && Date.now() < deadline) await sleep(10);
    loop.stop();
    loop.stop(); // idempotent

    assert.ok(beats.length >= 2, `expected ≥2 heartbeats, got ${beats.length}`);
    assert.deepEqual(beats[beats.length - 1], { players: 42, load: 0.5 });
  } finally {
    c.close();
    s.close();
  }
});

test("auto heartbeat error callback", async () => {
  const errors: AtlasError[] = [];
  const c = new AtlasClient({ baseUrl: "http://127.0.0.1:1", maxRetries: 0, baseBackoffMs: 1 });
  try {
    const loop = c.startHeartbeat("game-1", { intervalMs: 20, onError: (e) => errors.push(e) });
    const deadline = Date.now() + 2000;
    while (errors.length === 0 && Date.now() < deadline) await sleep(10);
    loop.stop();
    assert.ok(errors.length > 0);
    assert.equal(errors[0]?.code, "NETWORK");
  } finally {
    c.close();
  }
});
