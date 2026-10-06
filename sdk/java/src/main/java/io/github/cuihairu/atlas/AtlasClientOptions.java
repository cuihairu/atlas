package io.github.cuihairu.atlas;

import java.util.Map;

/** Client options with fluent setters; zero values fall back to the
 * same defaults as the Go/C++/Python/JS SDKs. */
public class AtlasClientOptions {

    private String baseUrl = "http://localhost:8080";
    private String registryBaseUrl;
    private String registryToken = "";
    private String adminApiKey = "";
    private Map<String, String> defaultHeaders = Map.of();
    private long timeoutMs = 10_000;
    private int maxRetries = 3;
    private long baseBackoffMs = 100;

    public String getBaseUrl() {
        return baseUrl;
    }

    /** Public + Admin base. */
    public AtlasClientOptions setBaseUrl(String baseUrl) {
        this.baseUrl = baseUrl;
        return this;
    }

    public String getRegistryBaseUrl() {
        return registryBaseUrl;
    }

    /** Registry split port override (ATLAS_REGISTRY_ADDR, default :8081). */
    public AtlasClientOptions setRegistryBaseUrl(String registryBaseUrl) {
        this.registryBaseUrl = registryBaseUrl;
        return this;
    }

    public String getRegistryToken() {
        return registryToken;
    }

    /** Bearer token for the Registry scope. */
    public AtlasClientOptions setRegistryToken(String registryToken) {
        this.registryToken = registryToken;
        return this;
    }

    public String getAdminApiKey() {
        return adminApiKey;
    }

    /** API key for the Admin scope. */
    public AtlasClientOptions setAdminApiKey(String adminApiKey) {
        this.adminApiKey = adminApiKey;
        return this;
    }

    public Map<String, String> getDefaultHeaders() {
        return defaultHeaders;
    }

    /** Static headers sent on every call (docs/api.md 请求追踪) — e.g. a
     * process-level "X-Request-ID" correlation id. Copied defensively. */
    public AtlasClientOptions setDefaultHeaders(Map<String, String> defaultHeaders) {
        this.defaultHeaders = defaultHeaders == null ? Map.of() : Map.copyOf(defaultHeaders);
        return this;
    }

    public long getTimeoutMs() {
        return timeoutMs;
    }

    /** Per-attempt timeout in ms (default 10000; 0 disables). */
    public AtlasClientOptions setTimeoutMs(long timeoutMs) {
        this.timeoutMs = timeoutMs;
        return this;
    }

    public int getMaxRetries() {
        return maxRetries;
    }

    /** Retries for transient failures — network errors and 5xx (default 3). */
    public AtlasClientOptions setMaxRetries(int maxRetries) {
        this.maxRetries = maxRetries;
        return this;
    }

    public long getBaseBackoffMs() {
        return baseBackoffMs;
    }

    /** Delay ceiling before the first retry; doubles each attempt with
     * full jitter, capped at 10s (default 100ms). */
    public AtlasClientOptions setBaseBackoffMs(long baseBackoffMs) {
        this.baseBackoffMs = baseBackoffMs;
        return this;
    }
}
