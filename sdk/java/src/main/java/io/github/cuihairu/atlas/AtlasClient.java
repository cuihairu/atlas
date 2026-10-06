package io.github.cuihairu.atlas;

import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import okhttp3.HttpUrl;
import okhttp3.MediaType;
import okhttp3.OkHttpClient;
import okhttp3.Request;
import okhttp3.RequestBody;
import okhttp3.Response;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.concurrent.ThreadLocalRandom;
import java.util.concurrent.TimeUnit;
import java.util.function.Consumer;

/** Atlas REST client (OkHttp + Gson). Same API surface as the Go/C++/
 * Python/JS SDKs: five API groups (Registry / Discovery / Directory /
 * Routing / Admin), transient-failure retry with full-jitter backoff,
 * and an auto-heartbeat loop. Methods throw {@link AtlasError}. */
public class AtlasClient implements AutoCloseable {

    private static final MediaType JSON = MediaType.parse("application/json");

    private final AtlasClientOptions opts;
    private final OkHttpClient http;
    private final HttpUrl baseUrl;
    private final HttpUrl registryBaseUrl;

    public AtlasClient() {
        this(new AtlasClientOptions());
    }

    public AtlasClient(AtlasClientOptions options) {
        this.opts = options;
        OkHttpClient.Builder b = new OkHttpClient.Builder();
        if (options.getTimeoutMs() > 0) {
            b.callTimeout(options.getTimeoutMs(), TimeUnit.MILLISECONDS);
        }
        this.http = b.build();
        this.baseUrl = HttpUrl.parse(trimSlash(options.getBaseUrl()));
        this.registryBaseUrl = options.getRegistryBaseUrl() != null
                ? HttpUrl.parse(trimSlash(options.getRegistryBaseUrl()))
                : this.baseUrl;
        if (this.baseUrl == null || this.registryBaseUrl == null) {
            throw new IllegalArgumentException("invalid baseUrl / registryBaseUrl");
        }
    }

    private static String trimSlash(String url) {
        return url.replaceAll("/+$", "");
    }

    private static String trimLeadingSlash(String path) {
        return path.replaceAll("^/+", "");
    }

    /** Lifecycle hook — releases OkHttp's executor and pooled connections.
     * Does NOT stop heartbeat loops; call {@link AutoHeartbeat#stop()} yourself. */
    @Override
    public void close() {
        http.dispatcher().executorService().shutdown();
        http.connectionPool().evictAll();
    }

    // ── core request path ──

    private <T> T request(String method, String path, Map<String, String> params,
                          Object jsonBody, String bearer, boolean registry,
                          Class<T> resultType) {
        HttpUrl root = registry ? registryBaseUrl : baseUrl;
        HttpUrl url = root.newBuilder()
                .addPathSegments(trimLeadingSlash(path))
                .build();
        if (params != null) {
            for (Map.Entry<String, String> e : params.entrySet()) {
                url = url.newBuilder().addQueryParameter(e.getKey(), e.getValue()).build();
            }
        }
        Request.Builder rb = new Request.Builder().url(url)
                .header("Accept", "application/json");
        if (bearer != null && !bearer.isEmpty()) {
            rb.header("Authorization", "Bearer " + bearer);
        }
        for (Map.Entry<String, String> e : opts.getDefaultHeaders().entrySet()) {
            rb.header(e.getKey(), e.getValue());
        }
        RequestBody body;
        if (jsonBody != null) {
            body = RequestBody.create(GsonUtil.GSON.toJson(jsonBody), JSON);
        } else if (method.equals("GET") || method.equals("HEAD")) {
            body = null;
        } else {
            // OkHttp requires an explicit (possibly empty) body for POST/PATCH/DELETE.
            body = RequestBody.create(new byte[0], null);
        }
        rb.method(method, body);
        Request httpRequest = rb.build();

        int attempt = 0;
        int maxRetries = Math.max(opts.getMaxRetries(), 0);
        while (true) {
            try (Response resp = http.newCall(httpRequest).execute()) {
                String text = resp.body() != null ? resp.body().string() : "";
                if (resp.isSuccessful()) {
                    return text.isEmpty() ? null : GsonUtil.GSON.fromJson(text, resultType);
                }
                if (resp.code() < 500 || attempt >= maxRetries) {
                    throw parseError(resp.code(), text);
                }
            } catch (java.io.IOException e) {
                if (attempt >= maxRetries) {
                    throw new AtlasError(0, "NETWORK", method + " " + path + ": " + e);
                }
            }
            sleep(backoffMs(opts.getBaseBackoffMs(), attempt));
            attempt += 1;
        }
    }

