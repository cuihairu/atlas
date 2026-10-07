using System.Globalization;
using System.Net;
using System.Net.Http.Headers;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;

namespace Atlas;

/// <summary>Atlas client for the five REST API groups (TODO v0.1.11):
/// Registry, Discovery, Directory, Routing and Admin. Mirrors the Go SDK
/// surface; transient failures (network errors and 5xx) retry with
/// full-jitter exponential backoff, 4xx raises <see cref="AtlasError"/>.</summary>
public sealed class AtlasClient : IDisposable, IAsyncDisposable
{
    private static readonly JsonSerializerOptions JsonOpts = new()
    {
        PropertyNamingPolicy = JsonNamingPolicy.SnakeCaseLower,
        DefaultIgnoreCondition = JsonIgnoreCondition.WhenWritingNull,
        PropertyNameCaseInsensitive = true,
    };

    private readonly HttpClient _http;
    private readonly bool _ownsHttp;
    private readonly string _publicBase;
    private readonly string _registryBase;
    private readonly AtlasClientOptions _opts;

    public AtlasClient(AtlasClientOptions? options = null)
    {
        _opts = options ?? new AtlasClientOptions();
        _publicBase = NormalizeBase(_opts.BaseUrl);
        _registryBase = NormalizeBase(_opts.RegistryBaseUrl ?? _opts.BaseUrl);
        if (_opts.HttpInvoker is HttpClient injected)
        {
            _http = injected;
            _ownsHttp = false;
        }
        else
        {
            var handler = new SocketsHttpHandler
            {
                AutomaticDecompression = DecompressionMethods.All,
            };
            _http = new HttpClient(handler)
            {
                Timeout = _opts.TimeoutMs > 0
                    ? TimeSpan.FromMilliseconds(_opts.TimeoutMs)
                    : Timeout.InfiniteTimeSpan,
            };
            _ownsHttp = true;
        }
    }

    // ── Registry ────────────────────────────────────────────────

    /// <summary>Registers this game server. The reply status is "starting";
    /// send one heartbeat to become discoverable ("online").</summary>
    public Task<RegisterResult> RegisterAsync(RegisterRequest req, CancellationToken ct = default) =>
        SendAsync<RegisterResult>(HttpMethod.Post, _registryBase,
            "/v1/registry/servers/register", null, req, Auth.Registry, ct);

    /// <summary>Reports live load. One synchronous beat right after
    /// RegisterAsync makes the server "online" before auto-heartbeat starts.</summary>
    public Task<HeartbeatResult> HeartbeatAsync(string serverId, HeartbeatRequest req, CancellationToken ct = default) =>
        SendAsync<HeartbeatResult>(HttpMethod.Post, _registryBase,
            $"/v1/registry/servers/{Esc(serverId)}/heartbeat", null, req, Auth.Registry, ct);

    /// <summary>Removes the server from the registry (graceful shutdown).</summary>
    public Task<StatusResult> UnregisterAsync(string serverId, CancellationToken ct = default) =>
        SendAsync<StatusResult>(HttpMethod.Post, _registryBase,
            $"/v1/registry/servers/{Esc(serverId)}/unregister", null, null, Auth.Registry, ct);

    // ── Discovery (public) ──────────────────────────────────────

    /// <summary>Lists registered servers, narrowed by filter (null = all).</summary>
    public async Task<List<Server>> ListServersAsync(ServerFilter? filter = null, CancellationToken ct = default)
    {
        var f = filter ?? new ServerFilter();
        var q = new Dictionary<string, string>();
        if (!string.IsNullOrEmpty(f.Region)) q["region"] = f.Region;
        if (!string.IsNullOrEmpty(f.Version)) q["version"] = f.Version;
        if (!string.IsNullOrEmpty(f.Platform)) q["platform"] = f.Platform;
        if (!string.IsNullOrEmpty(f.Status)) q["status"] = f.Status;
        if (f.Limit > 0) q["limit"] = f.Limit.ToString(CultureInfo.InvariantCulture);
        var (json, _) = await SendForJsonAsync(HttpMethod.Get, _publicBase,
            "/v1/discovery/servers", q, null, Auth.None, ct).ConfigureAwait(false);
        return JsonDoc(json).RootElement.TryGetProperty("servers", out var list)
            && list.ValueKind == JsonValueKind.Array
            ? list.Deserialize<List<Server>>(JsonOpts) ?? []
            : [];
    }

