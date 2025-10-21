#pragma once
#include <cstdint>
#include <cstring>
#include <string>

namespace rpc {

struct envelope {
  uint16_t type{};
  uint16_t flags{};  // 0=OK, 1=ERR
  uint32_t corr{};
  std::string payload;
};

inline void put_be32(uint32_t v, unsigned char* b) {
  b[0] = static_cast<unsigned char>(v >> 24);
  b[1] = static_cast<unsigned char>(v >> 16);
  b[2] = static_cast<unsigned char>(v >> 8);
  b[3] = static_cast<unsigned char>(v);
}
inline uint32_t be32(const unsigned char* b) {
  return (uint32_t(b[0]) << 24) | (uint32_t(b[1]) << 16) | (uint32_t(b[2]) << 8) | b[3];
}

inline std::string encode(envelope const& e) {
  std::string out;
  out.resize(2 + 2 + 4 + e.payload.size());
  auto* p = reinterpret_cast<unsigned char*>(out.data());
  p[0] = static_cast<unsigned char>(e.type >> 8);
  p[1] = static_cast<unsigned char>(e.type & 0xFF);
  p += 2;
  p[0] = static_cast<unsigned char>(e.flags >> 8);
  p[1] = static_cast<unsigned char>(e.flags & 0xFF);
  p += 2;
  put_be32(e.corr, p);
  p += 4;
  if (!e.payload.empty()) std::memcpy(p, e.payload.data(), e.payload.size());
  return out;
}
inline bool decode_header(std::string_view frame, envelope& e) {
  if (frame.size() < 8) return false;
  auto* p = reinterpret_cast<const unsigned char*>(frame.data());
  e.type = static_cast<uint16_t>((uint16_t(p[0]) << 8) | p[1]);
  p += 2;
  e.flags = static_cast<uint16_t>((uint16_t(p[0]) << 8) | p[1]);
  p += 2;
  e.corr = be32(p);
  p += 4;
  e.payload.assign(reinterpret_cast<const char*>(p), frame.data() + frame.size() - reinterpret_cast<const char*>(p));
  return true;
}

} // namespace rpc

