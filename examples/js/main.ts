// Atlas JS SDK example: register a game server, start auto-heartbeat,
// then look up characters and leave cleanly.
//
// Run against a local Atlas (default ports; Node 22.18+ runs TS directly):
//
//    cd examples/js && npm install && npm start
//
// Environment: ATLAS_ADDR, ATLAS_REGISTRY_ADDR, ATLAS_REGISTRY_TOKEN, SERVER_ID.

import { AtlasClient, type RegisterRequest } from "@cuihairu/atlas-client";

function envOr(key: string, fallback: string): string {
  return process.env[key] || fallback;
}

async function main(): Promise<void> {
  // RegistryBaseUrl points at the split registry port when not behind a
  // merged proxy (local dev: ATLAS_REGISTRY_ADDR=http://localhost:8081).
  const client = new AtlasClient({
    baseUrl: envOr("ATLAS_ADDR", "http://localhost:8080"),
    registryBaseUrl: process.env.ATLAS_REGISTRY_ADDR,
    registryToken: process.env.ATLAS_REGISTRY_TOKEN,
  });

  // 1. Register this server.
  const serverId = envOr("SERVER_ID", "demo-game-1");
  const reg = await client.register({
    serverId,
    name: "Demo Game Server",
    type: "game",
    region: "cn-east",
    version: "1.0.0",
    platform: "any",
    endpoint: { host: "10.0.0.1", port: 30001 },
    capacity: 2000,
  } satisfies RegisterRequest);
  console.log(`registered: ${reg.serverId} (status=${reg.status})`);

  // 2. Heartbeat: one synchronous beat first — discovery/routing only
  // see this server once it is online — then the auto loop every 10s.
  // Update the payload as load changes; Atlas suspects at 3x the interval.
  await client.heartbeat(serverId, {});
  const loop = client.startHeartbeat(serverId, {
    intervalMs: 10_000,
    onError: (err) => console.error("heartbeat failed:", err.message),
  });

  const shutdown = (): void => {
    console.log("shutting down…");
    loop.stop();
    client
      .unregister(serverId)
      .catch((err) => console.error("unregister:", err.message))
      .finally(() => client.close());
  };
  process.once("SIGINT", shutdown);
  process.once("SIGTERM", shutdown);

  // 3. Discovery: where should account 42 play?
  try {
    const rec = await client.recommend({ accountId: 42, region: "cn-east" });
    console.log(`account 42 → ${rec.server.id} (${rec.reason})`);

    // 4. Directory: characters on that server.
    const page = await client.listCharactersByServer(rec.server.id, 10);
    for (const ch of page.characters) {
      console.log(`  character ${ch.characterId} ${ch.name} lv${ch.level}`);
    }
  } catch (err) {
    console.error("lookup failed:", (err as Error).message);
  }

  // 5. Stay up until signalled (shutdown above runs on SIGINT/SIGTERM).
}

void main();