    /// <summary>Fetches one server with runtime state.</summary>
    public Task<Server> GetServerAsync(string serverId, CancellationToken ct = default) =>
        SendAsync<Server>(HttpMethod.Get, _publicBase,
            $"/v1/discovery/servers/{Esc(serverId)}", null, null, Auth.None, ct);

    // ── Directory (public) ──────────────────────────────────────

    /// <summary>Adds a character index entry. With an asynchronous event
    /// adapter the result carries Status "queued" and a null Character.</summary>
    public async Task<CharacterWriteResult> CreateCharacterAsync(CreateCharacterRequest req, CancellationToken ct = default)
    {
        var (json, _) = await SendForJsonAsync(HttpMethod.Post, _publicBase,
            "/v1/directory/characters", null, req, Auth.None, ct).ConfigureAwait(false);
        return ParseWrite(json, "created");
    }

    /// <summary>Fetches one character index entry.</summary>
    public Task<Character> GetCharacterAsync(long characterId, CancellationToken ct = default) =>
        SendAsync<Character>(HttpMethod.Get, _publicBase,
            $"/v1/directory/characters/{characterId}", null, null, Auth.None, ct);

    /// <summary>Lists a character directory by account (account → characters).</summary>
    public async Task<List<Character>> ListCharactersByAccountAsync(long accountId, CancellationToken ct = default)
    {
        var (json, _) = await SendForJsonAsync(HttpMethod.Get, _publicBase,
            $"/v1/directory/accounts/{accountId}/characters", null, null,
            Auth.None, ct).ConfigureAwait(false);
        return JsonDoc(json).RootElement.TryGetProperty("characters", out var list)
            && list.ValueKind == JsonValueKind.Array
            ? list.Deserialize<List<Character>>(JsonOpts) ?? []
            : [];
    }

    /// <summary>Lists a server's characters with cursor pagination.</summary>
    public async Task<CharacterPage> ListCharactersByServerAsync(
        string serverId, int limit = 0, string? cursor = null, CancellationToken ct = default)
    {
        var q = new Dictionary<string, string>();
        if (limit > 0) q["limit"] = limit.ToString(CultureInfo.InvariantCulture);
        if (!string.IsNullOrEmpty(cursor)) q["cursor"] = cursor;
        var (json, _) = await SendForJsonAsync(HttpMethod.Get, _publicBase,
            $"/v1/directory/servers/{Esc(serverId)}/characters", q, null,
            Auth.None, ct).ConfigureAwait(false);
        return NormalizePage(JsonSerializer.Deserialize<CharacterPage>(json, JsonOpts));
    }

    /// <summary>Patches a character (null fields unchanged).</summary>
    public async Task<CharacterWriteResult> UpdateCharacterAsync(
        long characterId, UpdateCharacterRequest req, CancellationToken ct = default)
    {
        var (json, _) = await SendForJsonAsync(HttpMethod.Patch, _publicBase,
            $"/v1/directory/characters/{characterId}", null, req, Auth.None, ct).ConfigureAwait(false);
        return ParseWrite(json, "updated");
    }

    /// <summary>Deletes a character index entry.</summary>
    public Task<CharacterWriteResult> DeleteCharacterAsync(long characterId, CancellationToken ct = default) =>
        SendAsync<CharacterWriteResult>(HttpMethod.Delete, _publicBase,
            $"/v1/directory/characters/{characterId}", null, null, Auth.None, ct);

    // ── Routing (public) ────────────────────────────────────────

    /// <summary>Recommends a server for the account (lowest load / capacity /
    /// existing character). accountID ≤ 0 skips the character tiebreak.</summary>
    public Task<Recommendation> RecommendAsync(long accountId, string? region = null,
        string? version = null, string? platform = null, CancellationToken ct = default)
    {
        var q = new Dictionary<string, string>();
        if (accountId > 0) q["account_id"] = accountId.ToString(CultureInfo.InvariantCulture);
        if (!string.IsNullOrEmpty(region)) q["region"] = region;
        if (!string.IsNullOrEmpty(version)) q["version"] = version;
        if (!string.IsNullOrEmpty(platform)) q["platform"] = platform;
        return SendAsync<Recommendation>(HttpMethod.Get, _publicBase,
            "/v1/routing/recommended", q, null, Auth.None, ct);
    }

