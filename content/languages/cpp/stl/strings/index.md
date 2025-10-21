---
title: 字符串（string / string_view）
---

string 与 string_view
- `std::string` 拥有存储；`std::string_view` 非拥有视图
- 视图不延长寿命；仅用于观察（函数参数）

编码与字面量
- 宽/UTF 字符：`u8/u/U/L` 前缀；后续建议统一 UTF-8 处理
- 转码：`std::wstring_convert` 已弃用；考虑 ICU/Boost.Text 或平台 API

拼接与格式化
- `std::format`（C++20/23）或 fmtlib；避免 `ostringstream` 在热路径
- 预留容量、`append`/`reserve` 降低分配

最佳实践
- 接口参数优先 `string_view`，边界处物化为 `string`
- 明确编码约定（UTF-8）并在边界处理转码