    private static long backoffMs(long base, int attempt) {
        double ceiling = Math.min(base * Math.pow(2, attempt), 10_000);
        return (long) (ThreadLocalRandom.current().nextDouble() * ceiling);
    }

    private static void sleep(long ms) {
        try {
            Thread.sleep(ms);
        } catch (InterruptedException e) {
            Thread.currentThread().interrupt();
            throw new AtlasError(0, "NETWORK", "interrupted during backoff");
        }
    }

    /** Map an error reply body onto AtlasError; non-JSON bodies become
     * "HTTP_&lt;status&gt;" with the raw text truncated. */
    static AtlasError parseError(int status, String body) {
        try {
            JsonElement el = JsonParser.parseString(body);
            if (el.isJsonObject()) {
                JsonElement err = el.getAsJsonObject().get("error");
                if (err != null && err.isJsonObject()) {
                    JsonObject o = err.getAsJsonObject();
                    String code = o.has("code") ? o.get("code").getAsString() : "";
                    String message = o.has("message") ? o.get("message").getAsString() : "";
                    return new AtlasError(status, code, message);
                }
            }
        } catch (Exception ignored) {
            // fall through to the generic mapping
        }
        String text = body == null ? "" : body;
        return new AtlasError(status, "HTTP_" + status, text.substring(0, Math.min(text.length(), 200)));
    }

    /** Normalize the three reply shapes: nested envelope, flat synchronous
     * character object, and status-only queued/deleted. */
    static CharacterWriteResult parseCharacterWrite(JsonElement body, String syncStatus) {
        if (body == null || !body.isJsonObject()) {
            return new CharacterWriteResult(null, syncStatus);
        }
        JsonObject o = body.getAsJsonObject();
        JsonElement nested = o.get("character");
        if (nested != null && nested.isJsonObject()) {
            Character c = GsonUtil.GSON.fromJson(nested, Character.class);
            String status = o.has("status") && !o.get("status").isJsonNull()
                    ? o.get("status").getAsString() : syncStatus;
            return new CharacterWriteResult(c, status);
        }
        if (o.has("character_id")) {
            Character c = GsonUtil.GSON.fromJson(o, Character.class);
            return new CharacterWriteResult(c, syncStatus);
        }
        String status = o.has("status") && !o.get("status").isJsonNull()
                ? o.get("status").getAsString() : "";
        return new CharacterWriteResult(null, status);
    }

    // ── Registry (service token, split port) ──

    public RegisterResult register(RegisterRequest req) {
        RegisterResult r = request("POST", "/v1/registry/servers/register", null, req,
                opts.getRegistryToken(), true, RegisterResult.class);
        return r != null ? r : new RegisterResult();
    }

    public HeartbeatResult heartbeat(String serverId, HeartbeatRequest req) {
        HeartbeatResult r = request("POST", "/v1/registry/servers/" + serverId + "/heartbeat",
                null, req, opts.getRegistryToken(), true, HeartbeatResult.class);
        if (r == null) {
            r = new HeartbeatResult();
        }
        if (r.serverId.isEmpty()) {
            r.serverId = serverId;
        }
        return r;
    }

    public StatusResult unregister(String serverId) {
        StatusResult r = request("POST", "/v1/registry/servers/" + serverId + "/unregister",
                null, null, opts.getRegistryToken(), true, StatusResult.class);
        return r != null ? r : new StatusResult();
    }

    // ── Discovery (public) ──

    public List<Server> listServers() {
        return listServers(new ServerFilter());
    }

    public List<Server> listServers(ServerFilter filter) {
        ServersResponse body = request("GET", "/v1/discovery/servers",
                serverFilterParams(filter), null, "", false, ServersResponse.class);
        return body != null && body.servers != null ? body.servers : new ArrayList<>();
    }