    // ── Admin ───────────────────────────────────────────────────

    /// <summary>Puts a server into maintenance (stops discovery traffic).</summary>
    public Task<StatusResult> SetMaintenanceAsync(string serverId, CancellationToken ct = default) =>
        LifecycleAsync("maintenance", serverId, ct);

    /// <summary>Drains a server (keeps current players, blocks new ones).</summary>
    public Task<StatusResult> SetDrainAsync(string serverId, CancellationToken ct = default) =>
        LifecycleAsync("drain", serverId, ct);

    /// <summary>Re-enables a server.</summary>
    public Task<StatusResult> EnableAsync(string serverId, CancellationToken ct = default) =>
        LifecycleAsync("enable", serverId, ct);

    /// <summary>Disables a server (registration-level kill switch).</summary>
    public Task<StatusResult> DisableAsync(string serverId, CancellationToken ct = default) =>
        LifecycleAsync("disable", serverId, ct);

    private Task<StatusResult> LifecycleAsync(string action, string serverId, CancellationToken ct) =>
        SendAsync<StatusResult>(HttpMethod.Post, _publicBase,
            $"/v1/admin/servers/{Esc(serverId)}/{action}", null, null, Auth.Admin, ct);

    /// <summary>Returns the fleet overview (Admin).</summary>
    public Task<Stats> StatsAsync(CancellationToken ct = default) =>
        SendAsync<Stats>(HttpMethod.Get, _publicBase, "/v1/admin/stats", null,
            null, Auth.Admin, ct);

    /// <summary>Searches characters by name/server/class/level range (Admin).</summary>
    public async Task<CharacterPage> SearchCharactersAsync(CharacterFilter filter, CancellationToken ct = default)
    {
        var q = new Dictionary<string, string>();
        if (!string.IsNullOrEmpty(filter.Name)) q["name"] = filter.Name;
        if (!string.IsNullOrEmpty(filter.ServerId)) q["server_id"] = filter.ServerId;
        if (filter.ClassId is int cls) q["class_id"] = cls.ToString(CultureInfo.InvariantCulture);
        if (filter.MinLevel is int min) q["min_level"] = min.ToString(CultureInfo.InvariantCulture);
        if (filter.MaxLevel is int max) q["max_level"] = max.ToString(CultureInfo.InvariantCulture);
        if (filter.Limit > 0) q["limit"] = filter.Limit.ToString(CultureInfo.InvariantCulture);
        if (!string.IsNullOrEmpty(filter.Cursor)) q["cursor"] = filter.Cursor;
        var (json, _) = await SendForJsonAsync(HttpMethod.Get, _publicBase,
            "/v1/admin/characters/search", q, null, Auth.Admin, ct).ConfigureAwait(false);
        return NormalizePage(JsonSerializer.Deserialize<CharacterPage>(json, JsonOpts));
    }

    /// <summary>Starts a character migration job (Admin).</summary>
    public Task<Migration> CreateMigrationAsync(CreateMigrationRequest req, CancellationToken ct = default) =>
        SendAsync<Migration>(HttpMethod.Post, _publicBase, "/v1/admin/migrations",
            null, req, Auth.Admin, ct);

    /// <summary>Fetches one migration job (Admin).</summary>
    public Task<Migration> GetMigrationAsync(string id, CancellationToken ct = default) =>
        SendAsync<Migration>(HttpMethod.Get, _publicBase,
            $"/v1/admin/migrations/{Esc(id)}", null, null, Auth.Admin, ct);

    /// <summary>Lists migration jobs (Admin).</summary>
    public async Task<List<Migration>> ListMigrationsAsync(int limit = 0, CancellationToken ct = default)
    {
        var q = new Dictionary<string, string>();
        if (limit > 0) q["limit"] = limit.ToString(CultureInfo.InvariantCulture);
        var (json, _) = await SendForJsonAsync(HttpMethod.Get, _publicBase,
            "/v1/admin/migrations", q, null, Auth.Admin, ct).ConfigureAwait(false);
        using var doc = JsonDoc(json);
        return doc.RootElement.TryGetProperty("migrations", out var list)
            && list.ValueKind == JsonValueKind.Array
            ? list.Deserialize<List<Migration>>(JsonOpts) ?? []
            : [];
    }

