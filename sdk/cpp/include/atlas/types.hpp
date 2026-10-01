// Atlas C++ SDK — data types (TODO v0.1.7).
//
// Mirrors the Go SDK (sdk/go/atlas/types.go) and the REST JSON shapes
// in docs/api.md. Serialization helpers live next to the structs so
// client.cpp stays focused on transport.
#pragma once

#include <chrono>
#include <cctype>
#include <cstdint>
#include <cstdio>
#include <ctime>
#include <map>
#include <optional>
#include <string>
#include <vector>

#include <nlohmann/json.hpp>

namespace atlas {

using TimePoint = std::chrono::system_clock::time_point;

// RFC3339 helpers (same layout Atlas uses on the wire, UTC).
inline std::string FormatTime(TimePoint t) {
    std::time_t tt = std::chrono::system_clock::to_time_t(t);
    std::tm tm{};
    gmtime_r(&tt, &tm);
    char buf[40];
    std::snprintf(buf, sizeof(buf), "%04d-%02d-%02dT%02d:%02d:%02dZ", tm.tm_year + 1900,
                  tm.tm_mon + 1, tm.tm_mday, tm.tm_hour, tm.tm_min, tm.tm_sec);
    return buf;
}

inline std::optional<TimePoint> ParseTime(const std::string& s) {
    std::tm tm{};
    int year = 0, month = 0, day = 0, hour = 0, minute = 0, second = 0;
    if (std::sscanf(s.c_str(), "%d-%d-%dT%d:%d:%d", &year, &month, &day, &hour, &minute,
                    &second) != 6) {
        return std::nullopt;
    }
    tm.tm_year = year - 1900;
    tm.tm_mon = month - 1;
    tm.tm_mday = day;
    tm.tm_hour = hour;
    tm.tm_min = minute;
    tm.tm_sec = second;
    std::time_t tt = timegm(&tm);
    if (tt == static_cast<std::time_t>(-1)) return std::nullopt;
    return std::chrono::system_clock::from_time_t(tt);
}

struct Endpoint {
    std::string host;
    int port = 0;
};

struct Server {
    std::string id;
    std::string name;
    std::string type;
    std::string region;
    std::optional<std::string> realm_id;
    std::optional<std::string> shard_id;
    std::string version;
    std::string platform;
    Endpoint endpoint;
    int capacity = 0;
    std::map<std::string, std::string> metadata;
    std::string status;
    int players = 0;
    double load = 0.0;
    std::optional<TimePoint> last_seen_at;
    TimePoint created_at{};
    TimePoint updated_at{};
};

NLOHMANN_DEFINE_TYPE_NON_INTRUSIVE_WITH_DEFAULT(Endpoint, host, port)

inline void from_json(const nlohmann::json& j, Server& s) {
    s.id = j.value("id", "");
    s.name = j.value("name", "");
    s.type = j.value("type", "");
    s.region = j.value("region", "");
    if (j.contains("realm_id") && !j["realm_id"].is_null()) s.realm_id = j["realm_id"];
    if (j.contains("shard_id") && !j["shard_id"].is_null()) s.shard_id = j["shard_id"];
    s.version = j.value("version", "");
    s.platform = j.value("platform", "");
    if (j.contains("endpoint")) j.at("endpoint").get_to(s.endpoint);
    s.capacity = j.value("capacity", 0);
    if (j.contains("metadata") && j["metadata"].is_object()) {
        j["metadata"].get_to(s.metadata);
    }
    s.status = j.value("status", "");
    s.players = j.value("players", 0);
    s.load = j.value("load", 0.0);
    if (j.contains("last_seen_at") && j["last_seen_at"].is_string()) {
        s.last_seen_at = ParseTime(j["last_seen_at"].get<std::string>());
    }
    if (j.contains("created_at")) s.created_at = ParseTime(j["created_at"].get<std::string>()).value_or(TimePoint{});
    if (j.contains("updated_at")) s.updated_at = ParseTime(j["updated_at"].get<std::string>()).value_or(TimePoint{});
}

inline void to_json(nlohmann::json& j, const Server& s) {
    j = nlohmann::json{{"id", s.id}, {"name", s.name}, {"type", s.type}, {"region", s.region},
                       {"version", s.version}, {"platform", s.platform}, {"endpoint", s.endpoint},
                       {"capacity", s.capacity}, {"status", s.status}, {"players", s.players},
                       {"load", s.load}};
    if (s.realm_id) j["realm_id"] = *s.realm_id;
    if (s.shard_id) j["shard_id"] = *s.shard_id;
    if (!s.metadata.empty()) j["metadata"] = s.metadata;
}

struct RegisterRequest {
    std::string server_id;
    std::string name;
    std::string type = "game";
    std::string region;
    std::optional<std::string> realm_id;
    std::optional<std::string> shard_id;
    std::string version;
    std::string platform;
    Endpoint endpoint;
    int capacity = 0;
    std::map<std::string, std::string> metadata;

    nlohmann::json ToJSON() const {
        nlohmann::json j{{"server_id", server_id}, {"name", name}, {"type", type},
                         {"region", region},       {"version", version}, {"platform", platform},
                         {"endpoint", endpoint},   {"capacity", capacity}};
        if (realm_id) j["realm_id"] = *realm_id;
        if (shard_id) j["shard_id"] = *shard_id;
        if (!metadata.empty()) j["metadata"] = metadata;
        return j;
    }
};

struct RegisterResult {
    std::string server_id;
    std::string status;
};

struct HeartbeatRequest {
    int players = 0;
    double load = 0.0;

