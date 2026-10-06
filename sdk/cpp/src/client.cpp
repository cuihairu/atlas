#include "atlas/client.hpp"

#include <algorithm>
#include <cctype>
#include <random>

#include <httplib.h>
#include <nlohmann/json.hpp>

namespace atlas {

namespace {

using json = nlohmann::json;

std::string UrlEscape(const std::string& s) {
    // httplib already percent-encodes path segments it is given via
    // set_path_escape... we escape manually to keep IDs safe.
    static const char* hex = "0123456789ABCDEF";
    std::string out;
    out.reserve(s.size());
    for (unsigned char c : s) {
        if (std::isalnum(c) || c == '-' || c == '_' || c == '.' || c == '~') {
            out += static_cast<char>(c);
        } else {
            out += '%';
            out += hex[c >> 4];
            out += hex[c & 0xF];
        }
    }
    return out;
}

int JitterDelay(std::chrono::milliseconds ceiling) {
    static thread_local std::mt19937_64 rng{std::random_device{}()};
    if (ceiling.count() <= 0) return 0;
    return static_cast<int>(rng() % static_cast<std::uint64_t>(ceiling.count() + 1));
}

std::chrono::milliseconds BackoffCeiling(std::chrono::milliseconds base, int attempt) {
    // Full jitter over base<<attempt, capped at 10s.
    auto ms = base.count();
    for (int i = 0; i < attempt && ms < 10'000; ++i) ms *= 2;
    return std::chrono::milliseconds{std::min<std::chrono::milliseconds::rep>(ms, 10'000)};
}

void AppendQuery(std::string& path, const std::string& key, const std::string& value) {
    if (value.empty()) return;
    path += (path.find('?') == std::string::npos ? '?' : '&');
    path += key + '=' + UrlEscape(value);
}

void AppendQuery(std::string& path, const std::string& key, int value) {
    if (value <= 0) return;
    AppendQuery(path, key, std::to_string(value));
}

StatusResult ParseStatus(const json& j) {
    StatusResult out;
    if (j.contains("server_id") && !j["server_id"].is_null()) out.server_id = j["server_id"];
    out.status = j.value("status", "");
    return out;
}

// Directory write replies come in three shapes (docs/api.md): nested
// {"character":...,"status":...} (gRPC-style payloads), the flat
// character object Atlas returns synchronously over REST, and
// {"status":"queued"|"deleted"} (async adapter / delete).
CharacterWriteResult ParseCharacterWrite(const json& body, const char* sync_status) {
    CharacterWriteResult out;
    if (body.contains("character") && body["character"].is_object()) {
        out.character = body["character"].get<Character>();
        out.status = body.value("status", sync_status);
    } else if (body.contains("character_id")) {
        out.character = body.get<Character>();
        out.status = sync_status;
    } else {
        out.status = body.value("status", "");
    }
    return out;
}

CharacterPage ParseCharacterPage(const json& j) {
    CharacterPage page;
    if (j.contains("characters") && j["characters"].is_array()) j["characters"].get_to(page.characters);
    page.next_cursor = j.value("next_cursor", "");
    return page;
}

} // namespace

struct Client::Impl {
    Options opts;

    /// Effective base for a domain: registry override or the main base.
    std::string BaseFor(bool registry_domain) const {
        return registry_domain && !opts.registry_base_url.empty() ? opts.registry_base_url
                                                                  : opts.base_url;
    }

    // Do performs one HTTP round trip; DoWithRetry wraps it with the
    // transient-failure backoff loop. Both throw atlas::Error.
    json Do(const std::string& base_url, const std::string& method, const std::string& path,
            const json* body, const std::string& bearer);

    json DoWithRetry(bool registry_domain, const std::string& method, const std::string& path,
                     const json* body, const std::string& bearer);
};

Client::Client(Options opts) : impl_(std::make_unique<Impl>()) { impl_->opts = std::move(opts); }
Client::~Client() = default;

json Client::Impl::DoWithRetry(bool registry_domain, const std::string& method,
                               const std::string& path, const json* body,
                               const std::string& bearer) {
    int attempt = 0;
    while (true) {
        try {
            return Do(BaseFor(registry_domain), method, path, body, bearer);
        } catch (const Error& e) {
            bool transient = e.status() >= 500;
            if (transient && attempt < opts.max_retries) {
                std::this_thread::sleep_for(
                    std::chrono::milliseconds{JitterDelay(BackoffCeiling(opts.base_backoff, attempt))});
                ++attempt;
                continue;
            }
            throw;
        } catch (const httplib::Error&) {
            // Network-level failure (connection refused, timeout, ...).
            if (attempt < opts.max_retries) {
                std::this_thread::sleep_for(
                    std::chrono::milliseconds{JitterDelay(BackoffCeiling(opts.base_backoff, attempt))});
                ++attempt;
                continue;
            }
            throw Error(0, "NETWORK", "request failed after retries: " + method + " " + path);
        }
    }
}

json Client::Impl::Do(const std::string& base_url, const std::string& method,
                      const std::string& path, const json* body, const std::string& bearer) {
    httplib::Client cli(base_url);
    cli.set_connection_timeout(std::chrono::milliseconds{opts.timeout});
    cli.set_read_timeout(std::chrono::milliseconds{opts.timeout});
    cli.set_write_timeout(std::chrono::milliseconds{opts.timeout});

    httplib::Headers headers;
    if (!bearer.empty()) headers = httplib::Headers{{"Authorization", "Bearer " + bearer}};
    for (const auto& [key, value] : opts.default_headers) {
        headers.erase(key);
        headers.emplace(key, value);
    }

    std::string payload = body ? body->dump() : "";
    httplib::Result res = [&] {
        if (method == "GET") return cli.Get(path, headers);
        if (method == "POST") return cli.Post(path, headers, payload, "application/json");
        if (method == "PATCH") return cli.Patch(path, headers, payload, "application/json");
        if (method == "DELETE") return cli.Delete(path, headers);
        throw Error(0, "INTERNAL", "unsupported method " + method);
    }();

    if (!res) throw httplib::Error{}; // transport failure, retried above
    if (res->status >= 400) {
        std::string code = "HTTP_" + std::to_string(res->status);
        std::string message = res->body.empty() ? "HTTP " + std::to_string(res->status) : res->body;
        try {
            json err = json::parse(res->body);
            if (err.contains("error")) {
                code = err["error"].value("code", code);
                message = err["error"].value("message", message);
            }
        } catch (const json::parse_error&) {
            // keep fallback code/message
        }
        throw Error(res->status, code, message);
    }
    if (res->body.empty()) return json::object();
    try {
        return json::parse(res->body);
    } catch (const json::exception& e) {
        throw Error(res->status, "PROTOCOL", std::string("invalid JSON response: ") + e.what());
    }
}

// ── Registry ────────────────────────────────────────────────

RegisterResult Client::Register(const RegisterRequest& req) {
    const json payload = req.ToJSON();
    json body = impl_->DoWithRetry(
        true, "POST", "/v1/registry/servers/register", &payload, impl_->opts.registry_token);
    return RegisterResult{body.value("server_id", ""), body.value("status", "")};
}

HeartbeatResult Client::Heartbeat(const std::string& server_id, const HeartbeatRequest& req) {
    const json payload = req.ToJSON();
    json body = impl_->DoWithRetry(true, "POST", "/v1/registry/servers/" + UrlEscape(server_id) + "/heartbeat",
                                   &payload, impl_->opts.registry_token);
    HeartbeatResult out;
    out.server_id = body.value("server_id", server_id);
    out.status = body.value("status", "");
    out.next_heartbeat_in = body.value("next_heartbeat_in", 0);
    return out;
}

StatusResult Client::Unregister(const std::string& server_id) {
    json body = impl_->DoWithRetry(true, "POST", "/v1/registry/servers/" + UrlEscape(server_id) + "/unregister",
                                   nullptr, impl_->opts.registry_token);
    return ParseStatus(body);
}

// ── Discovery ───────────────────────────────────────────────

std::vector<Server> Client::ListServers(const ServerFilter& filter) {
    std::string path = "/v1/discovery/servers";
    if (filter.region) AppendQuery(path, "region", *filter.region);
    if (filter.version) AppendQuery(path, "version", *filter.version);
    if (filter.platform) AppendQuery(path, "platform", *filter.platform);
    if (filter.status) AppendQuery(path, "status", *filter.status);
    if (filter.limit && *filter.limit > 0) AppendQuery(path, "limit", *filter.limit);

    json body = impl_->DoWithRetry(false, "GET", path, nullptr, "");
    std::vector<Server> servers;
    if (body.contains("servers") && body["servers"].is_array()) body["servers"].get_to(servers);
    return servers;
}

Server Client::GetServer(const std::string& server_id) {
    json body = impl_->DoWithRetry(false, "GET", "/v1/discovery/servers/" + UrlEscape(server_id), nullptr, "");
    return body.get<Server>();
}

// ── Directory ───────────────────────────────────────────────

CharacterWriteResult Client::CreateCharacter(const CreateCharacterRequest& req) {
    const json payload = req.ToJSON();
    json body = impl_->DoWithRetry(false, "POST", "/v1/directory/characters", &payload, "");
    return ParseCharacterWrite(body, "created");
}

Character Client::GetCharacter(std::int64_t character_id) {
    json body = impl_->DoWithRetry(false, "GET", "/v1/directory/characters/" + std::to_string(character_id),
                                   nullptr, "");
    return body.get<Character>();
}

std::vector<Character> Client::ListCharactersByAccount(std::int64_t account_id) {
    json body = impl_->DoWithRetry(false, "GET", "/v1/directory/accounts/" + std::to_string(account_id) + "/characters",
                                   nullptr, "");
    std::vector<Character> chars;
    if (body.contains("characters") && body["characters"].is_array()) body["characters"].get_to(chars);
    return chars;
}

CharacterPage Client::ListCharactersByServer(const std::string& server_id, int limit,
                                             const std::string& cursor) {
    std::string path = "/v1/directory/servers/" + UrlEscape(server_id) + "/characters";
    AppendQuery(path, "limit", limit);
    AppendQuery(path, "cursor", cursor);
    json body = impl_->DoWithRetry(false, "GET", path, nullptr, "");
    return ParseCharacterPage(body);
}

CharacterWriteResult Client::UpdateCharacter(std::int64_t character_id,
                                             const UpdateCharacterRequest& req) {
    const json payload = req.ToJSON();
    json body = impl_->DoWithRetry(false, "PATCH", "/v1/directory/characters/" + std::to_string(character_id),
                                   &payload, "");
    return ParseCharacterWrite(body, "updated");
}

CharacterWriteResult Client::DeleteCharacter(std::int64_t character_id) {
    json body = impl_->DoWithRetry(false, "DELETE", "/v1/directory/characters/" + std::to_string(character_id),
                                   nullptr, "");
    CharacterWriteResult out;
    out.status = body.value("status", "deleted");
    return out;
}

// ── Routing ─────────────────────────────────────────────────

Recommendation Client::Recommend(std::int64_t account_id, const std::string& region,
                                 const std::string& version, const std::string& platform) {
    std::string path = "/v1/routing/recommended";
    if (account_id > 0) AppendQuery(path, "account_id", std::to_string(account_id));
    AppendQuery(path, "region", region);
    AppendQuery(path, "version", version);
    AppendQuery(path, "platform", platform);
    json body = impl_->DoWithRetry(false, "GET", path, nullptr, "");
    Recommendation out;
    if (body.contains("server")) out.server = body["server"].get<Server>();
    out.reason = body.value("reason", "");
    return out;
}

// ── Admin ───────────────────────────────────────────────────

StatusResult Client::SetMaintenance(const std::string& server_id) {
    json body = impl_->DoWithRetry(false, "POST", "/v1/admin/servers/" + UrlEscape(server_id) + "/maintenance",
                                   nullptr, impl_->opts.admin_api_key);
    return ParseStatus(body);
}

StatusResult Client::SetDrain(const std::string& server_id) {
    json body = impl_->DoWithRetry(false, "POST", "/v1/admin/servers/" + UrlEscape(server_id) + "/drain",
                                   nullptr, impl_->opts.admin_api_key);
    return ParseStatus(body);
}

StatusResult Client::Enable(const std::string& server_id) {
    json body = impl_->DoWithRetry(false, "POST", "/v1/admin/servers/" + UrlEscape(server_id) + "/enable",
                                   nullptr, impl_->opts.admin_api_key);
    return ParseStatus(body);
}

StatusResult Client::Disable(const std::string& server_id) {
    json body = impl_->DoWithRetry(false, "POST", "/v1/admin/servers/" + UrlEscape(server_id) + "/disable",
                                   nullptr, impl_->opts.admin_api_key);
    return ParseStatus(body);
}

Stats Client::GetStats() {
    json body = impl_->DoWithRetry(false, "GET", "/v1/admin/stats", nullptr, impl_->opts.admin_api_key);
    return body.get<Stats>();
}

CharacterPage Client::SearchCharacters(const CharacterFilter& f) {
    std::string path = "/v1/admin/characters/search";
    if (f.name) AppendQuery(path, "name", *f.name);
    if (f.server_id) AppendQuery(path, "server_id", *f.server_id);
    if (f.class_id) AppendQuery(path, "class_id", *f.class_id);
    if (f.min_level) AppendQuery(path, "min_level", *f.min_level);
    if (f.max_level) AppendQuery(path, "max_level", *f.max_level);
    if (f.limit && *f.limit > 0) AppendQuery(path, "limit", *f.limit);
    if (f.cursor) AppendQuery(path, "cursor", *f.cursor);
    json body = impl_->DoWithRetry(false, "GET", path, nullptr, impl_->opts.admin_api_key);
    return ParseCharacterPage(body);
}

Migration Client::CreateMigration(const CreateMigrationRequest& req) {
    json body_in = req.ToJSON();
    json body = impl_->DoWithRetry(false, "POST", "/v1/admin/migrations", &body_in, impl_->opts.admin_api_key);
    return body.value("migration", json::object()).get<Migration>();
}

Migration Client::GetMigration(const std::string& id) {
    json body = impl_->DoWithRetry(false, "GET", "/v1/admin/migrations/" + UrlEscape(id), nullptr,
                                   impl_->opts.admin_api_key);
    return body.value("migration", json::object()).get<Migration>();
}

std::vector<Migration> Client::ListMigrations(int limit) {
    std::string path = "/v1/admin/migrations";
    AppendQuery(path, "limit", limit);
    json body = impl_->DoWithRetry(false, "GET", path, nullptr, impl_->opts.admin_api_key);
    std::vector<Migration> migrations;
    if (body.contains("migrations") && body["migrations"].is_array()) body["migrations"].get_to(migrations);
    return migrations;
}

Migration Client::RollbackMigration(const std::string& id) {
    json body = impl_->DoWithRetry(false, "POST", "/v1/admin/migrations/" + UrlEscape(id) + "/rollback",
                                   nullptr, impl_->opts.admin_api_key);
    return body.value("migration", json::object()).get<Migration>();
}

// ── AutoHeartbeat ───────────────────────────────────────────

AutoHeartbeat::AutoHeartbeat(Client& client, std::string server_id,
                             std::chrono::milliseconds interval, HeartbeatRequest initial)
    : client_(client), server_id_(std::move(server_id)), interval_(interval),
      payload_(std::move(initial)) {
    if (interval_.count() <= 0) interval_ = std::chrono::seconds{10};
    thread_ = std::thread([this] { Run(); });
}

AutoHeartbeat::~AutoHeartbeat() { Stop(); }

void AutoHeartbeat::Set(int players, double load) {
    std::lock_guard<std::mutex> lock(mu_);
    payload_ = HeartbeatRequest{players, load};
}

void AutoHeartbeat::OnError(std::function<void(const Error&)> cb) {
    std::lock_guard<std::mutex> lock(mu_);
    on_error_ = std::move(cb);
}

void AutoHeartbeat::Stop() {
    bool expected = false;
    if (!stopped_.compare_exchange_strong(expected, true)) return;
    if (thread_.joinable()) thread_.join();
}

void AutoHeartbeat::Run() {
    auto beat = [this] {
        HeartbeatRequest req;
        std::function<void(const Error&)> cb;
        {
            std::lock_guard<std::mutex> lock(mu_);
            req = payload_;
            cb = on_error_;
        }
        try {
            client_.Heartbeat(server_id_, req);
        } catch (const Error& e) {
            if (cb) cb(e);
        } catch (const httplib::Error&) {
            if (cb) cb(Error(0, "NETWORK", "heartbeat request failed"));
        }
    };

    beat(); // immediate first report
    auto next = std::chrono::steady_clock::now() + interval_;
    while (!stopped_.load()) {
        if (std::chrono::steady_clock::now() >= next) {
            beat();
            next += interval_;
        }
        std::this_thread::sleep_for(std::chrono::milliseconds{10});
    }
}

} // namespace atlas
