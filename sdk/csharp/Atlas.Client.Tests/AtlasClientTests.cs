using Xunit;

namespace Atlas.Client.Tests;

/// <summary>End-to-end SDK tests against a fake Atlas on a real socket:
/// paths, auth headers, snake_case wire format, error mapping, retry
/// policy, write-reply normalization and the heartbeat loop.</summary>
public sealed class AtlasClientTests
{
    private static AtlasClient Client(string baseUrl,
        string? registryBase = null, string? registryToken = null,
        string? adminKey = null, int maxRetries = 3) =>
        new(new AtlasClientOptions
        {
            BaseUrl = baseUrl,
            RegistryBaseUrl = registryBase,
            RegistryToken = registryToken,
            AdminApiKey = adminKey,
            MaxRetries = maxRetries,
            BaseBackoffMs = 1, // keep retry tests fast
        });

    // ── Registry ────────────────────────────────────────────────

    [Fact]
    public async Task Register_PostsJsonToRegistry()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r is { Method: "POST", Path: "/v1/registry/servers/register" },
            """{"server_id":"game-1","status":"starting"}""");

        using var client = Client(fake.BaseUrl, registryToken: "svc-token");
        var res = await client.RegisterAsync(new RegisterRequest(
            "game-1", "Game 1", "cn-east", new Endpoint { Host = "10.0.0.1", Port = 30001 }, 2000)
        { Type = "game", Version = "1.0.0" });

        Assert.Equal("starting", res.Status);
        var req = fake.Requests.Single();
        Assert.Equal("POST", req.Method);
        Assert.True(req.HasHeader("Authorization", "Bearer svc-token"));
        Assert.True(req.HasHeader("Content-Type", "application/json"));
        Assert.Contains("\"server_id\":\"game-1\"", req.Body);
        Assert.Contains("\"endpoint\":{\"host\":\"10.0.0.1\",\"port\":30001}", req.Body);
        Assert.Contains("\"capacity\":2000", req.Body);
    }

    [Fact]
    public async Task Heartbeat_ReportsLoad()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r is { Method: "POST", Path: "/v1/registry/servers/game-1/heartbeat" },
            """{"server_id":"game-1","status":"online","next_heartbeat_in":10}""");

        using var client = Client(fake.BaseUrl, registryToken: "svc");
        var res = await client.HeartbeatAsync("game-1", new HeartbeatRequest(42, 0.35));

        Assert.Equal("online", res.Status);
        Assert.Equal(10, res.NextHeartbeatIn);
        var req = fake.Requests.Single();
        Assert.Contains("\"players\":42", req.Body);
        Assert.Contains("\"load\":0.35", req.Body);
    }

    [Fact]
    public async Task Unregister_PostsUnregister()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r is { Method: "POST", Path: "/v1/registry/servers/game-1/unregister" },
            """{"server_id":"game-1","status":"offline"}""");

        using var client = Client(fake.BaseUrl, registryToken: "svc");
        var res = await client.UnregisterAsync("game-1");
        Assert.Equal("offline", res.Status);
    }

    // ── Discovery ───────────────────────────────────────────────

    [Fact]
    public async Task ListServers_BuildsQuery()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r is { Method: "GET", Path: "/v1/discovery/servers" },
            """{"servers":[{"id":"game-1","status":"online"}]}""");

        using var client = Client(fake.BaseUrl);
        var list = await client.ListServersAsync(new ServerFilter
        { Region = "cn-east", Status = "online", Limit = 5 });

        Assert.Single(list);
        Assert.Equal("game-1", list[0].Id);
        var req = fake.Requests.Single();
        Assert.Contains("region=cn-east", req.Query);
        Assert.Contains("status=online", req.Query);
        Assert.Contains("limit=5", req.Query);
    }

    [Fact]
    public async Task GetServer_FetchesOne()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r is { Method: "GET", Path: "/v1/discovery/servers/game-1" },
            """{"id":"game-1","status":"online","players":3}""");

        using var client = Client(fake.BaseUrl);
        var s = await client.GetServerAsync("game-1");
        Assert.Equal(3, s.Players);
    }

    [Fact]
    public async Task GetServer_404_ThrowsWithoutRetry()
    {
        using var fake = new FakeAtlas();
        fake.On(_ => true, _ => new FakeAtlas.Reply(404,
            """{"code":"SERVER_NOT_FOUND","message":"no such server"}"""));

        using var client = Client(fake.BaseUrl, maxRetries: 3);
        var ex = await Assert.ThrowsAsync<AtlasError>(
            () => client.GetServerAsync("nope"));

        Assert.Equal(404, ex.Status);
        Assert.Equal("SERVER_NOT_FOUND", ex.Code);
        Assert.Equal(1, fake.RequestCount); // 4xx never retries
    }

    // ── Retry policy ────────────────────────────────────────────

    [Fact]
    public async Task ServerError_IsRetriedUntilSuccess()
    {
        using var fake = new FakeAtlas();
        fake.FailNext(2);
        fake.On(r => r is { Method: "GET", Path: "/v1/discovery/servers/game-1" },
            """{"id":"game-1","status":"online"}""");

        using var client = Client(fake.BaseUrl, maxRetries: 3);
        var s = await client.GetServerAsync("game-1");

        Assert.Equal("online", s.Status);
        Assert.Equal(3, fake.RequestCount); // 2×503 then success
    }

    [Fact]
    public async Task ServerError_ExhaustsRetries()
    {
        using var fake = new FakeAtlas();
        fake.FailNext(10);
        fake.On(_ => true, """{"id":"x"}""");

        using var client = Client(fake.BaseUrl, maxRetries: 3);
        var ex = await Assert.ThrowsAsync<AtlasError>(
            () => client.GetServerAsync("game-1"));

        Assert.Equal(503, ex.Status);
        Assert.Equal("UNAVAILABLE", ex.Code);
        Assert.Equal(4, fake.RequestCount); // 1 + maxRetries
    }

    [Fact]
    public async Task NetworkError_RetriesThenThrows()
    {
        var port = 1; // nothing listens on tcp/1
        using var client = new AtlasClient(new AtlasClientOptions
        {
            BaseUrl = $"http://127.0.0.1:{port}",
            MaxRetries = 3,
            BaseBackoffMs = 1,
        });

        var ex = await Assert.ThrowsAsync<AtlasError>(
            () => client.GetServerAsync("game-1"));

        Assert.Equal(0, ex.Status); // 0 = network-layer failure
        Assert.Equal("NETWORK_ERROR", ex.Code);
    }

    // ── Directory + reply normalization ─────────────────────────

    [Fact]
    public async Task CreateCharacter_FlatReplyBecomesCreated()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r is { Method: "POST", Path: "/v1/directory/characters" },
            """{"account_id":42,"server_id":"game-1","character_id":1001,"name":"Hero","level":5}""");

        using var client = Client(fake.BaseUrl);
        var res = await client.CreateCharacterAsync(
            new CreateCharacterRequest(42, "game-1", 1001, "Hero"));

        Assert.Equal("created", res.Status); // flat reply carries no status
        Assert.NotNull(res.Character);
        Assert.Equal("Hero", res.Character!.Name);
    }

    [Fact]
    public async Task CreateCharacter_NestedReplyKeepsStatus()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r is { Method: "POST", Path: "/v1/directory/characters" },
            """{"character":{"account_id":42,"server_id":"game-1","character_id":1001,"name":"Hero"},"status":"created"}""");

        using var client = Client(fake.BaseUrl);
        var res = await client.CreateCharacterAsync(
            new CreateCharacterRequest(42, "game-1", 1001, "Hero"));

        Assert.Equal("created", res.Status);
        Assert.NotNull(res.Character);
    }

    [Fact]
    public async Task UpdateCharacter_FlatReplyBecomesUpdated()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r is { Method: "PATCH", Path: "/v1/directory/characters/1001" },
            """{"account_id":42,"server_id":"game-1","character_id":1001,"name":"HeroX"}""");

        using var client = Client(fake.BaseUrl);
        var res = await client.UpdateCharacterAsync(1001, new UpdateCharacterRequest { Name = "HeroX" });

        Assert.Equal("updated", res.Status);
        Assert.Equal("HeroX", res.Character!.Name);
    }

    [Fact]
    public async Task DeleteCharacter_QueuedReplyHasNullCharacter()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r is { Method: "DELETE", Path: "/v1/directory/characters/1001" },
            """{"status":"queued"}""");

        using var client = Client(fake.BaseUrl);
        var res = await client.DeleteCharacterAsync(1001);

        Assert.Equal("queued", res.Status);
        Assert.Null(res.Character);
    }

    [Fact]
    public async Task ListByAccount_NullCharactersBecomesEmpty()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r is { Method: "GET", Path: "/v1/directory/accounts/42/characters" },
            """{"characters":null}"""); // Atlas may emit explicit nulls

        using var client = Client(fake.BaseUrl);
        var list = await client.ListCharactersByAccountAsync(42);
        Assert.NotNull(list);
        Assert.Empty(list);
    }

    [Fact]
    public async Task ListByServer_BuildsQueryAndNormalizesPage()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r is { Method: "GET", Path: "/v1/directory/servers/game-1/characters" },
            """{"characters":null,"next_cursor":"c2"}""");

        using var client = Client(fake.BaseUrl);
        var page = await client.ListCharactersByServerAsync("game-1", 10, "c1");

        Assert.Empty(page.Characters);
        Assert.Equal("c2", page.NextCursor);
        var req = fake.Requests.Single();
        Assert.Contains("limit=10", req.Query);
        Assert.Contains("cursor=c1", req.Query);
    }

    // ── Routing ─────────────────────────────────────────────────

    [Fact]
    public async Task Recommend_BuildsQuery()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r is { Method: "GET", Path: "/v1/routing/recommended" },
            """{"server":{"id":"game-1","status":"online"},"reason":"lowest_load"}""");

        using var client = Client(fake.BaseUrl);
        var rec = await client.RecommendAsync(42, "cn-east", "1.0.0");

        Assert.Equal("game-1", rec.Server.Id);
        Assert.Equal("lowest_load", rec.Reason);
        var req = fake.Requests.Single();
        Assert.Contains("account_id=42", req.Query);
        Assert.Contains("region=cn-east", req.Query);
        Assert.Contains("version=1.0.0", req.Query);
    }

    // ── Admin ───────────────────────────────────────────────────

    [Fact]
    public async Task AdminLifecycle_SendsBearerKey()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r is { Method: "POST", Path: "/v1/admin/servers/game-1/maintenance" },
            """{"server_id":"game-1","status":"maintenance"}""");

        using var client = Client(fake.BaseUrl, adminKey: "admin-key");
        var res = await client.SetMaintenanceAsync("game-1");

        Assert.Equal("maintenance", res.Status);
        Assert.True(fake.Requests.Single().HasHeader("Authorization", "Bearer admin-key"));
    }

    [Fact]
    public async Task Stats_UsesAdminAuth()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r is { Method: "GET", Path: "/v1/admin/stats" },
            """{"total_servers":2,"servers_by_status":{"online":2},"total_players":10,"total_capacity":100}""");

        using var client = Client(fake.BaseUrl, adminKey: "admin-key");
        var stats = await client.StatsAsync();

        Assert.Equal(2, stats.TotalServers);
        Assert.Equal(2, stats.ServersByStatus["online"]);
    }

    [Fact]
    public async Task SearchCharacters_UsesAdminPath()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r is { Method: "GET", Path: "/v1/admin/characters/search" },
            """{"characters":[{"account_id":1,"server_id":"game-1","character_id":11,"name":"A"}],"next_cursor":""}""");

        using var client = Client(fake.BaseUrl, adminKey: "admin-key");
        var page = await client.SearchCharactersAsync(new CharacterFilter
        { ServerId = "game-1", MinLevel = 10, MaxLevel = 50, Limit = 20 });

        Assert.Single(page.Characters);
        var req = fake.Requests.Single();
        Assert.Contains("server_id=game-1", req.Query);
        Assert.Contains("min_level=10", req.Query);
        Assert.Contains("max_level=50", req.Query);
        Assert.Contains("limit=20", req.Query);
    }

    [Fact]
    public async Task Migrations_CreateGetListRollback()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r is { Method: "POST", Path: "/v1/admin/migrations" },
            """{"id":"m1","source_servers":["game-1"],"target_server":"game-2","status":"pending","started_at":"2026-01-01T00:00:00Z"}""");
        fake.On(r => r is { Method: "GET", Path: "/v1/admin/migrations/m1" },
            """{"id":"m1","source_servers":["game-1"],"target_server":"game-2","status":"running","started_at":"2026-01-01T00:00:00Z"}""");
        fake.On(r => r is { Method: "GET", Path: "/v1/admin/migrations" },
            """{"migrations":[{"id":"m1","target_server":"game-2","status":"done","started_at":"2026-01-01T00:00:00Z"}]}""");
        fake.On(r => r is { Method: "POST", Path: "/v1/admin/migrations/m1/rollback" },
            """{"id":"m1","target_server":"game-1","status":"rolling_back","started_at":"2026-01-01T00:00:00Z"}""");

        using var client = Client(fake.BaseUrl, adminKey: "admin-key");

        var created = await client.CreateMigrationAsync(
            new CreateMigrationRequest(["game-1"], "game-2"));
        Assert.Equal("m1", created.Id);
        Assert.Equal(["game-1"], created.SourceServers);

        var got = await client.GetMigrationAsync("m1");
        Assert.Equal("running", got.Status);

        var list = await client.ListMigrationsAsync(10);
        Assert.Single(list);

        var rolled = await client.RollbackMigrationAsync("m1");
        Assert.Equal("rolling_back", rolled.Status);
    }

    // ── Port split ──────────────────────────────────────────────

    [Fact]
    public async Task RegistryBaseUrl_SplitsRegistryCalls()
    {
        using var pub = new FakeAtlas();
        using var reg = new FakeAtlas();
        pub.On(_ => true, """{"id":"from-public"}""");
        reg.On(r => r is { Method: "POST", Path: "/v1/registry/servers/register" },
            """{"server_id":"game-1","status":"starting"}""");

        using var client = Client(pub.BaseUrl, registryBase: reg.BaseUrl, registryToken: "svc");
        await client.RegisterAsync(new RegisterRequest("game-1", "n", "r", new Endpoint(), 1));
        await client.GetServerAsync("game-1");

        // Registry call went to the registry base, discovery to the public one.
        Assert.Equal("/v1/registry/servers/register", reg.Requests.Single().Path);
        Assert.Equal("/v1/discovery/servers/game-1", pub.Requests.Single().Path);
        Assert.True(reg.Requests.Single().HasHeader("Authorization", "Bearer svc"));
    }

    // ── Auto heartbeat ──────────────────────────────────────────

    [Fact]
    public async Task AutoHeartbeat_BeatsImmediatelyThenOnInterval()
    {
        using var fake = new FakeAtlas();
        fake.On(r => r.Path.EndsWith("/heartbeat"),
            """{"server_id":"game-1","status":"online","next_heartbeat_in":1}""");

        using var client = Client(fake.BaseUrl);
        var loop = client.StartHeartbeat("game-1", TimeSpan.FromMilliseconds(60),
            new HeartbeatRequest(1, 0.1));

        await Task.Delay(400);
        loop.Set(99, 0.9); // later beats must carry the updated payload
        await Task.Delay(200);
        loop.Stop();
        var countAfterStop = fake.RequestCount;
        await Task.Delay(150);

        Assert.True(countAfterStop >= 2, $"expected ≥2 beats, got {countAfterStop}");
        Assert.Equal(countAfterStop, fake.RequestCount); // stopped means stopped
        Assert.Contains("\"players\":99", fake.Requests.Last().Body);
    }

    [Fact]
    public async Task AutoHeartbeat_OnErrorReceivesFailures()
    {
        var fake = new FakeAtlas();
        fake.On(r => r.Path.EndsWith("/heartbeat"),
            """{"server_id":"game-1","status":"online","next_heartbeat_in":1}""");

        var client = Client(fake.BaseUrl);
        var loop = client.StartHeartbeat("game-1", TimeSpan.FromMilliseconds(50),
            new HeartbeatRequest());

        var errTcs = new TaskCompletionSource<AtlasError>(TaskCreationOptions.RunContinuationsAsynchronously);
        loop.OnError(e => errTcs.TrySetResult(e));

        await Task.Delay(120); // let the first (successful) beat land
        fake.Dispose();        // then pull the server out → next beats fail

        var err = await errTcs.Task.WaitAsync(TimeSpan.FromSeconds(5));
        Assert.Equal(0, err.Status); // network-layer failure
        loop.Stop();
        client.Dispose();
    }
}
