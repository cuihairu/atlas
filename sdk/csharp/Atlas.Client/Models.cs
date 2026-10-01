namespace Atlas;

// Wire models mirroring sdk/go/atlas/types.go. The transport speaks
// snake_case (JsonNamingPolicy.SnakeCaseLower); C# surface stays PascalCase.

/// <summary>Endpoint is a game server's connect address.</summary>
public sealed class Endpoint
{
    public string Host { get; set; } = "";
    public int Port { get; set; }
}

/// <summary>Server is a registered server as returned by Discovery.</summary>
public sealed class Server
{
    public string Id { get; set; } = "";
    public string Name { get; set; } = "";
    public string Type { get; set; } = "";
    public string Region { get; set; } = "";
    public string? RealmId { get; set; }
    public string? ShardId { get; set; }
    public string Version { get; set; } = "";
    public string Platform { get; set; } = "";
    public Endpoint Endpoint { get; set; } = new();
    public int Capacity { get; set; }
    public Dictionary<string, string>? Metadata { get; set; }
    public string Status { get; set; } = "";
    public int Players { get; set; }
    public double Load { get; set; }
    public DateTimeOffset? LastSeenAt { get; set; }
    public DateTimeOffset CreatedAt { get; set; }
    public DateTimeOffset UpdatedAt { get; set; }
}

/// <summary>ServerFilter narrows ListServers. Empty/0 fields are not sent.</summary>
public sealed class ServerFilter
{
    public string? Region { get; set; }
    public string? Version { get; set; }
    public string? Platform { get; set; }
    public string? Status { get; set; }
    public int Limit { get; set; }
}

/// <summary>RegisterRequest describes a server joining the fleet.</summary>
public sealed class RegisterRequest
{
    public RegisterRequest() { }

    public RegisterRequest(string serverId, string name, string region,
        Endpoint endpoint, int capacity)
    {
        ServerId = serverId;
        Name = name;
        Region = region;
        Endpoint = endpoint;
        Capacity = capacity;
    }

    public string ServerId { get; set; } = "";
    public string Name { get; set; } = "";
    public string Type { get; set; } = "";
    public string Region { get; set; } = "";
    public string? RealmId { get; set; }
    public string? ShardId { get; set; }
    public string Version { get; set; } = "";
    public string Platform { get; set; } = "";
    public Endpoint Endpoint { get; set; } = new();
    public int Capacity { get; set; }
}

/// <summary>RegisterResult is the response to Register.</summary>
public sealed class RegisterResult
{
    public string ServerId { get; set; } = "";
    public string Status { get; set; } = "";
}

/// <summary>HeartbeatRequest carries live load metadata.</summary>
public sealed class HeartbeatRequest
{
    public HeartbeatRequest() { }

    public HeartbeatRequest(int players, double load)
    {
        Players = players;
        Load = load;
    }

    public int Players { get; set; }
    public double Load { get; set; }
}

/// <summary>HeartbeatResult is the response to Heartbeat.</summary>
public sealed class HeartbeatResult
{
    public string ServerId { get; set; } = "";
    public string Status { get; set; } = "";
    public int NextHeartbeatIn { get; set; }
}

/// <summary>Character is a directory index entry.</summary>
public sealed class Character
{
    public long AccountId { get; set; }
    public string ServerId { get; set; } = "";
    public long CharacterId { get; set; }
    public string Name { get; set; } = "";
    public int Level { get; set; }
    public int ClassId { get; set; }
    public string? Avatar { get; set; }
    public Dictionary<string, string>? Metadata { get; set; }
    public DateTimeOffset? LastLoginAt { get; set; }
    public DateTimeOffset CreatedAt { get; set; }
    public DateTimeOffset UpdatedAt { get; set; }
}

/// <summary>CreateCharacterRequest registers a new character on a server.</summary>
public sealed class CreateCharacterRequest
{
    public CreateCharacterRequest() { }

    public CreateCharacterRequest(long accountId, string serverId,
        long characterId, string name)
    {
        AccountId = accountId;
        ServerId = serverId;
        CharacterId = characterId;
        Name = name;
    }

    public long AccountId { get; set; }
    public string ServerId { get; set; } = "";
    public long CharacterId { get; set; }
    public string Name { get; set; } = "";
    public int Level { get; set; }
    public int ClassId { get; set; }
    public string? Avatar { get; set; }
}

/// <summary>UpdateCharacterRequest patches a character; null fields are left
/// unchanged (PATCH semantics).</summary>
public sealed class UpdateCharacterRequest
{
    public string? Name { get; set; }
    public int? Level { get; set; }
    public int? ClassId { get; set; }
    public string? Avatar { get; set; }
}

/// <summary>CharacterWriteResult is the response of a directory write.
/// Character is null when Status is "queued" (asynchronous event adapter).</summary>
public sealed class CharacterWriteResult
{
    public Character? Character { get; set; }
    public string Status { get; set; } = ""; // created | updated | deleted | queued
}

/// <summary>CharacterPage is a cursor-paginated character listing.</summary>
public sealed class CharacterPage
{
    public List<Character> Characters { get; set; } = [];
    public string NextCursor { get; set; } = "";
}

/// <summary>CharacterFilter narrows SearchCharacters (Admin API). Null/empty
/// fields are not sent.</summary>
public sealed class CharacterFilter
{
    public string? Name { get; set; }
    public string? ServerId { get; set; }
    public int? ClassId { get; set; }
    public int? MinLevel { get; set; }
    public int? MaxLevel { get; set; }
    public int Limit { get; set; }
    public string? Cursor { get; set; }
}

/// <summary>Recommendation is the Routing decision for an account.</summary>
public sealed class Recommendation
{
    public Server Server { get; set; } = new();
    public string Reason { get; set; } = ""; // lowest_load | highest_capacity | has_character | fallback
}

/// <summary>Stats is the Admin fleet overview.</summary>
public sealed class Stats
{
    public int TotalServers { get; set; }
    public Dictionary<string, int> ServersByStatus { get; set; } = [];
    public Dictionary<string, int> ServersByRegion { get; set; } = [];
    public Dictionary<string, int> ServersByVersion { get; set; } = [];
    public int TotalPlayers { get; set; }
    public int TotalCapacity { get; set; }
    public int TotalCharacters { get; set; }
}

/// <summary>Migration is a character migration job.</summary>
public sealed class Migration
{
    public string Id { get; set; } = "";
    public List<string> SourceServers { get; set; } = [];
    public string TargetServer { get; set; } = "";
    public string Status { get; set; } = "";
    public DateTimeOffset StartedAt { get; set; }
    public DateTimeOffset? CompletedAt { get; set; }
}

/// <summary>CreateMigrationRequest starts a migration from source servers to one target.</summary>
public sealed class CreateMigrationRequest
{
    public CreateMigrationRequest() { }

    public CreateMigrationRequest(List<string> sourceServers, string targetServer)
    {
        SourceServers = sourceServers;
        TargetServer = targetServer;
    }

    public List<string> SourceServers { get; set; } = [];
    public string TargetServer { get; set; } = "";
}

/// <summary>StatusResult is the common {"server_id","status"} reply.</summary>
public sealed class StatusResult
{
    public string ServerId { get; set; } = "";
    public string Status { get; set; } = "";
}
