# BUGS

> 已确认缺陷登记簿。每条：现象 → 根因 → 修法 → 状态。修完打勾并注明 commit。

## ① 管理台总览「在线服务器」恒 0（2026-10-04）

- **现象**：总览页「在线服务器」卡片恒为 0；实测 `GET /v1/admin/stats?status=online` 有 7 台在线，数据本身不缺。
- **根因**：前后端字段脱节——前端 `AdminStats`（dashboard/src/types/index.ts）声明并读取 `online_servers`（Overview.tsx 统计卡），后端 `model.Stats`（internal/model/model.go）没有这个字段，JSON 里不存在 → `undefined` → 渲染为 0。
- **修法**：后端 `Stats` 补 `OnlineServers`（= `ServersByStatus["online"]`，JSON `online_servers`），三库 `GetStats` 与聚合索引同口径回填；`AdminStats` 类型逐字段核对补齐其余差异。
- **状态**：[x] 已修复（见提交记录）

## ② /servers/{id} 详情页「标记面板」崩溃报告（2026-10-04）

- **现象**：用户访问 /servers/game-1001 详情页崩溃；网络面板见 `/v1/admin/servers/{id}/tags` 404。
- **排查**（三处核对，2026-10-04 实测）：
  1. **部署镜像**：`2a0ba2b0` 是 `ghcr.io/cuihairu/atlas:latest` 的**镜像 ID**（sha256 前缀，非提交），构建于 2026-10-03 18:38 UTC，晚于 tags 路由提交（c77c028）——运行中镜像含该路由；直连部署机 `http://192.168.5.5:8082/v1/admin/servers/game-1001/tags` 与线上 `https://atlas.cuihairu.site` 同路径**均返回 200**（viewer key）。
  2. **ADMIN_BASE**：演示构建未设 `VITE_ADMIN_API_BASE`（dashboard/Dockerfile 无 ARG/ENV），同源相对路径 → nginx `location /v1/admin/` → `atlas:8082`，承载全部 admin 路由（stats/tags 均 200）。
  3. **路径**：大小写/尾斜杠与客户端拼接一致，无问题。
- **结论**：路由与部署均正常，404 为测试时刻的镜像/构建时效问题（该时刻后已随 latest 滚动修复）。**真正的崩溃风险在前端空值**：`ServerDetail` 的 `Promise.all` 中 `getServer`/`listCharactersByServer` 无 catch（任一失败 → 页面卡死在 Spin 并抛未处理 rejection），`characters`/`tags.tags` 缺失时 `.length` 直接白屏（与迁移页 `null.some` 同款教训）。
- **修法**：详情页全页空值兜底（三个请求各自 catch 归一化默认值、可选字段全量 `??` 防护、字段缺失不许崩）。
- **状态**：[x] 已修复（见提交记录）

## ③ /servers 页「区域」等聚合各页各自现查现算，数字不一致（2026-10-04）

- **现象**：/servers 页区域数据与总览等其他视图对不上。
- **根因**：同一指标多处独立计算——总览读 `/v1/admin/stats`（store 现查聚合），/servers 页走 discovery 列表默认 `Visible()` 过滤（隐藏 offline，7 台 vs 统计 8 台）、筛选条选项由当前页数据推导，口径互不相同。
- **裁决（用户）**：区域、服务器 ID 这类聚合在注册/心跳时就统计出来存内存，**所有视图读同一份聚合，不许各页各自现查现算**。
- **修法**：内存聚合索引（region/status/version/type/realm/shard/tag → 计数 + server id 索引）随注册/心跳/注销实时维护（写路径装饰器喂入 + 启动 seed + 定时对账兜底）；总览 `servers_by_*`、/servers 筛选条选项、列表统计全部统一读这份——数字天然一致。
- **状态**：[x] 已修复（见提交记录）
