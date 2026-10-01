// Atlas C++ SDK — HTTP client (TODO v0.1.7).
//
// Same API surface as the Go SDK (sdk/go/atlas) over the REST transport.
// Vendored single headers: cpp-httplib v0.58.0, nlohmann/json v3.12.0
// (sdk/cpp/third_party/, see VENDORED.md).
#pragma once

#include <atomic>
#include <chrono>
#include <functional>
#include <memory>
#include <mutex>
#include <optional>
#include <stdexcept>
#include <string>
#include <thread>

#include "atlas/types.hpp"

namespace atlas {

/// API error: HTTP status + Atlas error code ("SERVER_NOT_FOUND", ...).
class Error : public std::runtime_error {
public:
    Error(int status, std::string code, std::string message)
        : std::runtime_error(code + ": " + message), status_(status), code_(std::move(code)),
          message_(std::move(message)) {}

    int status() const noexcept { return status_; }
    const std::string& code() const noexcept { return code_; }
    const std::string& message() const noexcept { return message_; }

private:
    int status_;
    std::string code_;
    std::string message_;
};

struct Options {
    /// REST base, e.g. "http://atlas:8080" (public + admin domains).
    std::string base_url = "http://localhost:8080";
    /// Registry base override. Atlas splits the Registry API onto its own
    /// port (ATLAS_REGISTRY_ADDR, default :8081); point this there when
    /// calling it directly. Empty = base_url (merged behind a proxy).
    std::string registry_base_url;
    /// Registry domain bearer (ATLAS_REGISTRY_TOKENS counterpart).
    std::string registry_token;
    /// Admin domain bearer (ATLAS_ADMIN_API_KEYS counterpart).
    std::string admin_api_key;
    /// Retries for transient failures (network errors + 5xx).
    int max_retries = 3;
    /// Backoff ceiling growth base; each attempt doubles it with full jitter.
    std::chrono::milliseconds base_backoff{100};
    /// Per-request HTTP timeout.
    std::chrono::milliseconds timeout{10'000};
};

/// Atlas REST client. Methods throw atlas::Error on failure; safe for
/// concurrent use (one cpp-httplib client per call).
class Client {
public:
    explicit Client(Options opts = {});
    ~Client();

    Client(const Client&) = delete;
    Client& operator=(const Client&) = delete;

    // ── Registry ──
    RegisterResult Register(const RegisterRequest& req);
    HeartbeatResult Heartbeat(const std::string& server_id, const HeartbeatRequest& req);
    StatusResult Unregister(const std::string& server_id);

    // ── Discovery ──
    std::vector<Server> ListServers(const ServerFilter& filter = {});
    Server GetServer(const std::string& server_id);

    // ── Directory ──
    CharacterWriteResult CreateCharacter(const CreateCharacterRequest& req);
    Character GetCharacter(std::int64_t character_id);
    std::vector<Character> ListCharactersByAccount(std::int64_t account_id);
    CharacterPage ListCharactersByServer(const std::string& server_id, int limit = 0,
                                         const std::string& cursor = "");
    CharacterWriteResult UpdateCharacter(std::int64_t character_id, const UpdateCharacterRequest& req);
    CharacterWriteResult DeleteCharacter(std::int64_t character_id);

    // ── Routing ──
    Recommendation Recommend(std::int64_t account_id, const std::string& region = {},
                             const std::string& version = {}, const std::string& platform = {});

    // ── Admin ──
    StatusResult SetMaintenance(const std::string& server_id);
    StatusResult SetDrain(const std::string& server_id);
    StatusResult Enable(const std::string& server_id);
    StatusResult Disable(const std::string& server_id);
    Stats GetStats();
    CharacterPage SearchCharacters(const CharacterFilter& filter);
    Migration CreateMigration(const CreateMigrationRequest& req);
    Migration GetMigration(const std::string& id);
    std::vector<Migration> ListMigrations(int limit = 0);
    Migration RollbackMigration(const std::string& id);

private:
    struct Impl;
    std::unique_ptr<Impl> impl_;
};

/// Auto-heartbeat loop: sends once immediately, then every interval.
/// Update the payload from the game thread with Set(). Stop() joins the
/// thread (also called by the destructor).
class AutoHeartbeat {
public:
    AutoHeartbeat(Client& client, std::string server_id, std::chrono::milliseconds interval,
                  HeartbeatRequest initial = {});
    ~AutoHeartbeat();

    AutoHeartbeat(const AutoHeartbeat&) = delete;
    AutoHeartbeat& operator=(const AutoHeartbeat&) = delete;

    /// Thread-safe payload update for the next tick.
    void Set(int players, double load);
    /// Callback invoked on heartbeat failure (from the heartbeat thread).
    void OnError(std::function<void(const Error&)> cb);
    /// Stops the loop and joins the thread. Idempotent.
    void Stop();

private:
    void Run();

    Client& client_;
    std::string server_id_;
    std::chrono::milliseconds interval_;
    HeartbeatRequest payload_;
    std::function<void(const Error&)> on_error_;

    std::mutex mu_;
    std::atomic<bool> stopped_{false};
    std::thread thread_;
};

} // namespace atlas
