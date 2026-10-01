package io.github.cuihairu.atlas;

import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.util.Arrays;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.regex.Pattern;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

/** Client tests against a fake Atlas on a real socket (mirrors the
 * Go/C++/Python/JS suites): paths, auth headers, query building, error
 * mapping, retry and the auto-heartbeat loop. */
class AtlasClientTest {

    private FakeAtlas s;
    private AtlasClient c;

    @AfterEach
    void tearDown() {
        if (c != null) {
            c.close();
        }
        if (s != null) {
            s.close();
        }
    }

    private void start() throws IOException {
        s = new FakeAtlas();
        c = new AtlasClient(new AtlasClientOptions()
                .setBaseUrl(s.url())
                .setRegistryToken("reg-token")
                .setAdminApiKey("adm-key")
                .setBaseBackoffMs(1));
    }

    // ── Registry lifecycle ──

    @Test
    void registryLifecycle() throws IOException {
        start();
        s.routes.add(FakeAtlas.route("POST", "/v1/registry/servers/register",
                r -> FakeAtlas.reply(200, "{\"server_id\":\"game-1\",\"status\":\"online\"}")));
        List<Map<String, Object>> beats = new CopyOnWriteArrayList<>();
        s.routes.add(FakeAtlas.route("POST", "/v1/registry/servers/game-1/heartbeat", r -> {
            beats.add(Map.of("players", r.body()));
            return FakeAtlas.reply(200,
                    "{\"server_id\":\"game-1\",\"status\":\"online\",\"next_heartbeat_in\":10}");
        }));
        s.routes.add(FakeAtlas.route("POST", "/v1/registry/servers/game-1/unregister",
                r -> FakeAtlas.reply(200, "{\"server_id\":\"game-1\",\"status\":\"offline\"}")));

        RegisterResult reg = c.register(new RegisterRequest(
                "game-1", "Test", "cn-east", new Endpoint("10.0.0.1", 30001), 100));
        assertEquals("game-1", reg.serverId);
        assertEquals("online", reg.status);
        FakeAtlas.Recorded sent = s.request("POST", Pattern.compile(".*/servers/register"));
        assertEquals("Bearer reg-token", sent.headers().get("authorization"));
        assertTrue(sent.body().contains("\"capacity\":100"));
        assertTrue(sent.body().contains("\"type\":\"game\""));

        HeartbeatResult hb = c.heartbeat("game-1", new HeartbeatRequest(7, 0.3));
        assertEquals(10, hb.nextHeartbeatIn);
        assertEquals(1, beats.size());

        StatusResult off = c.unregister("game-1");
        assertEquals("offline", off.status);
    }

    @Test
    void registrySplitPort() throws IOException {
        try (FakeAtlas main = new FakeAtlas(); FakeAtlas registry = new FakeAtlas()) {
            main.routes.add(FakeAtlas.route("GET", "/v1/discovery/servers",
                    r -> FakeAtlas.reply(200, "{\"servers\":[{\"id\":\"game-1\",\"status\":\"online\"}]}")));
            registry.routes.add(FakeAtlas.route("POST", "/v1/registry/servers/register",
                    r -> FakeAtlas.reply(200, "{\"server_id\":\"game-1\",\"status\":\"online\"}")));
            try (AtlasClient split = new AtlasClient(new AtlasClientOptions()
                    .setBaseUrl(main.url())
                    .setRegistryBaseUrl(registry.url())
                    .setRegistryToken("reg-token"))) {
                split.register(new RegisterRequest(
                        "game-1", "T", "r", new Endpoint("h", 1), 1));
                registry.request("POST", Pattern.compile(".*/register"));
                assertEquals(1, split.listServers().size());
                main.request("GET", Pattern.compile(".*/discovery/servers"));
                assertEquals(0, main.requests.stream()
                        .filter(r -> r.path().startsWith("/v1/registry")).count());
            }
        }
    }

    // ── Discovery ──

    @Test
    void discoveryAndErrorMapping() throws IOException {
        start();
        Map<String, String>[] seen = new Map[1];
        s.routes.add(FakeAtlas.route("GET", "/v1/discovery/servers", r -> {
            seen[0] = r.query();
            return FakeAtlas.reply(200,
                    "{\"servers\":[{\"id\":\"game-1\",\"status\":\"online\",\"players\":12}]}");
        }));
        s.routes.add(FakeAtlas.route("GET", "/v1/discovery/servers/nope",
                r -> FakeAtlas.reply(404,
                        "{\"error\":{\"code\":\"SERVER_NOT_FOUND\",\"message\":\"server nope does not exist\"}}")));

        ServerFilter f = new ServerFilter();
        f.region = "cn-east";
        f.status = "online";
        f.limit = 20;
        List<Server> got = c.listServers(f);
        assertEquals(1, got.size());
        assertEquals("game-1", got.get(0).id);
        assertEquals(12, got.get(0).players);
        assertEquals(Map.of("region", "cn-east", "status", "online", "limit", "20"), seen[0]);

        AtlasError e = assertThrows(AtlasError.class, () -> c.getServer("nope"));
        assertEquals("SERVER_NOT_FOUND", e.getCode());
        assertEquals(404, e.getStatus());
    }

