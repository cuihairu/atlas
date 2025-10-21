---
title: 测试框架（gtest / catch2 / doctest）
---

风格与选择
- GoogleTest：主流、断言丰富、子进程/参数化/Fixture 完备
- Catch2/doctest：单头/轻量，断言表达式简洁

组织结构
- 单元测试与集成测试分层；覆盖率统计纳入 CI
- Fixture/自定义匹配器；参数化测试覆盖边界情况

示例（gtest）
```cpp
TEST(Math, Add){ EXPECT_EQ(add(1,2), 3); }
```

最佳实践
- 测试可重复、隔离外部依赖；引入 Fake/Stub/Mock 控制环境
- 快速失败：断言与日志；在 sanitizer/TSan 组合下跑回归
