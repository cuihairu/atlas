---
layout: home

hero:
  name: Atlas
  text: Game Infrastructure Directory
  tagline: 面向在线游戏的服务器注册、发现与角色目录基础设施。不是返回一份服务器列表，而是回答三个问题——"我是谁？""谁在线？""我的角色在哪？"
  image:
    src: /logo.svg
    alt: Atlas
  actions:
    - theme: brand
      text: 快速上手
      link: /api-quickstart
    - theme: alt
      text: 架构设计
      link: /architecture

features:
  - title: 服务器注册与心跳
    details: 游戏服务器启动时注册身份、拓扑与端点，通过周期心跳保持在线状态。心跳超时后自动进入 suspect → offline，客户端永远看不到已死的服务器。
  - title: 跨服角色目录
    details: 账号到角色的跨服索引。玩家在一个界面看到自己在所有服务器上的角色名称、等级和职业。这是投影索引，不持有权威数据——角色数据库始终归游戏服务器所有。
  - title: 合服与迁移编排
    details: 合服、转服、迁服的幂等编排。角色索引在事务内原子切换，过程可重放、可回滚。先复制、后切换、再清理，任何一步失败源数据都还在。
  - title: 公告与计划维护
    details: 运维声明、Atlas 执行——紧急故障一条 API 发公告，计划维护提前建窗口：start_at 到点自动进入维护、end_at 自动恢复，维护公告同时段自动挂出，客户端登录即见。
---

<style>
:root {
  --vp-home-hero-name-color: transparent;
  --vp-home-hero-name-background: linear-gradient(135deg, #d97706 0%, #f59e0b 50%, #e98f36 100%);
  --vp-home-hero-image-background-image: radial-gradient(circle at 50% 50%, rgba(217, 119, 6, 0.12) 0%, transparent 70%);
  --vp-home-hero-image-filter: blur(44px);
}
</style>