    // ── Directory write shapes (flat / nested / queued / deleted) ──

    @Test
    void directoryWriteShapes() throws IOException {
        start();
        List<String> bodies = new CopyOnWriteArrayList<>(Arrays.asList(
                "{\"account_id\":7,\"server_id\":\"game-1\",\"character_id\":1001,\"name\":\"Hero\"}",
                "{\"character\":{\"character_id\":1001,\"name\":\"Hero\"},\"status\":\"updated\"}",
                "{\"status\":\"queued\"}",
                "{\"status\":\"deleted\"}"));
        List<String> captured = new CopyOnWriteArrayList<>();
        s.routes.add(FakeAtlas.route("POST", "/v1/directory/characters", r -> {
            captured.add(r.body());
            return FakeAtlas.reply(201, bodies.remove(0));
        }));
        s.routes.add(FakeAtlas.route("PATCH", "/v1/directory/characters/1001", r -> {
            captured.add(r.body());
            return FakeAtlas.reply(200, bodies.remove(0));
        }));
        s.routes.add(FakeAtlas.route("DELETE", "/v1/directory/characters/1001",
                r -> FakeAtlas.reply(200, bodies.remove(0))));

        CreateCharacterRequest create = new CreateCharacterRequest(7, "game-1", 1001, "Hero");
        CharacterWriteResult wr = c.createCharacter(create);
        assertEquals("created", wr.status);
        assertEquals(1001, wr.character.characterId);
        assertTrue(captured.get(0).contains("\"account_id\":7"));
        assertTrue(captured.get(0).contains("\"character_id\":1001"));

        UpdateCharacterRequest patch = new UpdateCharacterRequest();
        patch.level = 10;
        wr = c.updateCharacter(1001, patch);
        assertEquals("updated", wr.status);
        assertEquals("Hero", wr.character.name);
        assertEquals("{\"level\":10}", captured.get(1)); // PATCH omits unset fields

        wr = c.updateCharacter(1001, new UpdateCharacterRequest()); // no-op patch
        assertEquals("{}", captured.get(2));
        assertEquals("queued", wr.status); // status-only reply → no character
        assertNull(wr.character);

        wr = c.deleteCharacter(1001);
        assertEquals("deleted", wr.status);
        assertNull(wr.character);
    }

    @Test
    void directoryListing() throws IOException {
        start();
        String c0 = "{\"account_id\":7,\"server_id\":\"game-1\",\"character_id\":0,\"name\":\"c0\"}";
        String c1 = "{\"account_id\":7,\"server_id\":\"game-1\",\"character_id\":1,\"name\":\"c1\"}";
        String c2 = "{\"account_id\":7,\"server_id\":\"game-1\",\"character_id\":2,\"name\":\"c2\"}";
        String all = "[" + c0 + "," + c1 + "," + c2 + "]";
        s.routes.add(FakeAtlas.route("GET", "/v1/directory/accounts/7/characters",
                r -> FakeAtlas.reply(200, "{\"characters\":" + all + "}")));
        Map<String, String>[] seen = new Map[1];
        s.routes.add(FakeAtlas.route("GET", "/v1/directory/servers/game-1/characters", r -> {
            seen[0] = r.query();
            return FakeAtlas.reply(200,
                    "{\"characters\":[" + c0 + "," + c1 + "],\"next_cursor\":\"cursor-2\"}");
        }));

        List<Character> byAccount = c.listCharactersByAccount(7);
        assertEquals(Arrays.asList(0L, 1L, 2L), byAccount.stream().map(x -> x.characterId).toList());

        CharacterPage page = c.listCharactersByServer("game-1", 2, "cursor-1");
        assertEquals(2, page.characters.size());
        assertEquals("cursor-2", page.nextCursor);
        assertEquals(Map.of("limit", "2", "cursor", "cursor-1"), seen[0]);
    }

    // ── Routing ──

