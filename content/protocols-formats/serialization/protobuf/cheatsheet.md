---
title: Protobuf Cheatsheet
---

- proto3 语法要点：标量/enum/oneof/map，默认值
- 向后兼容：tag 不复用；保留字段；新增字段使用可选
- 性能：packed 重复字段；避免过深嵌套；注意 arena/zero-copy（C++）
- 工具：protoc 插件/生成器；语言绑定注意事项
