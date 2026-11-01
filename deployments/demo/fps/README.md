# FPS 低延迟房间 demo —— 区域制接入 + 就近房间服

低延迟品类的两个目录侧命题：**玩家该连哪个地域**（就近接入）和**房间在哪
开、开了多久**（区域房间轮转）。一键起 4 个地域的接入网关 + 7 个房间服，
网关常驻，房间按 2 分钟一局轮转——同一套 ephemeral 机制，但节奏是 FPS 的。

```bash
docker compose up -d --build   # 管理台 http://localhost:28250
docker compose down -v         # 收尾（清库重来）
```

## 品类机制 → Atlas 概念映射

| FPS 机制 | Atlas 概念 | 本 demo 的体现 |
|---|---|---|
| 就近接入（华东/华北/华南/东南亚） | `region` 字段 + 客户端按 region 过滤 discovery | 4 台网关各占一个 region，玩家侧拉列表时只取本域 |
| 区域房间服 | server type = `room` + region | 7 个房间分布在 4 个地域，跟着网关就近开 |
| 一局一条进程（约 2 分钟） | ephemeral 生命周期（`lifetime` 2m） | 对局结束 `Unregister`、`recycle` 10s 后开新局；与 MOBA 同机制、不同节奏 |
| 错峰开局 | `start_delay` 交错 0-60s | 任一时刻各房间处于不同对局相位 |
| 满员敏感（32 人小容量） | capacity 32 + 小 drift | 人数贴近容量，容量条接近红区——FPS 房间是"满即换房"的 |
| PC/主机跨平台 | `platform` 字段 | 2 个 console 房间、5 个 PC 房间，discovery 可按平台过滤 |
| 快速负载反映 | 心跳 5s | 低延迟品类的负载信息要新，`-interval=5s` |

## 拓扑

```
cn-east 华东                 cn-north 华北
├─ fps-gw-east-1    网关     ├─ fps-gw-north-1   网关
├─ fps-room-east-1  pc       ├─ fps-room-north-1 pc
└─ fps-room-east-2  console  └─ fps-room-north-2 pc

cn-south 华南                ap-southeast 东南亚
├─ fps-gw-south-1   网关     ├─ fps-gw-sea-1     网关
└─ fps-room-south-1 pc       ├─ fps-room-sea-1   pc
                             └─ fps-room-sea-2   console
```

无 realm/shard：FPS 的组织维度是**地域**不是大区分线，`region` 一层就够，
这也是"Atlas 不强制拓扑"的另一面。

## 管理台里看什么

- **按 region 过滤**：切到 `cn-east` 只剩网关 + 2 房间——这就是玩家侧
  "就近拉列表"在管理台的镜像。
- **按 platform 过滤** `console`：两台主机房；`pc`：五台。
- **房间轮转**：房间寿命 2 分钟，比 MOBA 慢一拍；`docker compose logs
  demoagents | grep "room closed"` 数已结束的对局。
- **容量条**：房间 32 人容量、开局人数贴近满——与 MMORPG 场景服的
  大容量低占比形成对照。

## 与 MOBA demo 的对照

同一条 ephemeral 轮转曲线，两个品类参数不同：MOBA 是单地域、45-75s
极速轮转、12 台同屏进出；FPS 是 4 地域、2m 一局、每域一两间、
重点在 region 过滤与平台维度。调参都在 `fleet.json`。