    @Test
    void recommend() throws IOException {
        start();
        Map<String, String>[] seen = new Map[1];
        s.routes.add(FakeAtlas.route("GET", "/v1/routing/recommended", r -> {
            seen[0] = r.query();
            return FakeAtlas.reply(200,
                    "{\"server\":{\"id\":\"game-1\",\"status\":\"online\"},\"reason\":\"lowest_load\"}");
        }));

        Recommendation out = c.recommend(42, "cn-east", "1.0.0", "pc");
        assertEquals("game-1", out.server.id);
        assertEquals("lowest_load", out.reason);
        assertEquals(Map.of("account_id", "42", "region", "cn-east",
                "version", "1.0.0", "platform", "pc"), seen[0]);

        c.recommend();
        assertEquals(Map.of(), seen[0]); // zero-value args are omitted
    }

    // ── Admin ──

    @Test
    void admin() throws IOException {
        start();
        s.routes.add(FakeAtlas.route("POST", "/v1/admin/servers/game-1/maintenance",
                r -> FakeAtlas.reply(200, "{\"server_id\":\"game-1\",\"status\":\"maintenance\"}")));
        s.routes.add(FakeAtlas.route("POST", "/v1/admin/servers/game-1/enable",
                r -> FakeAtlas.reply(200, "{\"server_id\":\"game-1\",\"status\":\"online\"}")));
        s.routes.add(FakeAtlas.route("GET", "/v1/admin/stats",
                r -> FakeAtlas.reply(200, "{\"total_servers\":2,\"servers_by_status\":{\"online\":2},"
                        + "\"servers_by_region\":{\"cn-east\":2},\"servers_by_version\":{\"1.0.0\":2},"
                        + "\"total_players\":50,\"total_capacity\":4000,\"total_characters\":9}")));
        s.routes.add(FakeAtlas.route("GET", "/v1/admin/characters/search",
                r -> FakeAtlas.reply(200,
                        "{\"characters\":[{\"character_id\":1001,\"name\":\"Hero\"}],\"next_cursor\":\"\"}")));
        s.routes.add(FakeAtlas.route("POST", "/v1/admin/migrations",
                r -> FakeAtlas.reply(200, "{\"migration\":{\"id\":\"m-1\",\"source_servers\":[\"a\"],"
                        + "\"target_server\":\"b\",\"status\":\"pending\"}}")));
        s.routes.add(FakeAtlas.route("GET", "/v1/admin/migrations/m-1",
                r -> FakeAtlas.reply(200, "{\"migration\":{\"id\":\"m-1\",\"status\":\"completed\"}}")));
        s.routes.add(FakeAtlas.route("GET", "/v1/admin/migrations",
                r -> FakeAtlas.reply(200, "{\"migrations\":[{\"id\":\"m-1\"}]}")));
        s.routes.add(FakeAtlas.route("POST", "/v1/admin/migrations/m-1/rollback",
                r -> FakeAtlas.reply(200, "{\"migration\":{\"id\":\"m-1\",\"status\":\"rolled_back\"}}")));

        assertEquals("maintenance", c.setMaintenance("game-1").status);
        assertEquals("online", c.enable("game-1").status);
        FakeAtlas.Recorded life = s.request("POST", Pattern.compile(".*/servers/game-1/maintenance"));
        assertEquals("Bearer adm-key", life.headers().get("authorization"));

        Stats stats = c.getStats();
        assertEquals(2, stats.totalServers);
        assertEquals(9, stats.totalCharacters);
        assertEquals(Map.of("online", 2L), stats.serversByStatus);

        CharacterFilter f = new CharacterFilter();
        f.name = "Hero";
        f.minLevel = 10;
        f.limit = 5;
        CharacterPage page = c.searchCharacters(f);
        assertEquals("Hero", page.characters.get(0).name);
        FakeAtlas.Recorded search = s.request("GET", Pattern.compile(".*/characters/search"));
        assertEquals(Map.of("name", "Hero", "min_level", "10", "limit", "5"), search.query());

        Migration mig = c.createMigration(new CreateMigrationRequest(List.of("a"), "b"));
        assertEquals("m-1", mig.id);
        assertEquals("pending", mig.status);
        assertEquals("completed", c.getMigration("m-1").status);
        assertEquals(1, c.listMigrations(10).size());
        assertEquals("rolled_back", c.rollbackMigration("m-1").status);
    }

    // ── Retry & network errors ──

