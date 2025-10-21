---
title: 基准测试（Google Benchmark）
---

基本用法
```cpp
static void BM_Add(benchmark::State& s){
  for(auto _ : s){ benchmark::DoNotOptimize(add(1,2)); }
}
BENCHMARK(BM_Add);
BENCHMARK_MAIN();
```

基准质量
- 预热/迭代次数：让 JIT/缓存状态稳定（无 JIT 时也有缓存与动态载入因素）
- 隔离外界：固定 CPU 频率/亲和性、关闭涡轮/节能、断开网络
- 避免死代码消除：`DoNotOptimize`/`ClobberMemory`

注意事项
- 统计分布：观察中位数/方差而非单次结果
- 与 Profiling 结合：定位热点后再做针对性优化
