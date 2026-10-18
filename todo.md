# TODO

- [x] 注册生命周期：永久 / 暂时 两类标记（来源：用户 2026-10-14）— 已交付（2026-10-14）：`reg_kind`（permanent / temporary）+ `ttl_seconds` 显式标记落 server 记录与三库存储（迁移 0008），注册 API（REST）与校验接入；过期走健康监控独立路径，只对 `temporary` 且 `created_at + ttl_seconds` 超时回收（绝对时长，不读运行时），永久与遗留空值行任何情况不自动删；显式删除入口 `DELETE /v1/admin/servers/{id}`（config 托管 409）；model/registry/health/admin/httpapi/storetest 新增 14 tests 绿 — commit 4ed7dae
- [x] 永久实例离线告警（来源：用户 2026-10-14）— 已交付（2026-10-14）：健康监控 offline 转移时经既有 Alerter 渠道（Warn 日志 + `ATLAS_ALERT_WEBHOOK_URL`）发单服务器告警 `permanent_offline`，载荷含 `server_id` / `reg_kind` / `offline_for` / `maintenance`（lifecycle.md §4.4）；豁免两条路径齐备：maintenance 非 auto-managed 不进转移（结构性豁免），temporary 消失属预期不告警；阈值沿用 `ATLAS_SUSPECT_AFTER` / `ATLAS_OFFLINE_AFTER`（默认 30s / 60s）；health 新增 6 tests 绿（含告警触发与维修豁免） — commit 4ed7dae
- [ ] 审核整改（2026-10-18 独立审核，见 docs/review/luna-review-2026-10-18.md）：
      按审核报告的三张表逐项处理，**先核对再动手**，每项给 file:line 证据。
      1) 表一「缺陷」：按风险排序修，高风险优先
      2) 表二「文档与实现不一致」：以代码为准改文档，或明确标注为规划中
         **禁止为对齐文档去改业务代码**
      3) 表三「值得补的能力」：只做评估与分期建议，**不实现**（需用户拍板）
      每完成一项勾掉并写证据；全部测试通过后 commit+push；不 bump 版本号。