    @Test
    void retryOnTransient() throws IOException {
        start();
        Map<String, Integer> attempts = new java.util.concurrent.ConcurrentHashMap<>();
        s.routes.add(FakeAtlas.route("GET", "/v1/discovery/servers/.*", r -> {
            String sid = r.path().substring(r.path().lastIndexOf('/') + 1);
            int n = attempts.merge(sid, 1, Integer::sum);
            if (sid.equals("flaky")) {
                if (n < 3) {
                    return FakeAtlas.reply(502,
                            "{\"error\":{\"code\":\"BAD_GATEWAY\",\"message\":\"try again\"}}");
                }
                return FakeAtlas.reply(200, "{\"id\":\"flaky\",\"status\":\"online\"}");
            }
            return FakeAtlas.reply(404, "{\"error\":{\"code\":\"SERVER_NOT_FOUND\",\"message\":\"x\"}}");
        }));

        assertEquals("flaky", c.getServer("flaky").id);
        assertEquals(3, attempts.get("flaky")); // 2 failures + success

        assertThrows(AtlasError.class, () -> c.getServer("missing"));
        assertEquals(1, attempts.get("missing")); // 4xx must not be retried
    }

    @Test
    void networkError() {
        // Port 1 is unroutable — expect a mapped NETWORK error, not a raw throw.
        try (AtlasClient dead = new AtlasClient(new AtlasClientOptions()
                .setBaseUrl("http://127.0.0.1:1")
                .setMaxRetries(0)
                .setBaseBackoffMs(1))) {
            AtlasError e = assertThrows(AtlasError.class, dead::listServers);
            assertEquals(0, e.getStatus());
            assertEquals("NETWORK", e.getCode());
        }
    }

    @Test
    void httpErrorWithoutJsonBody() throws IOException {
        start();
        s.routes.add(FakeAtlas.route("GET", "/v1/admin/stats",
                r -> FakeAtlas.reply(500, "boom")));
        AtlasClient noRetry = new AtlasClient(new AtlasClientOptions()
                .setBaseUrl(s.url())
                .setAdminApiKey("adm-key")
                .setMaxRetries(0));
        try {
            AtlasError e = assertThrows(AtlasError.class, noRetry::getStats);
            assertEquals("HTTP_500", e.getCode());
        } finally {
            noRetry.close();
        }
    }

    // ── Auto heartbeat ──

    @Test
    void autoHeartbeat() throws IOException, InterruptedException {
        start();
        CopyOnWriteArrayList<String> beats = new CopyOnWriteArrayList<>();
        s.routes.add(FakeAtlas.route("POST", "/v1/registry/servers/game-1/heartbeat", r -> {
            beats.add(r.body());
            return FakeAtlas.reply(200, "{\"server_id\":\"game-1\",\"status\":\"online\"}");
        }));

        AutoHeartbeat loop = c.startHeartbeat("game-1", 50, new HeartbeatRequest(5, 0));
        loop.set(42, 0.5); // picked up by the first or second beat
        long deadline = System.currentTimeMillis() + 2000;
        while (beats.size() < 2 && System.currentTimeMillis() < deadline) {
            Thread.sleep(10);
        }
        loop.stop();
        loop.stop(); // idempotent

        assertTrue(beats.size() >= 2, "expected ≥2 heartbeats, got " + beats.size());
        assertEquals("{\"players\":42,\"load\":0.5}", beats.get(beats.size() - 1));
    }

    @Test
    void autoHeartbeatErrorCallback() {
        CopyOnWriteArrayList<AtlasError> errors = new CopyOnWriteArrayList<>();
        try (AtlasClient dead = new AtlasClient(new AtlasClientOptions()
                .setBaseUrl("http://127.0.0.1:1")
                .setMaxRetries(0)
                .setBaseBackoffMs(1))) {
            AutoHeartbeat loop = dead.startHeartbeat("game-1", 20,
                    new HeartbeatRequest(), errors::add);
            long deadline = System.currentTimeMillis() + 2000;
            while (errors.isEmpty() && System.currentTimeMillis() < deadline) {
                Thread.sleep(10);
            }
            loop.stop();
        } catch (InterruptedException e) {
            throw new RuntimeException(e);
        }
        assertTrue(!errors.isEmpty());
        assertEquals("NETWORK", errors.get(0).getCode());
    }

    // ── null tolerance (service may serialize empty collections as null) ──

    @Test
    void nullCollectionsBecomeEmpty() throws IOException {
        start();
        s.routes.add(FakeAtlas.route("GET", "/v1/discovery/servers",
                r -> FakeAtlas.reply(200, "{\"servers\":null}")));
        s.routes.add(FakeAtlas.route("GET", "/v1/directory/servers/x/characters",
                r -> FakeAtlas.reply(200, "{\"characters\":null}")));
        assertEquals(Collections.emptyList(), c.listServers());
        assertEquals(Collections.emptyList(),
                c.listCharactersByServer("x").characters);
        assertNotNull(c.listServers());
    }
}