    /// <summary>Rolls a migration back (Admin).</summary>
    public Task<Migration> RollbackMigrationAsync(string id, CancellationToken ct = default) =>
        SendAsync<Migration>(HttpMethod.Post, _publicBase,
            $"/v1/admin/migrations/{Esc(id)}/rollback", null, null, Auth.Admin, ct);

    // ── Auto heartbeat ──────────────────────────────────────────

    /// <summary>Starts a heartbeat loop: one immediate beat, then every
    /// <paramref name="interval"/> until <see cref="AutoHeartbeat.Stop"/>.
    /// Send one synchronous <see cref="HeartbeatAsync"/> first if the server
    /// must be "online" (recommendable) right away.</summary>
    public AutoHeartbeat StartHeartbeat(string serverId, TimeSpan interval,
        HeartbeatRequest? req = null, Action<AtlasError>? onError = null) =>
        new(this, serverId, interval, req ?? new HeartbeatRequest(), onError);

    // ── Core request path ───────────────────────────────────────

    private enum Auth { None, Registry, Admin }

    private async Task<T> SendAsync<T>(HttpMethod method, string baseUrl,
        string path, Dictionary<string, string>? query, object? body,
        Auth auth, CancellationToken ct)
    {
        var (json, _) = await SendForJsonAsync(method, baseUrl, path, query,
            body, auth, ct).ConfigureAwait(false);
        return JsonSerializer.Deserialize<T>(json, JsonOpts)
            ?? throw new AtlasError(0, "EMPTY_REPLY", "server returned an empty payload");
    }

    private async Task<(string Json, HttpResponseMessage Response)> SendForJsonAsync(
        HttpMethod method, string baseUrl, string path,
        Dictionary<string, string>? query, object? body, Auth auth,
        CancellationToken ct)
    {
        var bodyJson = body is null ? null : JsonSerializer.Serialize(body, JsonOpts);
        string url = Query(baseUrl + path, query);

        int attempts = Math.Max(_opts.MaxRetries, 0) + 1;
        for (int attempt = 0; ; attempt++)
        {
            try
            {
                using var req = new HttpRequestMessage(method, url);
                if (bodyJson is not null)
                    req.Content = new StringContent(bodyJson, Encoding.UTF8, "application/json");
                ApplyAuth(req.Headers, auth);
                if (_opts.DefaultHeaders is not null)
                {
                    foreach (var kv in _opts.DefaultHeaders)
                        req.Headers.TryAddWithoutValidation(kv.Key, kv.Value);
                }

                using var resp = await _http.SendAsync(req, ct).ConfigureAwait(false);
                var text = await resp.Content.ReadAsStringAsync(ct).ConfigureAwait(false);

                if ((int)resp.StatusCode >= 200 && (int)resp.StatusCode < 300)
                    return (text, resp);

                var err = ParseError((int)resp.StatusCode, text);
                if ((int)resp.StatusCode < 500 || attempt >= attempts - 1)
                    throw err; // 4xx and exhausted retries stop here
            }
            catch (AtlasError)
            {
                throw;
            }
            catch (Exception ex) when (IsNetworkError(ex, ct))
            {
                if (attempt >= attempts - 1)
                    throw new AtlasError(0, "NETWORK", ex.Message);
            }

            await Task.Delay(BackoffDelay(attempt), ct).ConfigureAwait(false);
        }
    }

    private void ApplyAuth(HttpRequestHeaders headers, Auth auth)
    {
        switch (auth)
        {
            case Auth.Registry when !string.IsNullOrEmpty(_opts.RegistryToken):
                headers.Authorization = new("Bearer", _opts.RegistryToken);
                break;
            case Auth.Admin when !string.IsNullOrEmpty(_opts.AdminApiKey):
                headers.Authorization = new("Bearer", _opts.AdminApiKey);
                break;
        }
    }

    private int BackoffDelay(int attempt)
    {
        long ceiling = Math.Min((long)_opts.BaseBackoffMs << attempt, 10_000);
        return Random.Shared.Next(0, (int)ceiling + 1);
    }

    private static bool IsNetworkError(Exception ex, CancellationToken ct) => ex switch
    {
        OperationCanceledException when !ct.IsCancellationRequested => true, // timeout
        HttpRequestException => true,
        _ => false,
    };

