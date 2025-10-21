#pragma once
#include <atomic>
#include <future>
#include <mutex>
#include <string>
#include <thread>
#include <unordered_map>
#include <boost/asio.hpp>
#include <boost/asio/experimental/parallel_group.hpp>
#include "rpc/envelope.hpp"
#include "rpc/io.hpp"

namespace rpc {
namespace asio = boost::asio;
using tcp = asio::ip::tcp;
using asio::awaitable;
using asio::use_awaitable;

struct client {
  tcp socket;
  std::mutex m;
  std::unordered_map<uint32_t, std::promise<envelope>> pending;
  std::atomic<uint32_t> next{1};

  explicit client(asio::any_io_executor ex) : socket(ex) {}

  awaitable<void> reader() {
    try {
      for (;;) {
        auto frame = co_await read_frame(socket);
        envelope e{};
        if (!decode_header(frame, e)) continue;
        std::promise<envelope> p;
        {
          std::scoped_lock lk(m);
          auto it = pending.find(e.corr);
          if (it == pending.end()) continue;
          p = std::move(it->second);
          pending.erase(it);
        }
        p.set_value(std::move(e));
      }
    } catch (...) {
      std::scoped_lock lk(m);
      for (auto& [id, p] : pending) {
        try { throw std::runtime_error("conn closed"); } catch (...) {
          p.set_exception(std::current_exception());
        }
      }
      pending.clear();
    }
  }

  awaitable<envelope> call(uint16_t type, std::string payload, std::chrono::milliseconds to) {
    uint32_t id = next.fetch_add(1, std::memory_order_relaxed);
    envelope req{.type = type, .flags = 0, .corr = id, .payload = std::move(payload)};
    std::promise<envelope> p;
    auto fut = p.get_future();
    {
      std::scoped_lock lk(m);
      pending.emplace(id, std::move(p));
    }
    co_await write_frame(socket, encode(req));

    auto ex = co_await asio::this_coro::executor;
    asio::steady_timer timer(ex);
    timer.expires_after(to);
    auto g = asio::experimental::make_parallel_group(
      asio::async_initiate<decltype(use_awaitable), void()>(
        [&](auto h) {
          std::thread([f = std::move(fut), h = std::move(h)]() mutable {
            f.wait();
            asio::post([h = std::move(h)]() mutable { h(); });
          }).detach();
        },
        use_awaitable),
      timer.async_wait(use_awaitable));
    auto [idx, r1, r2] = co_await g.async_wait(asio::experimental::wait_for_one(), use_awaitable);
    if (idx == 1) {
      std::scoped_lock lk(m);
      if (auto it = pending.find(id); it != pending.end()) {
        try { throw std::runtime_error("timeout"); } catch (...) {
          it->second.set_exception(std::current_exception());
        }
        pending.erase(it);
      }
      throw std::runtime_error("timeout");
    }

    envelope rsp = p.get_future().get();
    co_return rsp;
  }
};

} // namespace rpc

