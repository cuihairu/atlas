# MOBA 房间制 demo —— 匹配服 + 短生命周期战斗房

一键起一套"活的"房间制拓扑：匹配服/大厅/观战常驻，9 个战斗房按真实对局
节奏轮转——开局注册、对局内心跳、结束注销、短暂休整后换新开局再注册。
管理台服务器列表因此持续有房间进出，这是房间制品类在 Atlas 上的核心画面：
**注册/注销就是目录事件流**。

```bash
docker compose up -d --build   # 管理台 http://localhost:28220
docker compose down -v         # 收尾（清库重来）
```

## 品类机制 → Atlas 概念映射

| MOBA 机制 | Atlas 概念 | 本 demo 的体现 |
|---|---|---|
| 匹配服（长驻） | server type = `match` | `moba-match-1`，容量 5000，人数漂移 |
| 大厅/网关 | server type = `gateway` | `moba-lobby-gw-1` |
| 战斗房（一局一条进程） | server type = `battle` + **ephemeral 生命周期** | 9 个房间：`lifetime` 75s（排位）/45s（匹配），到点 `Unregister` 下线，`recycle` 8s 后重开一局 |
| 错峰开局 | `start_delay` 交错 | 0-48s 交错，任一时刻房间处于开局/对局中/歇场的不同相位 |
| 排位/匹配之分 | 标签体系 | 排位房带自定义标签 `ranked`（排位/info/公开），匹配房无标签 |
| 无角色目录 | 房间制没有 per-server 角色 | fleet 不声明 characters；看点在服务器列表与事件流，不是角色页 |

## 房间轮转的工作方式

`cmd/atlas-demoagents` 的 ephemeral 模式（`lifetime` 字段）驱动每个房间：

```
register → 心跳(5s×N，人数每次开局重摇) → lifetime 到点 → unregister(行删除)
        → 休整 recycle → 重新 register(标签随注册重打) → ……
```

排位房一局 75 秒、匹配房 45 秒；`docker compose logs -f demoagents` 里
`room closed` / `registered` 成对出现，管理台列表同步增减。

## 管理台里看什么

- **服务器列表**：12 台里 9 台在轮转——隔 30 秒刷新一次，房间数量相同但
  在线人数各不相同（每次开局重摇基线）。
- **按 type 过滤** `battle`：只看房间；排位房带"排位"标签。
- **日志**：`docker compose logs demoagents | grep "room closed"` 数出已结束
  的对局数——这就是品类的"场次"指标在目录侧的投影。

## 调参

改 `fleet.json` 里的 `lifetime`/`recycle`/`start_delay` 后
`docker compose down -v && docker compose up -d --build` 生效（数据卷里
存着旧房间的注册历史，清卷保证列表干净）。
