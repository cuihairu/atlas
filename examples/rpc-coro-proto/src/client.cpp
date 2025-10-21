#include <boost/asio.hpp>
#include <chrono>
#include <iostream>
#include "rpc/envelope.hpp"
#include "rpc/io.hpp"
#include "hello.pb.h"

namespace asio = boost::asio;
using asio::awaitable; using asio::use_awaitable; using tcp=asio::ip::tcp; using namespace std::chrono_literals;

awaitable<void> run(){
  auto ex = co_await asio::this_coro::executor;
  tcp sock(ex); co_await sock.async_connect({asio::ip::make_address("127.0.0.1"), 5556}, use_awaitable);
  demo::HelloReq req; req.set_name("atlas"); std::string bytes; req.SerializeToString(&bytes);
  rpc::envelope env{.type=1001,.flags=0,.corr=1,.payload=bytes};
  co_await rpc::write_frame(sock, rpc::encode(env));
  auto frame = co_await rpc::read_frame(sock);
  rpc::envelope rsp{}; rpc::decode_header(frame, rsp);
  demo::HelloRsp out; out.ParseFromArray(rsp.payload.data(), (int)rsp.payload.size());
  std::cout << out.msg() << "\n";
}

int main(){ asio::io_context ctx; co_spawn(ctx, run(), asio::detached); ctx.run(); }