    public Server getServer(String serverId) {
        Server s = request("GET", "/v1/discovery/servers/" + serverId, null, null,
                "", false, Server.class);
        return s != null ? s : new Server();
    }

    // ── Directory (public) ──

    public CharacterWriteResult createCharacter(CreateCharacterRequest req) {
        JsonElement body = request("POST", "/v1/directory/characters", null, req, "", false, JsonElement.class);
        return parseCharacterWrite(body, "created");
    }

    public Character getCharacter(long characterId) {
        Character c = request("GET", "/v1/directory/characters/" + characterId, null, null,
                "", false, Character.class);
        return c != null ? c : new Character();
    }

    public List<Character> listCharactersByAccount(long accountId) {
        CharactersResponse body = request("GET",
                "/v1/directory/accounts/" + accountId + "/characters", null, null,
                "", false, CharactersResponse.class);
        return body != null && body.characters != null ? body.characters : new ArrayList<>();
    }

    public CharacterPage listCharactersByServer(String serverId) {
        return listCharactersByServer(serverId, 0, "");
    }

    public CharacterPage listCharactersByServer(String serverId, int limit, String cursor) {
        Map<String, String> params = new LinkedHashMap<>();
        if (limit > 0) {
            params.put("limit", String.valueOf(limit));
        }
        if (cursor != null && !cursor.isEmpty()) {
            params.put("cursor", cursor);
        }
        CharacterPage page = request("GET",
                "/v1/directory/servers/" + serverId + "/characters", params, null,
                "", false, CharacterPage.class);
        return normalizePage(page);
    }

    /** A JSON explicit null for "characters" overrides the field default. */
    private static CharacterPage normalizePage(CharacterPage page) {
        if (page == null) {
            return new CharacterPage();
        }
        if (page.characters == null) {
            page.characters = new ArrayList<>();
        }
        return page;
    }

    public CharacterWriteResult updateCharacter(long characterId, UpdateCharacterRequest req) {
        JsonElement body = request("PATCH", "/v1/directory/characters/" + characterId,
                null, req, "", false, JsonElement.class);
        return parseCharacterWrite(body, "updated");
    }

    public CharacterWriteResult deleteCharacter(long characterId) {
        CharacterWriteResult r = request("DELETE", "/v1/directory/characters/" + characterId,
                null, null, "", false, CharacterWriteResult.class);
        return r != null ? r : new CharacterWriteResult(null, "deleted");
    }

    // ── Routing (public) ──

    public Recommendation recommend() {
        return recommend(0, null, null, null);
    }

    public Recommendation recommend(long accountId, String region, String version, String platform) {
        Map<String, String> params = new LinkedHashMap<>();
        if (accountId > 0) {
            params.put("account_id", String.valueOf(accountId));
        }
        if (region != null && !region.isEmpty()) params.put("region", region);
        if (version != null && !version.isEmpty()) params.put("version", version);
        if (platform != null && !platform.isEmpty()) params.put("platform", platform);
        Recommendation r = request("GET", "/v1/routing/recommended", params, null,
                "", false, Recommendation.class);
        return r != null ? r : new Recommendation();
    }

    // ── Admin (API key) ──

    private StatusResult lifecycle(String action, String serverId) {
        StatusResult r = request("POST", "/v1/admin/servers/" + serverId + "/" + action,
                null, null, opts.getAdminApiKey(), false, StatusResult.class);
        return r != null ? r : new StatusResult();
    }

    public StatusResult setMaintenance(String serverId) {
        return lifecycle("maintenance", serverId);
    }

    public StatusResult setDrain(String serverId) {
        return lifecycle("drain", serverId);
    }

    public StatusResult enable(String serverId) {
        return lifecycle("enable", serverId);
    }

    public StatusResult disable(String serverId) {
        return lifecycle("disable", serverId);
    }

    public Stats getStats() {
        Stats s = request("GET", "/v1/admin/stats", null, null,
                opts.getAdminApiKey(), false, Stats.class);
        return s != null ? s : new Stats();
    }

    public CharacterPage searchCharacters(CharacterFilter filter) {
        CharacterPage page = request("GET", "/v1/admin/characters/search",
                characterFilterParams(filter), null, opts.getAdminApiKey(), false,
                CharacterPage.class);
        return normalizePage(page);
    }

