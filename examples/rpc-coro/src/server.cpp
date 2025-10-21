#include <boost/asio.hpp>
#include "rpc/envelope.hpp"
#include "rpc/io.hpp"
#include "rpc/router.hpp"

namespace asio = boost::asio;
using asio::awaitable;
using asio::use_awaitable;
using tcp = asio::ip::tcp;

awaitable<void> handle(tcp::socket s, rpc::router& r) {
  try {
    for (;;) {
      auto frame = co_await rpc::read_frame(s);
      rpc::envelope req{};
      if (!rpc::decode_header(frame, req)) throw std::runtime_error("bad frame");
      rpc::envelope rsp{.type = req.type, .flags = 0, .corr = req.corr};
      try {
        if (r.map.empty()) {
          // default echo
          rsp.payload = std::string("echo:") + req.payload;
        } else {
          rsp.payload = co_await r.dispatch(req.type, req.payload);
        }
      } catch (std::exception const& e) {
        rsp.flags = 1; rsp.payload = e.what();
      }
      co_await rpc::write_frame(s, rpc::encode(rsp));
    }
  } catch (...) {
    co_return;
  }
}

awaitable<void> run(unsigned short port) {
  auto ex = co_await asio::this_coro::executor;
  tcp::acceptor acc(ex, {tcp::v4(), port});
  rpc::router r; // optionally register handlers here
  for (;;) {
    tcp::socket s(ex);
    co_await acc.async_accept(s, use_awaitable);
    co_spawn(ex, handle(std::move(s), r), asio::detached);
  }
}

int main() {
  try {
    asio::io_context ctx;
    co_spawn(ctx, run(5555), asio::detached);
    ctx.run();
  } catch (...) {
    return 1;
  }
}

