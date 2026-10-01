# 安全审计

安全不是一次性的交付，而是**周期性的复查**。本文记录 Atlas 的审计范围、检查清单与
历次审计结论；自动化部分由 CI 承担（[workflow](https://github.com/cuihairu/atlas/blob/main/.github/workflows/security-audit.yml)：
每周一执行 `govulncheck` + 两处 npm `audit`），人工部分按版本节点执行并在此追加记录。

## 1. 审计范围与清单

每次审计（CI 周期或版本发布前）按层过一遍：

| 层 | 检查项 | 代码位置 |
| --- | --- | --- |
| 认证 | Registry token / mTLS 握手策略（最低 TLS 1.2、fail fast） | `internal/httpapi/registry_auth.go`、`internal/tlsutil` |
| 认证 | Admin RBAC 角色解析（未知角色启动失败、viewer 只读） | `internal/httpapi/auth.go` |
| 审计 | actor 指纹不可逆（key / 客户端证书 SHA-256 前 12 位）；diff 截断 4KB | `internal/httpapi/audit.go` |
| 限流 | 桶表上限防 XFF 内存膨胀；规则解析 fail fast | `internal/httpapi/ratelimit.go` |
| 存储 | SQL 全参数化（拼接只允许占位符序号与固定子句名） | `internal/store/postgres`、`internal/store/mysql` |
| 存储 | Redis key 构造（ID 直接进 key 的注入面） | `internal/store/redisstore` |
| 供应链 | 依赖漏洞（Go 模块 + dashboard / docs npm 生产依赖） | CI `govulncheck` / `npm audit` |
| 密钥 | 环境变量密钥不进日志、不进审计 diff | `internal/config`、`internal/httpapi` |

## 2. v0.1.20 审计记录（2026-10）

**结论：发现并修复 1 项依赖漏洞（GO-2026-6443）；1 项低危加固建议在案。**

**发现与处置（依赖漏洞）**：

- `govulncheck` 检出 **GO-2026-6443**：gRPC 服务端在缺失 authority / Host 头时
  panic（远程 DoS），`google.golang.org/grpc@v1.84.0`，可达路径
  `cmd/atlas/main.go → grpc.Server.Serve → http2Server.HandleStreams`（:9090 内网端口，暴露面有限但真实）。
- 处置：先按伪版本锁定修复（`v1.85.0-dev.0.20260825072537-93e31b48545e`），
  2026-10-02 巡检经 Dependabot PR #6 升至 `v1.86.0-dev`（含修复，解除伪版本
  锁定；带动 `protobuf v1.36.12`）。修复后 `govulncheck` 通过，全部测试门禁通过。
- 残留（不可修复，不可达）：**GO-2026-5932**——`golang.org/x/crypto/openpgp`
  已废弃且不再维护（`Found in v0.57.0`，`Fixed in: N/A`）。Atlas 不 import
  `openpgp`，govulncheck 判定「模块级存在、调用路径不涉及」（affected = 0）。
  `x/crypto` 仍被 `tlsutil` 等处间接依赖，无法移除；每次复扫确认该条从
  「affected」降级为「required only」即可。

已确认（抽查 + 全量 `go vet` / `go test`）：

- **SQL 注入**：postgres / mysql 两库全部查询走占位符参数；`fmt.Sprintf` 仅拼
  `$N` / `?` 序号与代码内固定的 `SET` 子句名，无用户数据进 SQL 文本。
- **指纹不可逆**：审计 actor 的 key 指纹与 v0.1.20 新增的 mTLS 客户端证书指纹
  均为 SHA-256 截断 12 hex，无法还原原值。
- **日志卫生**：审计 diff 截断 4KB，非法 JSON 包装为字符串存储，不放大日志体积；
  密钥类环境变量仅存在于 config 加载，不输出到结构化日志。
- **限流抗滥用**：桶表 65,536 上限，XFF 伪造大量 IP 时对新客户端 fail closed。
- **npm**：dashboard 与 docs 生产依赖 `npm audit --omit=dev` 均为 0 漏洞。

**发现（低危，记录在案）**：

- `Server.ID` 只做非空校验，无字符集约束。注册接口（token / mTLS 保护）调用方
  可提交含 `:`、空格或超长 ID，会原样进入 Redis key（`atlas:server:{id}:runtime`）
  与 SQL 主键。当前无越权或注入后果（RESP 二进制安全 + SQL 参数化），但建议后续
  版本收紧为 `[A-Za-z0-9._-]{1,128}`，与角色索引分片键的约定保持一致。

## 3. 自动化门禁

```yaml
# .github/workflows/security-audit.yml（每周一 + 手动触发）
govulncheck ./...                    # Go 标准库/依赖已知漏洞，发现即失败
npm audit --omit=dev --audit-level=high   # dashboard 与 docs，仅生产依赖，high+ 失败
```

依赖更新日常由 Dependabot 承担（patch 自动合并、minor/major 人工评审，
见 `dependabot-auto-merge.yml`）；本 workflow 是第二道独立复查。
