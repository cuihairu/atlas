# 品类 demo 集 —— 五种游戏类型，五个一键拓扑

同一个 `atlas-demoagents` 二进制解释五份 `fleet.json`，跑出五种品类的服务器
拓扑。每个子目录一条命令起全套（Atlas + PostgreSQL + Redis + 管理台 + 模拟
舰队），端口互不冲突，可同时起五个对照着看。

| demo | 品类命题 | 端口 | 一句话 |
|---|---|---|---|
| [mmorpg/](mmorpg/) | 大世界：大区/分线组织 + 常驻跨服 | 28210-28215 | realm/shard 编排、维护分线、跨服配置热更 |
| [moba/](moba/) | 房间制：高频注册/注销 | 28220-28225 | 战斗房 45-75s 一局轮转，目录就是事件流 |
| [slg/](slg/) | 策略：赛季/合服 + 临时跨服 | 28230-28235 | 长驻世界服 + 4 分钟一场的联盟战节点 |
| [card/](card/) | 弱实时：最小接入 | 28240-28245 | 无 realm 无标签，注册/心跳/角色目录三件事 |
| [fps/](fps/) | 低延迟：区域制就近接入 | 28250-28255 | 按 region 注册、2 分钟一局的区域房间 |

```bash
cd deployments/demo/mmorpg   # 或 moba / slg / card / fps
docker compose up -d --build # 管理台见上表首端口
docker compose down -v       # 收尾（清库重来）
```

## 选哪个看什么

- 第一次接触 Atlas → **card**（必选面只有三项，10 行配置）。
- 想看拓扑能力 → **mmorpg**（realm/shard/标签/跨服配置一网打尽）。
- 想看动态性 → **moba**（列表 30 秒一变）或 **fps**（按地域过滤的房间轮转）。
- 想看长线运营语义 → **slg**（赛季换 realm、合服打标签、战时跨服）。

## 通用事实

- fleet 是纯数据：改 `fleet.json` 后 `docker compose down -v && up -d --build`
  生效，不需要改任何代码。
- 生命周期字段（`lifetime`/`recycle`/`start_delay`）驱动房间类节点的
  开局→心跳→注销→休整→重开循环；不带这些字段的节点常驻。
- `notify_mode`（subscribe/callback/poll）决定节点如何收跨服配置变更；
  mmorpg 与 slg 各带一份 `crossserver-config.json` 供一键发布验证热更。
- 本目录顶层另有一份 `fleet.json`：那是 demoagents **内建默认舰队**的镜像
  （`atlas-demoagents -print-fleet` 生成，二者保持同步）——不带 `-fleet`
  参数启动时跑的就是它，与五个品类 demo 无关。
