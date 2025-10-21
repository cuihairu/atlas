---
title: RPC 信封（Envelope）与类型表
---

二进制信封（建议）
- 字节序：大端（network byte order）
- 结构：
  - `uint16_t type`
  - `uint16_t flags`（0=OK，1=ERR；bit 可扩展：压缩/编码等）
  - `uint32_t corr_id`（请求-响应关联 ID）
  - `uint32_t payload_len`
  - `payload bytes`

类型表（示例）
- 1: add（JSON）
- 2: ping（JSON）
- 1001: Hello.Request（Proto）
- 1002: Hello.Response（Proto）

错误语义
- flags=ERR 时，payload 建议为 UTF‑8 文本或 JSON/Proto 的 Error 对象
- 协议版本兼容：保留未使用的 type 范围与字段；新增字段走向后兼容

扩展
- 压缩：在 flags 标记 gzip/zstd，并对 payload 压缩
- 签名：在负载前附一段签名/校验和；或双层信封
