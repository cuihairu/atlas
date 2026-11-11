# SLG 策略 demo —— 长驻世界服 + 联盟战临时跨服（赛季/合服语义）

一键起一套"活的"SLG 拓扑：一个赛季 realm 下三个长驻世界服，两个联盟战
跨服节点按"战役日"节奏上线/下线——SLG 的跨服是**临时的**（战时合兵、战后
解散），与 MMORPG 常驻跨服恰成对照。

```bash
docker compose up -d --build   # 管理台 http://localhost:28230
docker compose down -v         # 收尾（清库重来）
```

## 品类机制 → Atlas 概念映射

| SLG 机制 | Atlas 概念 | 本 demo 的体现 |
|---|---|---|
| 赛季（S3） | realm | `realm-s3`：赛季就是一次大区编排，下赛季换 realm 重建 |
| 世界服（长驻数月） | server type = `world` + shard | 3 个世界 `shard-s3-w1..w3`，全部 subscribe 模式收赛季编排 |
| 联盟战临时跨服 | server type = `war` + **ephemeral** | 2 个战役节点：`lifetime` 4m（演示压缩；真实战役数小时），打完注销、休整 1m 再开下一场 |
| 战役错峰 | `start_delay` 交错 | 官渡 0s / 赤壁 2m：任一时刻至少一场战役在进行 |
| 合服（人气下滑的区并入大区） | 标签 + 迁移 API | 三区带自定义标签 `merge-pending`（合服预约/info/公开）；真实合服走管理台的服迁移接口，把三区角色目录迁入目标世界 |
| 君主（玩家角色） | directory | 账号 800001 在一区/二区各有一个君主——SLG 跨区多号在角色目录里就是两行 |

## 赛季怎么换

realm 是数据不是代码：下赛季 = 管理台新建 `realm-s4` 与新 shard，新世界服
注册时挂上去；旧 `realm-s3` 整体归档（服务器列表按 realm 过滤即"历史赛季"
视图）。demo 里重跑一遍 `docker compose down -v && up` 就是一次"换赛季"。

## 管理台里看什么

- **服务器列表**：三个世界服永在；两个联盟战节点每 4-5 分钟消失又出现
  （`docker compose logs demoagents | grep "room closed"` 即战役结束记录）。
- **按 realm 过滤** `realm-s3`：赛季视图；按 type 过滤 `war`：只看战役。
- **角色目录**：搜账号 `800001` —— 两个世界各一位君主（燕云十八骑/燕云·塞北）。
- **跨服配置**：发布 demo 附带的 spec（战役参战名单就是一次拓扑发布），
  `slg-world-1`（`-watch-server` 观察目标）的日志立刻出现 Info 级
  `cross-server config applied (hot, no restart)`；两个 war 节点同样经
  subscribe 信号收到（它们的热更日志是 Debug 级，默认不显示）：

  ```bash
  curl -X PUT -H 'Content-Type: application/json' \
    --data-binary @crossserver-config.json \
    http://localhost:28230/v1/admin/crossserver/config
  ```

  未发布前 subscribe 节点每 10s 提示"等待跨服配置"——那就是编排在途的样子。

## 与其他 demo 的对照

- vs **MMORPG**：跨服常驻（`cross` 类型永在线）vs 跨服临时（`war` 类型
  ephemeral 轮转）。
- vs **MOBA**：同样是 ephemeral，SLG 的周期是"小时级战役"（demo 压缩成
  4 分钟），MOBA 是"分钟级对局"。
