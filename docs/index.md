---
layout: home

hero:
  name: Atlas
  text: Game Infrastructure Directory / Control Plane
  tagline: 面向在线游戏的服务器注册、发现与角色目录基础设施。回答三个问题——"我是谁？""谁在线？""我的角色在哪？" 它是控制面（Control Plane），不是游戏后端。
  image:
    src: /logo.svg
    alt: Atlas
  actions:
    - theme: brand
      text: 快速上手
      link: /api-quickstart
    - theme: alt
      text: 按场景找文档
      link: #使用场景

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

## 界面速览

管理台（Dashboard）长什么样，一眼看全——自动轮播，也可用两侧箭头、下方圆点或键盘 ←/→ 翻看：

<script setup>
const showcaseSlides = [
  { image: '/screenshots/overview.png', title: '总览', caption: '总览 — 在线服务器 / 玩家 / 容量与状态分布', link: '/scenarios#scenario-overview' },
  { image: '/screenshots/servers.png', title: '服务器列表', caption: '服务器列表 — 状态、角标（火热/新服/爆满）与容量水位', link: '/scenarios#scenario-tags' },
  { image: '/screenshots/server-detail-tags.png', title: '服务器详情', caption: '服务器详情 — 标记管理（对外/内部）与实时指标', link: '/scenarios#scenario-tags' },
  { image: '/screenshots/characters.png', title: '角色搜索', caption: '角色搜索 — 跨服角色索引，按名字/服务器/等级过滤', link: '/scenarios#scenario-characters' },
  { image: '/screenshots/migrations.png', title: '迁移管理', caption: '迁移管理 — 合服/转服编排与回滚', link: '/scenarios#scenario-migration' },
]
</script>

<ShowcaseCarousel :slides="showcaseSlides" />

每张截图都来自真实运行的 Atlas + 管理台（数据为演示集群）。按场景一步步走一遍，见 **[场景导览](/scenarios)**；实现层面的取舍见 **[性能设计](/performance)**。

## 一条命令跑起来

```bash
go run ./cmd/atlas
# …level=INFO msg="public API listening"   addr=:8080
# …level=INFO msg="registry API listening" addr=:8081 scheme=http
# …level=INFO msg="admin API listening"    addr=:8082
```

看到三行 listening，Atlas 就在跑了（内存存储，零依赖）。接下来[快速上手](/api-quickstart)
用约 5 分钟、全部真实命令走完**注册上线 → 玩家选服 → 角色目录 → 运维操作 → 优雅下线**的完整链路。

## 使用场景

Atlas 站在游戏服务器和玩家客户端中间，三类人各自从这里拿走不同的东西：

| 你是 | 典型问题 | 去哪 |
| --- | --- | --- |
| 游戏服务器开发者 | 进程怎么接入注册、心跳怎么打、如何优雅下线？不想写注册代码行不行？ | [快速上手 §1/§4](/api-quickstart) · [服务器生命周期](/lifecycle) · [服务器信息配置化](/server-config) · [六语言 SDK](/sdk-go) |
| 客户端 / 网关开发者 | 玩家登录后进哪台服？我的角色在哪？公告从哪拉？ | [快速上手 §2](/api-quickstart) · [接入推荐与粘滞](/concepts) · [APISIX 接入](/apisix) |
| 运维 / 平台 | 大区分服怎么管？怎么发公告、排维护？半夜掉线谁告诉我？合服转服怎么编排？ | [Realm 与 Shard](/realms-shards) · [公告与计划维护](/operations) · [健康警报](/lifecycle#_4-1-健康警报-v0-1-15-交付) · [合服 / 转服 / 迁服](/migration) |
| 架构 / 平台负责人 | 多副本怎么部署？消息总线怎么选？扛得住多少 QPS？ | [高可用](/ha) · [部署拓扑](/topology) · [数据同步](/sync) · [性能基准](/benchmarks) |

## 能力速览

| 能力 | 一句话说清 | 文档 |
| --- | --- | --- |
| 注册与心跳 | 游戏服务器登记身份与端点，心跳保活，失联两段式判死（suspect → offline） | [生命周期](/lifecycle) |
| 发现与推荐 | 按区域 / 大区 / 分服筛选在线服；账号维度推荐"该进哪服"，有角色则粘滞 | [快速上手 §2](/api-quickstart) |
| 角色目录 | 账号 → 跨服角色索引，登录一次拉全；写端点唯一入口，可选 Message Bus 异步缓冲，at-least-once 幂等落地 | [数据同步](/sync) |
| 服务器信息配置化 | 静态舰队用 JSON 声明服务器，起服即生效，不写注册代码；profile 一键切环境 | [服务器信息配置化](/server-config) |
| Realm / Shard | 大区 → 分服 → 实例三层运营档案，玩家端按维度拉列表 | [Realm 与 Shard](/realms-shards) |
| 公告与计划维护 | 公告到生效区间自动可见；维护窗口到点自动进维护、结束自动恢复 | [公告与计划维护](/operations) |
| 健康警报 | 失联 / 死亡占比越阈值，锁存发 webhook + Warn 日志，不刷屏 | [健康警报](/lifecycle#_4-1-健康警报-v0-1-15-交付) |
| 合服 / 转服 / 迁服 | 角色索引原子迁移，先复制、后切换、再清理，失败可回滚 | [迁移](/migration) |
| 网关与鉴权 | APISIX 两插件：玩家 token 校验 + 身份注入，按端点组限流 | [APISIX 接入插件](/apisix) |
| 高可用 | Atlas 副本无状态化，PG / Redis 后端共享，HAProxy 心跳扇入 | [高可用](/ha) |

<style>
:root {
  --vp-home-hero-name-color: transparent;
  --vp-home-hero-name-background: linear-gradient(135deg, #d97706 0%, #f59e0b 50%, #e98f36 100%);
  --vp-home-hero-image-background-image: radial-gradient(circle at 50% 50%, rgba(217, 119, 6, 0.12) 0%, transparent 70%);
  --vp-home-hero-image-filter: blur(44px);
}
</style>
