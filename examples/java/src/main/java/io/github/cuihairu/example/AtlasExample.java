package io.github.cuihairu.example;

import io.github.cuihairu.atlas.AtlasClient;
import io.github.cuihairu.atlas.AtlasClientOptions;
import io.github.cuihairu.atlas.AutoHeartbeat;
import io.github.cuihairu.atlas.Endpoint;
import io.github.cuihairu.atlas.HeartbeatRequest;
import io.github.cuihairu.atlas.RegisterRequest;
import io.github.cuihairu.atlas.Recommendation;

import java.util.concurrent.CountDownLatch;

/** Atlas Java SDK example: register a game server, start auto-heartbeat,
 * then look up characters and leave cleanly.
 *
 * Run against a local Atlas (default ports):
 *
 *   mvn install -f ../../sdk/java/pom.xml
 *   ATLAS_ADDR=http://localhost:8080 mvn exec:java
 */
public final class AtlasExample {

    public static void main(String[] args) throws Exception {
        String addr = envOr("ATLAS_ADDR", "http://localhost:8080");
        String registryAddr = System.getenv("ATLAS_REGISTRY_ADDR");
        String registryToken = System.getenv("ATLAS_REGISTRY_TOKEN");
        String serverId = envOr("SERVER_ID", "demo-game-1");

        // RegistryBaseUrl points at the split registry port when not behind
        // a merged proxy (local dev: ATLAS_REGISTRY_ADDR=http://localhost:8081).
        AtlasClientOptions options = new AtlasClientOptions()
                .setBaseUrl(addr)
                .setRegistryBaseUrl(registryAddr)
                .setRegistryToken(registryToken == null ? "" : registryToken);
        try (AtlasClient client = new AtlasClient(options)) {
            // 1. Register this server.
            RegisterRequest req = new RegisterRequest(
                    serverId, "Demo Game Server", "cn-east",
                    new Endpoint("10.0.0.1", 30001), 2000);
            req.type = "game";
            req.version = "1.0.0";
            req.platform = "any";
            var reg = client.register(req);
            System.out.println("registered: " + reg.serverId + " (status=" + reg.status + ")");

            // 2. Heartbeat: one synchronous beat first — discovery/routing
            // only see this server once it is online — then the auto loop
            // every 10s. Update the payload as load changes.
            client.heartbeat(serverId, new HeartbeatRequest());
            AutoHeartbeat loop = client.startHeartbeat(serverId, 10_000,
                    new HeartbeatRequest(),
                    err -> System.err.println("heartbeat failed: " + err.getMessage()));

            CountDownLatch done = new CountDownLatch(1);
            Runtime.getRuntime().addShutdownHook(new Thread(() -> {
                System.out.println("shutting down…");
                loop.stop();
                try {
                    client.unregister(serverId);
                } catch (Exception e) {
                    System.err.println("unregister: " + e.getMessage());
                }
                done.countDown();
            }));

            try {
                // 3. Discovery: where should account 42 play?
                Recommendation rec = client.recommend(42, "cn-east", null, null);
                System.out.println("account 42 → " + rec.server.id + " (" + rec.reason + ")");

                // 4. Directory: characters on that server.
                var page = client.listCharactersByServer(rec.server.id, 10, "");
                page.characters.forEach(ch ->
                        System.out.printf("  character %d %s lv%d%n",
                                ch.characterId, ch.name, ch.level));
            } catch (Exception e) {
                System.err.println("lookup failed: " + e.getMessage());
            }

            // 5. Stay up until signalled (shutdown hook above runs on SIGINT/SIGTERM).
            done.await();
        }
    }

    private static String envOr(String key, String fallback) {
        String v = System.getenv(key);
        return v != null && !v.isEmpty() ? v : fallback;
    }

    private AtlasExample() {}
}
