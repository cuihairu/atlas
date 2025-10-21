---
title: 序列化（JSON / CBOR / Proto / FlatBuffers / Cap'n Proto）
---

选择对比（简表）
- JSON：文本友好、可读性高；性能一般；`nlohmann::json`/`rapidjson`
- CBOR/MessagePack：二进制自描述，较 JSON 更高效；`nlohmann::json` 支持 CBOR
- Protocol Buffers：IDL + 代码生成，前后兼容优秀，广泛使用
- FlatBuffers/Cap'n Proto：零拷贝读取、低延迟（内存映射友好），写入更复杂

实践建议
- 边界文本（日志/配置）用 JSON；服务 RPC/流量层用 Proto/FlatBuffers
- 定义稳定 schema，采用“向后兼容”演进；保留字段号，不复用已删除的
- 对性能敏感路径优先 FlatBuffers/Cap'n Proto；离线/持久化也可选 CBOR

示例：nlohmann::json
```cpp
#include <nlohmann/json.hpp>
using json = nlohmann::json;
json j = {{"x",1},{"y",2}}; int x=j["x"];
```

示例：Protocol Buffers
- 定义 `.proto`，生成 C++ 代码，序列化到字节流或文件
- 配合 arena 分配、zero-copy I/O 优化
