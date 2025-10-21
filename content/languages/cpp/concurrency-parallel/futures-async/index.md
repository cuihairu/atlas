---
title: futures / promises / async（过渡与互操作）
---

模型
- `std::async` 提交任务并返回 `future`；`promise` 手工完成 `future`
- 与协程互操作：在异步边界处可封装 `future`→`co_await` 的桥接（或反之）

示例
```cpp
std::future<int> f = std::async(std::launch::async, []{ return 42; });
auto v = f.get();
```

建议
- 新代码优先协程 + 任务系统；legacy 场景用 `future` 作为过渡
- 避免不明确的 `std::async` 策略（deferred vs async）；显式线程池更可控
