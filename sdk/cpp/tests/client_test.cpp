// Gate-level tests for the Atlas C++ SDK: run the Client against an
// embedded cpp-httplib server that mimics the REST API shapes from
// docs/api.md.
#include <atomic>
#include <chrono>
#include <iostream>
#include <thread>

#include <httplib.h>
#include <nlohmann/json.hpp>

#include "atlas/client.hpp"

using namespace std::chrono_literals;

namespace {

struct TestServer {
    httplib::Server srv;
    std::thread thread;
    int port = 0;

    std::atomic<int> register_auth_ok{0};
    std::atomic<int> heartbeat_count{0};
    std::atomic<int> last_players{0};

    TestServer() {
        srv.Get("/stop", [&](const httplib::Request&, httplib::Response& res) {
            res.set_content("bye", "text/plain");
            srv.stop();
        });

        srv.Post("/v1/registry/servers/register", [&](const httplib::Request& req, httplib::Response& res) {
            if (req.get_header_value("Authorization") == "Bearer reg-token") register_auth_ok++;
            nlohmann::json body = nlohmann::json::parse(req.body);
            if (body.value("server_id", "") != "game-1") {
                res.status = 400;
                res.set_content(R"({"error":{"code":"INVALID_ARGUMENT","message":"server_id"}})",
                                "application/json");
                return;
            }
            res.status = 201;
            res.set_content(R"({"server_id":"game-1","status":"starting"})", "application/json");
        });

        srv.Post(R"(/v1/registry/servers/([^/]+)/heartbeat)",
                 [&](const httplib::Request& req, httplib::Response& res) {
                     heartbeat_count++;
                     auto body = nlohmann::json::parse(req.body);
                     last_players = body.value("players", 0);
                     res.set_content(R"({"server_id":"game-1","status":"online","next_heartbeat_in":10})",
                                     "application/json");
                 });

        srv.Get("/v1/discovery/servers", [&](const httplib::Request& req, httplib::Response& res) {
            if (req.get_param_value("region") != "cn-east" ||
                req.get_param_value("status") != "online") {
                res.status = 400;
                return;
            }
            res.set_content(
                R"({"servers":[{"id":"game-1","name":"Test","type":"game","region":"cn-east",)"
                R"("version":"1.0.0","platform":"any","endpoint":{"host":"10.0.0.1","port":30001},)"
                R"("capacity":100,"status":"online","players":5,"load":0.25}]})",
                "application/json");
        });

        srv.Get(R"(/v1/discovery/servers/([^/]+))", [&](const httplib::Request& req, httplib::Response& res) {
            if (req.matches[1] == "missing") {
                res.status = 404;
                res.set_content(R"({"error":{"code":"SERVER_NOT_FOUND","message":"server missing does not exist"}})",
                                "application/json");
                return;
            }
            res.set_content(R"({"id":"game-1","status":"online"})", "application/json");
        });

        srv.Post(R"(/v1/directory/characters)", [&](const httplib::Request& req, httplib::Response& res) {
            auto body = nlohmann::json::parse(req.body);
            if (body.value("account_id", 0) == 0) {
                res.status = 400;
                res.set_content(R"({"error":{"code":"INVALID_ARGUMENT","message":"account_id"}})",
                                "application/json");
                return;
            }
            res.status = 201;
            res.set_content(
                R"({"account_id":7,"server_id":"game-1","character_id":823712,"name":"Hero",)"
                R"("level":1,"class_id":3,"created_at":"2026-10-01T00:00:00Z",)"
                R"("updated_at":"2026-10-01T00:00:00Z"})",
                "application/json");
        });
    }

    void Start() {
        port = srv.bind_to_any_port("127.0.0.1");
        thread = std::thread([this] { srv.listen_after_bind(); });
        // Wait for accept loop.
        while (!srv.is_running()) std::this_thread::sleep_for(1ms);
    }
    ~TestServer() {
        if (srv.is_running()) srv.stop();
        if (thread.joinable()) thread.join();
    }
};

int failures = 0;

#define CHECK(cond)                                                                    \
    do {                                                                               \
        if (!(cond)) {                                                                 \
            std::cerr << "FAIL " << __FILE__ << ":" << __LINE__ << "  " << #cond << "\n"; \
            failures++;                                                                \
        }                                                                              \
    } while (0)

atlas::Options TestOptions(const std::string& base) {
    atlas::Options opts;
    opts.base_url = base;
    opts.registry_token = "reg-token";
    opts.admin_api_key = "adm-key";
    opts.base_backoff = 1ms;
    return opts;
}

void TestLifecycle(TestServer& ts) {
    atlas::Client c(TestOptions("http://127.0.0.1:" + std::to_string(ts.port)));

    auto reg = c.Register([] {
        atlas::RegisterRequest r;
        r.server_id = "game-1";
        r.name = "Test";
        r.capacity = 100;
        r.endpoint = {"10.0.0.1", 30001};
        return r;
    }());
    CHECK(reg.server_id == "game-1");
    CHECK(ts.register_auth_ok.load() == 1);

    auto hb = c.Heartbeat("game-1", {5, 0.25});
    CHECK(hb.status == "online");

    auto servers = c.ListServers([] {
        atlas::ServerFilter f;
        f.region = "cn-east";
        f.status = "online";
        return f;
    }());
    CHECK(servers.size() == 1);
    CHECK(servers[0].id == "game-1");
    CHECK(servers[0].endpoint.port == 30001);

    bool threw = false;
    try {
        c.GetServer("missing");
    } catch (const atlas::Error& e) {
        threw = e.code() == "SERVER_NOT_FOUND" && e.status() == 404;
    }
    CHECK(threw);

    auto wr = c.CreateCharacter([] {
        atlas::CreateCharacterRequest r;
        r.account_id = 7;
        r.server_id = "game-1";
        r.character_id = 823712;
        r.name = "Hero";
        return r;
    }());
    CHECK(wr.status == "created");
    CHECK(wr.character.has_value());
    CHECK(wr.character->character_id == 823712);
    CHECK(wr.character->created_at.time_since_epoch().count() != 0);

    atlas::CreateCharacterRequest bad;
    bad.server_id = "game-1";
    bool invalid = false;
    try {
        c.CreateCharacter(bad);
    } catch (const atlas::Error& e) {
        invalid = e.code() == "INVALID_ARGUMENT";
    }
    CHECK(invalid);
}

void TestAutoHeartbeat(TestServer& ts) {
    atlas::Client c(TestOptions("http://127.0.0.1:" + std::to_string(ts.port)));
    {
        atlas::AutoHeartbeat loop(c, "game-1", 30ms, {1, 0.1});
        loop.Set(42, 0.5);
        auto deadline = std::chrono::steady_clock::now() + 2s;
        while (ts.heartbeat_count.load() < 2 && std::chrono::steady_clock::now() < deadline) {
            std::this_thread::sleep_for(5ms);
        }
    } // destructor joins
    CHECK(ts.heartbeat_count.load() >= 2);
    CHECK(ts.last_players.load() == 42);
}

} // namespace

int main() {
    TestServer ts;
    ts.Start();
    TestLifecycle(ts);
    TestAutoHeartbeat(ts);
    if (failures == 0) {
        std::cout << "all atlas cpp sdk tests passed\n";
        return 0;
    }
    std::cerr << failures << " check(s) failed\n";
    return 1;
}
