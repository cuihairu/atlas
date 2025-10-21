---
title: Work-Stealing 线程池（设计草案）
---

目标
- 降低全局队列争用：每个 worker 维护无锁双端队列，工作窃取从尾端偷取

核心结构
- workers：`N` 个 worker，每个线程一个 `deque<task>`（无锁/有锁视实现）
- 提交：优先入本地队列头（调用线程或随机 worker）；无 worker 时入全局 MPMC 队列
- 窃取：空闲 worker 从其他 worker 尾端窃取任务

伪代码
```text
submit(task t):
  w = pick_worker(); w.local.push_front(t) or global.push(t)

worker_loop(w):
  while(!stop){
    if(auto t = w.local.pop_front()) run(t);
    else if(auto t = global.pop()) run(t);
    else if(auto t = steal_from_others_tail()) run(t);
    else park_wait();
  }
```

注意点
- 双端队列选择：`chase-lev` 算法等无锁结构；或先用有锁版验证
- 任务粘性：同一数据/缓存亲和的任务尽量留在本地队列
- 停止与回收：广播停止信号；确保无悬挂任务，父对象负责 `join`

背压与优先级
- 多级队列：高优先级任务单独队列；或任务带权重
- 背压：按总队列长度/内存占用限流

观测与调参
- 指标：全局/本地队列长度、窃取次数、任务执行/等待时间分布
- 调参：workers 数、窃取策略、唤醒/park 策略
