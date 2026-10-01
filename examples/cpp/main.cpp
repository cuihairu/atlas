// Atlas C++ SDK example: register a game server, auto-heartbeat,
// recommend + list characters, then leave cleanly on SIGINT.
//
// Build (from examples/cpp):
//   cmake -S . -B build -DATLAS_SDK_BUILD_EXAMPLE=ON
//   cmake --build build
//   ./build/atlas_example_cpp
#include <atomic>
#include <chrono>
#include <csignal>
#include <iostream>

#include "atlas/client.hpp"

using namespace std::chrono_literals;

namespace {
std::atomic<bool> g_stop{false};
void OnSignal(int) { g_stop = true; }
} // namespace

int main() {
    std::signal(SIGINT, OnSignal);
    std::signal(SIGTERM, OnSignal);

    atlas::Client c([] {
        atlas::Options o;
        if (const char* env = std::getenv("ATLAS_ADDR")) o.base_url = env;
        // Local dev (no merged proxy): point Registry at its split port.
        if (const char* env = std::getenv("ATLAS_REGISTRY_ADDR")) o.registry_base_url = env;
        if (const char* tok = std::getenv("ATLAS_REGISTRY_TOKEN")) o.registry_token = tok;
        return o;
    }());

    // 1. Register.
    atlas::RegisterRequest reg;
    reg.server_id = "demo-game-1";
    reg.name = "Demo Game Server";
    reg.type = "game";
    reg.region = "cn-east";
    reg.version = "1.0.0";
    reg.platform = "any";
    reg.endpoint = {"10.0.0.1", 30001};
    reg.capacity = 2000;

    auto regRes = c.Register(reg);
    std::cout << "registered: " << regRes.server_id << " (" << regRes.status << ")\n";

    // 2. Auto-heartbeat: immediate first report, then every 10s.
    atlas::AutoHeartbeat loop(c, reg.server_id, 10s);
    loop.OnError([](const atlas::Error& e) { std::cerr << "heartbeat failed: " << e.what() << "\n"; });

    // Wait for the first heartbeat to land so Recommend sees us online.
    for (int i = 0; i < 20; ++i) {
        try {
            if (c.GetServer(reg.server_id).status == "online") break;
        } catch (const atlas::Error&) {
        }
        std::this_thread::sleep_for(100ms);
    }

    // 3. Routing: where should account 42 play?
    try {
        auto rec = c.Recommend(42, "cn-east");
        std::cout << "account 42 -> " << rec.server.id << " (" << rec.reason << ")\n";

        // 4. Directory: characters there.
        auto page = c.ListCharactersByServer(rec.server.id, 10);
        for (const auto& ch : page.characters) {
            std::cout << "  character " << ch.character_id << " " << ch.name << " lv" << ch.level << "\n";
        }
    } catch (const atlas::Error& e) {
        std::cerr << "atlas error: " << e.what() << "\n";
    }

    // 5. Stay up until signalled, then leave cleanly.
    while (!g_stop) std::this_thread::sleep_for(200ms);

    std::cout << "shutting down…\n";
    loop.Stop();
    try {
        c.Unregister(reg.server_id);
    } catch (const atlas::Error& e) {
        std::cerr << "unregister failed: " << e.what() << "\n";
    }
    return 0;
}
