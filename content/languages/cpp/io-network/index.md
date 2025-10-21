---
title: I/O 与网络（Asio / 序列化 / RPC）
---

导航
- Asio/Networking TS（协程整合、定时器/TCP）：`io-network/asio-networking`
- 序列化选型（JSON/CBOR/Proto/FlatBuffers）：`io-network/serialization`
- RPC 栈示例（demo/路由/信封/客户端/Proto/指标/库脚手架）：见 Asio 页下 RPC 分组

实践建议
- I/O 线程与计算线程分离；以 `strand` 保证连接内顺序；以任务系统执行计算
- 载荷编码约定清晰（JSON/Proto）；错误语义与版本兼容预先设计