    nlohmann::json ToJSON() const { return nlohmann::json{{"players", players}, {"load", load}}; }
};

struct HeartbeatResult {
    std::string server_id;
    std::string status;
    int next_heartbeat_in = 0;
};

struct StatusResult {
    std::optional<std::string> server_id;
    std::string status;
};

// Filter for ListServers; empty fields are omitted.
struct ServerFilter {
    std::optional<std::string> region;
    std::optional<std::string> version;
    std::optional<std::string> platform;
    std::optional<std::string> status;
    std::optional<int> limit;
};

struct Character {
    std::int64_t account_id = 0;
    std::string server_id;
    std::int64_t character_id = 0;
    std::string name;
    int level = 0;
    int class_id = 0;
    std::string avatar;
    std::map<std::string, std::string> metadata;
    std::optional<TimePoint> last_login_at;
    TimePoint created_at{};
    TimePoint updated_at{};
};

inline void from_json(const nlohmann::json& j, Character& c) {
    c.account_id = j.value("account_id", std::int64_t{0});
    c.server_id = j.value("server_id", "");
    c.character_id = j.value("character_id", std::int64_t{0});
    c.name = j.value("name", "");
    c.level = j.value("level", 0);
    c.class_id = j.value("class_id", 0);
    c.avatar = j.value("avatar", "");
    if (j.contains("metadata") && j["metadata"].is_object()) j["metadata"].get_to(c.metadata);
    if (j.contains("last_login_at") && j["last_login_at"].is_string()) {
        c.last_login_at = ParseTime(j["last_login_at"].get<std::string>());
    }
    if (j.contains("created_at")) c.created_at = ParseTime(j["created_at"].get<std::string>()).value_or(TimePoint{});
    if (j.contains("updated_at")) c.updated_at = ParseTime(j["updated_at"].get<std::string>()).value_or(TimePoint{});
}

struct CreateCharacterRequest {
    std::int64_t account_id = 0;
    std::string server_id;
    std::int64_t character_id = 0;
    std::string name;
    std::optional<int> level;
    std::optional<int> class_id;
    std::optional<std::string> avatar;

    nlohmann::json ToJSON() const {
        nlohmann::json j{{"account_id", account_id}, {"server_id", server_id},
                         {"character_id", character_id}, {"name", name}};
        if (level) j["level"] = *level;
        if (class_id) j["class_id"] = *class_id;
        if (avatar) j["avatar"] = *avatar;
        return j;
    }
};

// PATCH body; unset fields are left unchanged.
struct UpdateCharacterRequest {
    std::optional<std::string> name;
    std::optional<int> level;
    std::optional<int> class_id;
    std::optional<std::string> avatar;

    nlohmann::json ToJSON() const {
        nlohmann::json j = nlohmann::json::object();
        if (name) j["name"] = *name;
        if (level) j["level"] = *level;
        if (class_id) j["class_id"] = *class_id;
        if (avatar) j["avatar"] = *avatar;
        return j;
    }
};

// Directory write reply; character is empty when status == "queued".
struct CharacterWriteResult {
    std::optional<Character> character;
    std::string status;
};

struct CharacterPage {
    std::vector<Character> characters;
    std::string next_cursor;
};

struct CharacterFilter {
    std::optional<std::string> name;
    std::optional<std::string> server_id;
    std::optional<int> class_id;
    std::optional<int> min_level;
    std::optional<int> max_level;
    std::optional<int> limit;
    std::optional<std::string> cursor;
};

struct Recommendation {
    Server server;
    std::string reason;
};

struct Stats {
    int total_servers = 0;
    std::map<std::string, int> servers_by_status;
    std::map<std::string, int> servers_by_region;
    std::map<std::string, int> servers_by_version;
    int total_players = 0;
    int total_capacity = 0;
    int total_characters = 0;
};

inline void from_json(const nlohmann::json& j, Stats& s) {
    s.total_servers = j.value("total_servers", 0);
    s.total_players = j.value("total_players", 0);
    s.total_capacity = j.value("total_capacity", 0);
    s.total_characters = j.value("total_characters", 0);
    if (j.contains("servers_by_status")) j["servers_by_status"].get_to(s.servers_by_status);
    if (j.contains("servers_by_region")) j["servers_by_region"].get_to(s.servers_by_region);
    if (j.contains("servers_by_version")) j["servers_by_version"].get_to(s.servers_by_version);
}

struct Migration {
    std::string id;
    std::vector<std::string> source_servers;
    std::string target_server;
    std::string status;
    TimePoint started_at{};
    std::optional<TimePoint> completed_at;
};

inline void from_json(const nlohmann::json& j, Migration& m) {
    m.id = j.value("id", "");
    if (j.contains("source_servers")) j["source_servers"].get_to(m.source_servers);
    m.target_server = j.value("target_server", "");
    m.status = j.value("status", "");
    if (j.contains("started_at") && j["started_at"].is_string()) {
        m.started_at = ParseTime(j["started_at"].get<std::string>()).value_or(TimePoint{});
    }
    if (j.contains("completed_at") && j["completed_at"].is_string()) {
        m.completed_at = ParseTime(j["completed_at"].get<std::string>());
    }
}

struct CreateMigrationRequest {
    std::vector<std::string> source_servers;
    std::string target_server;

    nlohmann::json ToJSON() const {
        return nlohmann::json{{"source_servers", source_servers}, {"target_server", target_server}};
    }
};

} // namespace atlas
