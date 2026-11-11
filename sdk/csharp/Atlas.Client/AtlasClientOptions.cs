namespace Atlas;

/// <summary>Client configuration (object-initializer style).</summary>
public sealed class AtlasClientOptions
{
    /// <summary>Public base URL ("http://atlas:8080"). Also serves Admin;
    /// a bare "host:port" gets "http://" prepended.</summary>
    public string BaseUrl { get; set; } = "http://localhost:8080";

    /// <summary>Overrides the REST base for Registry calls only. Atlas
    /// splits the Registry API onto its own port (:8081); point this there
    /// when calling it directly. Null = BaseUrl (merged behind a proxy).</summary>
    public string? RegistryBaseUrl { get; set; }

    /// <summary>RegistryToken authenticates Registry calls
    /// (ATLAS_REGISTRY_TOKENS counterpart, sent as Bearer).</summary>
    public string? RegistryToken { get; set; }

    /// <summary>AdminApiKey authenticates Admin calls (ATLAS_ADMIN_API_KEYS
    /// counterpart, sent as Bearer).</summary>
    public string? AdminApiKey { get; set; }

    /// <summary>Static headers sent on every call (docs/api.md 请求追踪) —
    /// e.g. a process-level "X-Request-ID" correlation id. Applied after
    /// auth, keys verbatim.</summary>
    public Dictionary<string, string>? DefaultHeaders { get; set; }

    /// <summary>Per-attempt HTTP timeout in milliseconds. 0 disables it.</summary>
    public int TimeoutMs { get; set; } = 10_000;

    /// <summary>Bounded retries for transient failures (network errors and
    /// 5xx). 4xx never retries.</summary>
    public int MaxRetries { get; set; } = 3;

    /// <summary>Backoff ceiling before the first retry, milliseconds; each
    /// attempt doubles it with full jitter (capped at 10s).</summary>
    public int BaseBackoffMs { get; set; } = 100;

    /// <summary>Advanced: reuse an externally managed HttpClient. When null
    /// the client constructs (and owns) one with the configured timeout.</summary>
    public HttpClient? HttpInvoker { get; set; }
}
