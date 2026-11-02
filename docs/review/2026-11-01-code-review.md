# 代码深审报告（2026-11-01）

> 任务：docs 审计完成后接续【代码深审】（用户 6 点清单）：①安全静态扫描 ②隐藏 bug 猎捕 ③质量门 ④报告本档 ⑤P0/P1 当场修 ⑥汇报计数。
> 范围：本仓全量——`internal/`、`cmd/`、`sdk/`、`examples/`、`plugins/apisix/`（Lua 静态面）、`dashboard/`（TS/TSX 面）、`deploy/`（shell 面）。
> 方法：以源码为真相；每条结论带 file:line 证据；否定性结论先验证搜索路径存在。

## ① 安全静态扫描（六类）

| 类别 | 扫描方法 | 结果 | 证据 |
| --- | --- | --- | --- |
| shell 注入 | `exec.Command` / `os/exec` 全仓 grep | **无** | internal/ cmd/ sdk/ 零命中（含非测试） |
| 硬编码密钥 | `(password|secret|apikey|token)= "≥8 字符"` 字面量 grep | **无** | internal/ cmd/ sdk/ 零命中；所有敏感值来自 env（ATLAS_* 或 SDK options） |
| SQL 注入 | `fmt.Sprintf` 拼接 SQL + 列名位置 | **无**（列名硬编码白名单、值全参数化） | internal/store/postgres/postgres.go:345-388（`name = $%d`…字面列名 + `args` 参数化）；mysql/mysql.go:372 同构（`?` 占位） |
| eval / 模板 | `text/template` / `html/template` / `eval` | **无** | internal/ cmd/ 零命中（dashboard 走前端打包，无 Go 模板） |
| 反序列化 | `json.Unmarshal` 进 `map[string]any` + 类型断言 | **无危险形态** | 全部解码进具名结构体（json tag 校验），无 `map[string]any` 承载不受信输入驱动代码路径 |
| 路径穿越 | 请求路径 → 文件路径 | **无** | 文件打开均配置驱动：internal/audit/jsonl.go:24（sink 路径 env 配置）、cmd/soakcheck/main.go:76,115（CLI flag）；HTTP 面无 `os.Open(请求字段)` |
| TLS 降级 | `InsecureSkipVerify` | **无** | internal/ cmd/ sdk/ 零命中；tlsutil 全强校验（mTLS 客户端 CA 校验） |

**结论：安全静态扫描未发现 P0/P1/P2 漏洞。** 随机数使用 `crypto/rand`（非 math/rand）用于 token 生成。

## ② 隐藏 bug 猎捕

### 资源泄漏（rows / 文件 / HTTP body 逐站点配对核验）

| 资源 | 计数 | 关闭核验 | 证据 |
| --- | --- | --- | --- |
| pgx `Query` 行集 | 10 处 | 10/10 `defer rows.Close()`，无遗漏 | internal/store/postgres/postgres.go:199,419,453,521,607,642,828,891,980,1075 |
| mysql `QueryContext` 行集 | 10 处 | 10/10 `defer rows.Close()`，无遗漏 | internal/store/mysql/mysql.go:183,407,438,502,599,637,826,890,965,1058 |
| audit JSONL 文件 | 1（sink 持有） | `Close()` 关闭 f | internal/audit/jsonl.go:24,45 |
| soakcheck 文件 | 2 | 均 `defer f.Close()` | cmd/soakcheck/main.go:76,81,115,120 |
| HTTP 响应体 | 4 处 `Do` | 3 处内联关（500 路径 drain+close）；SDK 经 `decodeJSON` 的 `defer resp.Body.Close()` 统一关 | internal/crossserver/service.go:423、internal/health/alert.go:194、cmd/atlas-demoagents/main.go:928；sdk/go/atlas/client.go:205 |

**结论：无资源泄漏。**

### 竞态 / 并发

- `go test -race ./...`：**exit 0，0 FAIL**（真实 SQL/Redis 容器）。
- 共享可变状态访问均加锁：crossserver 回调 `go func(t callbackTarget)` 值传递 + `sync.Mutex`（service.go:354-367）+ `sync.WaitGroup`/信号量 `sem` 配对；audit dispatcher `sync.Once`（queue.go:209）；fleet reconciler 复合委托 + CAS（先前取证）。
- 后台 goroutine（21 处 `go func`/`go X()`）均为独立生命周期（server 监听、watcher、心跳循环、worker 池），无未同步共享写。

