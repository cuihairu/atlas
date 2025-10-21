---
title: 载荷编码（JSON / Proto）与选择策略
---

JSON
- 优点：开发快、可读可调试；生态丰富
- 缺点：编码/解析开销大；类型约束弱
- 适合：配置/控制平面、低频请求、日志/审计

Protocol Buffers
- 优点：IDL 强类型、紧凑、前后兼容；跨语言广泛
- 缺点：需要生成代码；动态 schema 支持弱
- 适合：高频 RPC、跨团队服务接口、长线维护

选择建议
- 控制/调试 → JSON；数据平面/高频 → Proto/FlatBuffers
- 内部服务优先 Proto；外部 API 可 JSON + 版本化

协商
- flags 指出编码类型；或定义 type 空间的一部分为 JSON 类型
- 为 handler 提供统一解码接口（多态/variant 返回统一对象）