    private static AtlasError ParseError(int status, string body)
    {
        try
        {
            using var doc = JsonDocument.Parse(body);
            if (doc.RootElement.ValueKind == JsonValueKind.Object &&
                doc.RootElement.TryGetProperty("error", out var err) &&
                err.ValueKind == JsonValueKind.Object)
            {
                string code = "", message = "";
                if (err.TryGetProperty("code", out var c)) code = c.GetString() ?? "";
                if (err.TryGetProperty("message", out var m)) message = m.GetString() ?? "";
                return new AtlasError(status, code, message);
            }
        }
        catch (JsonException)
        {
            // non-JSON error body: fall through to the generic mapping
        }
        // Atlas nests the error envelope under "error"; anything else (flat
        // JSON, plain text) maps to HTTP_<status> with the raw text — parity
        // with the Go/JS/Java/Python/C++ SDKs.
        string text = body ?? "";
        return new AtlasError(status, "HTTP_" + status, text[..Math.Min(text.Length, 200)]);
    }

    // ── Reply normalization helpers ─────────────────────────────

    private JsonDocument JsonDoc(string json)
    {
        try { return JsonDocument.Parse(json); }
        catch (JsonException e) { throw new AtlasError(0, "BAD_REPLY", e.Message); }
    }

    /// <summary>Accepts both write-reply shapes: the nested
    /// {"character":...,"status":...} envelope and the flat character object
    /// the synchronous REST path returns (which carries no status — callers
    /// fill in <paramref name="fallbackStatus"/>).</summary>
    private static CharacterWriteResult NormalizeWrite(CharacterWriteResult r, string fallbackStatus)
    {
        r.Status = string.IsNullOrEmpty(r.Status) ? fallbackStatus : r.Status;
        return r;
    }

    /// <summary>Tolerates Atlas serializing empty collections as null
    /// ({"characters":null} → []).</summary>
    private static CharacterPage? NormalizePage(CharacterPage? page)
    {
        if (page is not null && page.Characters is null)
            page.Characters = [];
        return page;
    }

    /// <summary>Accepts both write-reply shapes Atlas produces: the nested
    /// {"character":...,"status":...} envelope and the flat character object
    /// the synchronous REST path returns (which carries no status —
    /// <paramref name="fallbackStatus"/> fills it in, "created"/"updated"
    /// by endpoint). A bare {"status":...} reply (queued/deleted) yields a
    /// null Character.</summary>
    private CharacterWriteResult ParseWrite(string json, string fallbackStatus)
    {
        using var doc = JsonDoc(json);
        var root = doc.RootElement;
        if (root.ValueKind != JsonValueKind.Object)
            return new CharacterWriteResult { Status = fallbackStatus };

        string status = root.TryGetProperty("status", out var st)
            ? st.GetString() ?? "" : "";
        if (root.TryGetProperty("character", out var nested) &&
            nested.ValueKind == JsonValueKind.Object)
        {
            return new CharacterWriteResult
            {
                Character = nested.Deserialize<Character>(JsonOpts),
                Status = string.IsNullOrEmpty(status) ? fallbackStatus : status,
            };
        }
        if (root.TryGetProperty("character_id", out _))
        {
            return new CharacterWriteResult
            {
                Character = root.Deserialize<Character>(JsonOpts),
                Status = string.IsNullOrEmpty(status) ? fallbackStatus : status,
            };
        }
        return new CharacterWriteResult { Status = status };
    }

    // ── URL helpers ─────────────────────────────────────────────

    private static string NormalizeBase(string baseUrl)
    {
        var b = baseUrl.TrimEnd('/');
        if (!b.Contains("://")) b = "http://" + b;
        return b;
    }

    private static string Esc(string segment) => Uri.EscapeDataString(segment);

    private static string Query(string path, Dictionary<string, string>? q)
    {
        if (q is not { Count: > 0 }) return path;
        var sb = new StringBuilder(path).Append('?');
        bool first = true;
        foreach (var (k, v) in q)
        {
            if (!first) sb.Append('&');
            sb.Append(k).Append('=').Append(Uri.EscapeDataString(v));
            first = false;
        }
        return sb.ToString();
    }

    // ── Lifecycle ───────────────────────────────────────────────

    public void Dispose()
    {
        if (_ownsHttp) _http.Dispose();
    }

    public ValueTask DisposeAsync()
    {
        Dispose();
        return ValueTask.CompletedTask;
    }
}