**结论：无数据竞态。**

### 越界 / off-by-one / 除零

- 分页 clamp：limit 夹在 [1,200]，offset ≥0（先前取证，store 层统一）。
- 路由有效负载除零守卫：`effectiveLoad` 仅当 `s.Weight > 0 && s.Weight != 100` 才除（`load × 100 / weight`），weight=0（未设）与 100（中性）均返回 `s.Load`——**无除零**（internal/routing/service.go:525-530）。
- 迁移窗口/批量循环 per-server 失败落 Failed（先前取证，无越界）。

**结论：无越界/off-by-one/除零缺陷。**

### 错误处理缺失

- 唯一 `_` 丢弃错误：auth.go:201 `host, _, _ := net.SplitHostPort(r.RemoteAddr)`——端口确实不需要，host 尽力提取；RemoteAddr 由 net/http 保证为 `ip:port`，非客户端可控，失败仅得空 host 落入空桶。**非缺陷**（见 OBS-1）。

**结论：无实质错误处理缺失。**

## ③ 质量门

| 门禁 | 结果 | 证据 |
| --- | --- | --- |
| `go build ./...` | ✅ 0 错 | 实测 |
| `go vet ./...`（= `make lint`） | ✅ 0 错 | Makefile:64 `lint: go vet ./...` |
| `go test ./...` | ✅ 全绿 exit 0（真实 SQL/Redis 容器） | 实测 |
| `go test -race ./...` | ✅ 全绿 exit 0，0 FAIL | 实测 |
| `make test-coverage` | ✅ 98.09% ≥ 98%（COVERAGE_GATE:=98，Makefile:41） | 先前本会话实测；Go 逻辑本轮无改动，门禁不受 docs 批影响 |
| `npx vitepress build` | ✅ 绿（59.93s） | 阶段一实测 |

**lint/类型检查基线：无 golangci-lint/staticcheck/errcheck 配置**（见 OBS-2）。

## ④ 发现（按严重度排序）

| ID | 严重度 | 位置 | 描述 | 证据 | 处置 |
| --- | --- | --- | --- | --- | --- |
| SEC-1…SEC-7 | — | — | 七类安全扫描全净 | 见 §① | 无需修 |
| BUG-1…BUG-4 | — | — | 资源/竞态/越界/错误处理全净 | 见 §② | 无需修 |
| **OBS-1** | P3 | internal/httpapi/auth.go:201 | `SplitHostPort` 错误被 `_` 丢弃；RemoteAddr 非客户端可控，失败仅空 host 落空桶，无利用路径 | auth.go:201 | 记录不修 |
| **OBS-2** | P3 | Makefile:64 | 无 golangci-lint/staticcheck/errcheck；`make lint` 仅 `go vet`。errcheck 类未配，当前唯一丢弃错误为 OBS-1 有意为之 | Makefile:64；rest.go:99-100 `//nolint:errcheck` | 记录不修（可后续加 errcheck 作为防回归） |

**P0 = 0，P1 = 0，P2 = 0，P3 = 2（观察项）。**

## ⑤ 修复与 commit

- **P0/P1/P2：0 条，无需当场修（无 commit）。**
- 阶段一（docs 审计）相关 commit（已推 `main`）：`3a50603` docs fix doc-source drift、`a99a231` fix examples env、`5ed862f` docs tidy todo、`35e39a4` docs audit report。
- 阶段二：无修复 commit（发现仅 2 条 P3 观察项，按 P2/P3 记 todo 口径——OBS-2 记入 todo 待用户拍板是否补 errcheck）。

## ⑥ 计数汇报

- **发现：0 条 P0/P1/P2 + 2 条 P3 观察项（OBS-1、OBS-2）。**
- **已修：0 条（无 P0/P1 需修）。**
- **剩余：2 条 P3 观察项（记录不修；OBS-2 已记 todo 待拍板）。**
- **commit 清单：阶段一 4 条（`3a50603`/`a99a231`/`5ed862f`/`35e39a4`，已推）；阶段二 0 条。**
