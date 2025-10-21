---
title: Work-Stealing 最小可运行示例（有锁 deque 版）
---

说明
- 为便于快速跑通，这里用有锁 deque 实现 work-stealing；后续可替换成无锁结构（如 chase‑lev）。

代码（单文件示例）
```cpp
#include <thread>
#include <mutex>
#include <deque>
#include <vector>
#include <functional>
#include <atomic>
#include <condition_variable>
#include <random>

struct ws_pool{
  struct lane{ std::mutex m; std::deque<std::function<void()>> q; };
  std::vector<lane> lanes; std::vector<std::thread> ths; std::atomic<bool> stop{false};
  explicit ws_pool(size_t n): lanes(n){ for(size_t i=0;i<n;++i) ths.emplace_back([&,i]{ run(i); }); }
  ~ws_pool(){ stop=true; for(auto& t:ths) t.join(); }
  void submit(std::function<void()> f){ auto i = std::hash<std::thread::id>{}(std::this_thread::get_id())%lanes.size(); std::scoped_lock lk(lanes[i].m); lanes[i].q.emplace_front(std::move(f)); }
  bool pop_front(size_t i, std::function<void()>& f){ std::scoped_lock lk(lanes[i].m); if(lanes[i].q.empty()) return false; f=std::move(lanes[i].q.front()); lanes[i].q.pop_front(); return true; }
  bool steal(size_t i, std::function<void()>& f){ for(size_t k=0;k<lanes.size();++k){ size_t j=(i+k+1)%lanes.size(); std::scoped_lock lk(lanes[j].m); if(!lanes[j].q.empty()){ f=std::move(lanes[j].q.back()); lanes[j].q.pop_back(); return true; } } return false; }
  void run(size_t i){ while(!stop){ std::function<void()> f; if(pop_front(i,f) || steal(i,f)) f(); else std::this_thread::yield(); } }
};

int main(){ ws_pool pool(std::thread::hardware_concurrency()); std::atomic<int> c{0};
  for(int i=0;i<100000;i++) pool.submit([&]{ c.fetch_add(1, std::memory_order_relaxed); });
  while(c.load()<100000) std::this_thread::yield();
}
```

注意
- 该实现仅用于演示：有锁 deque，yield 作为退避；实际应使用条件变量/事件与无锁结构。
- 指标：可在 `submit/run/steal` 计数，打印窃取次数与任务等待时长。
