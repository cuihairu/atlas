#pragma once
#include <array>
#include <string>
#include <boost/asio.hpp>
#include "rpc/envelope.hpp"

namespace rpc {
namespace asio = boost::asio;
using tcp = asio::ip::tcp;
using asio::awaitable;
using asio::use_awaitable;

inline awaitable<std::string> read_frame(tcp::socket& s) {
  std::array<unsigned char, 4> len{};
  co_await asio::async_read(s, asio::buffer(len), use_awaitable);
  uint32_t n = be32(len.data());
  std::string buf(n, '\0');
  if (n) co_await asio::async_read(s, asio::buffer(buf), use_awaitable);
  co_return buf;
}
inline awaitable<void> write_frame(tcp::socket& s, std::string_view frame) {
  std::array<unsigned char, 4> len{};
  put_be32(static_cast<uint32_t>(frame.size()), len.data());
  std::array<asio::const_buffer, 2> bufs{asio::buffer(len), asio::buffer(frame)};
  co_await asio::async_write(s, bufs, use_awaitable);
}

} // namespace rpc