    public Migration createMigration(CreateMigrationRequest req) {
        MigrationResponse body = request("POST", "/v1/admin/migrations", null, req,
                opts.getAdminApiKey(), false, MigrationResponse.class);
        return body != null && body.migration != null ? body.migration : new Migration();
    }

    public Migration getMigration(String migrationId) {
        MigrationResponse body = request("GET", "/v1/admin/migrations/" + migrationId,
                null, null, opts.getAdminApiKey(), false, MigrationResponse.class);
        return body != null && body.migration != null ? body.migration : new Migration();
    }

    public List<Migration> listMigrations() {
        return listMigrations(0);
    }

    public List<Migration> listMigrations(int limit) {
        Map<String, String> params = new LinkedHashMap<>();
        if (limit > 0) {
            params.put("limit", String.valueOf(limit));
        }
        MigrationsResponse body = request("GET", "/v1/admin/migrations", params, null,
                opts.getAdminApiKey(), false, MigrationsResponse.class);
        return body != null && body.migrations != null ? body.migrations : new ArrayList<>();
    }

    public Migration rollbackMigration(String migrationId) {
        MigrationResponse body = request("POST", "/v1/admin/migrations/" + migrationId + "/rollback",
                null, null, opts.getAdminApiKey(), false, MigrationResponse.class);
        return body != null && body.migration != null ? body.migration : new Migration();
    }

    // ── Auto heartbeat ──

    /** Start reporting immediately, then every 10s (default interval). */
    public AutoHeartbeat startHeartbeat(String serverId) {
        return startHeartbeat(serverId, 10_000, new HeartbeatRequest(), null);
    }

    /** Start reporting immediately, then every {@code intervalMs}. */
    public AutoHeartbeat startHeartbeat(String serverId, long intervalMs, HeartbeatRequest initial) {
        return startHeartbeat(serverId, intervalMs, initial, null);
    }

    /** Start reporting immediately, then every {@code intervalMs};
     * failures go to {@code onError} and the loop keeps running. */
    public AutoHeartbeat startHeartbeat(String serverId, long intervalMs,
                                        HeartbeatRequest initial, Consumer<AtlasError> onError) {
        return new AutoHeartbeat(this, serverId, intervalMs, initial, onError);
    }

    // ── query helpers ──

    private static Map<String, String> serverFilterParams(ServerFilter f) {
        ServerFilter x = f != null ? f : new ServerFilter();
        Map<String, String> out = new LinkedHashMap<>();
        if (x.region != null && !x.region.isEmpty()) out.put("region", x.region);
        if (x.version != null && !x.version.isEmpty()) out.put("version", x.version);
        if (x.platform != null && !x.platform.isEmpty()) out.put("platform", x.platform);
        if (x.status != null && !x.status.isEmpty()) out.put("status", x.status);
        if (x.limit != null && x.limit > 0) out.put("limit", String.valueOf(x.limit));
        return out;
    }

    private static Map<String, String> characterFilterParams(CharacterFilter f) {
        CharacterFilter x = f != null ? f : new CharacterFilter();
        Map<String, String> out = new LinkedHashMap<>();
        if (x.name != null && !x.name.isEmpty()) out.put("name", x.name);
        if (x.cursor != null && !x.cursor.isEmpty()) out.put("cursor", x.cursor);
        if (x.serverId != null && !x.serverId.isEmpty()) out.put("server_id", x.serverId);
        if (x.classId != null && x.classId > 0) out.put("class_id", String.valueOf(x.classId));
        if (x.minLevel != null && x.minLevel > 0) out.put("min_level", String.valueOf(x.minLevel));
        if (x.maxLevel != null && x.maxLevel > 0) out.put("max_level", String.valueOf(x.maxLevel));
        if (x.limit != null && x.limit > 0) out.put("limit", String.valueOf(x.limit));
        return out;
    }

    // ── response envelopes ──

    private static final class ServersResponse {
        List<Server> servers;
    }

    private static final class CharactersResponse {
        List<Character> characters;
        @com.google.gson.annotations.SerializedName("next_cursor")
        String nextCursor;
    }

    private static final class MigrationResponse {
        Migration migration;
    }

    private static final class MigrationsResponse {
        List<Migration> migrations;
    }
}
