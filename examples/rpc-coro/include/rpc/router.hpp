#pragma once
#include <functional>
#include <string>
#include <string_view>
#include <unordered_map>
#include <boost/asio.hpp>

namespace rpc {
namespace asio = boost::asio;
using asio::awaitable;

using handler_t = awaitable<std::string>(std::string_view);

struct router {
  std::unordered_map<uint16_t, std::function<handler_t>> map;
  template <class F>
  void reg(uint16_t type, F&& f) { map[type] = std::forward<F>(f); }
  handler_t dispatch(uint16_t type, std::string_view payload) {
    if (auto it = map.find(type); it != map.end())
      co_return co_await it->second(payload);
    throw std::runtime_error("unknown type");
  }
};

} // namespace rpc

