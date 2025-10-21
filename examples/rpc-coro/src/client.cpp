#include <boost/asio.hpp>
#include <chrono>
#include <iostream>
#include "rpc/client.hpp"

namespace asio = boost::asio;
using asio::awaitable;
using asio::use_awaitable;
using tcp = asio::ip::tcp;
using namespace std::chrono_literals;

awaitable<void> run() {
  auto ex = co_await asio::this_coro::executor;
  rpc::client c(ex);
  co_await c.socket.async_connect({asio::ip::make_address("127.0.0.1"), 5555}, use_awaitable);
  co_spawn(ex, c.reader(), asio::detached);
  auto rsp = co_await c.call(1, "{\"a\":2,\"b\":3}", 500ms);
  std::cout << rsp.payload << "\n";
}

int main() {
  asio::io_context ctx;
  co_spawn(ctx, run(), asio::detached);
  ctx.run();
}

