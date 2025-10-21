#include <boost/asio.hpp>
#include "rpc/envelope.hpp"
#include "rpc/io.hpp"
#include "hello.pb.h"

namespace asio = boost::asio;
using asio::awaitable;
using asio::use_awaitable;
using tcp = asio::ip::tcp;

awaitable<void> session(tcp::socket s){
  try{
    for(;;){
      auto frame = co_await rpc::read_frame(s);
      rpc::envelope req{}; if(!rpc::decode_header(frame, req)) throw std::runtime_error("bad frame");
      rpc::envelope rsp{.type=req.type, .flags=0, .corr=req.corr};
      if(req.type == 1001){
        demo::HelloReq in; in.ParseFromArray(req.payload.data(), (int)req.payload.size());
        demo::HelloRsp out; out.set_msg("hello, "+in.name());
        std::string bytes; out.SerializeToString(&bytes); rsp.payload = std::move(bytes);
      } else {
        rsp.flags=1; rsp.payload = "unknown type";
      }
      co_await rpc::write_frame(s, rpc::encode(rsp));
    }
  }catch(...){ co_return; }
}

awaitable<void> run(unsigned short port){
  auto ex = co_await asio::this_coro::executor; tcp::acceptor acc(ex, {tcp::v4(), port});
  for(;;){ tcp::socket s(ex); co_await acc.async_accept(s, use_awaitable); co_spawn(ex, session(std::move(s)), asio::detached); }
}

int main(){ asio::io_context ctx; co_spawn(ctx, run(5556), asio::detached); ctx.run(); }

