# 安全

v0.1.17 起 Atlas 内建四层防护：Registry mTLS、Admin RBAC、操作审计与限流。所有能力均为**可选启用**，默认行为与既往版本一致。

## 1. TLS / mTLS（Registry :8081 与 gRPC :9090）

两个面向内网的服务间监听口——REST 注册口与 gRPC 五服务口——共用同一套 TLS 语义，各自独立配置：配置证书后开启 TLS；再配置客户端 CA 即升级为**双向 TLS**——客户端必须出示受信任 CA 签发的证书。

```bash
# Registry 口
ATLAS_REGISTRY_TLS_CERT=/etc/atlas/server.crt.pem
ATLAS_REGISTRY_TLS_KEY=/etc/atlas/server.key.pem
ATLAS_REGISTRY_CLIENT_CA=/etc/atlas/client-ca-bundle.pem   # 可选：配置后为 mTLS

# gRPC 口
ATLAS_GRPC_TLS_CERT=/etc/atlas/server.crt.pem
ATLAS_GRPC_TLS_KEY=/etc/atlas/server.key.pem
ATLAS_GRPC_TLS_CLIENT_CA=/etc/atlas/client-ca-bundle.pem   # 可选：配置后为 mTLS
```

- 仅配 cert/key：服务器侧 TLS（客户端可校验服务端身份）。
- 追加 `*_CLIENT_CA`：`RequireAndVerifyClientCert`，未持证书的连接在握手层直接失败。
- 最低 TLS 1.2。证书/CA 材料无效时进程启动即报错退出（fail fast）。两个口可只开其一。
- 客户端对接：Go SDK gRPC 传输以 `GRPCTLS: true` + `GRPCTLSCACert`（PEM CA 束）开启；REST 传输照常走 `https://` 基地址。其余语言 SDK 目前仅提供 REST 传输（gRPC 传输为 Go SDK 独有），以 `https://` 基地址接 TLS 即可，不涉及 gRPC 口。

## 2. Admin RBAC

Admin API 按 API Key 授予三种角色，角色在中间件内解析并写入请求上下文：

| 角色 | 权限 |
| --- | --- |
| `admin` | 全部 Admin 端点（默认） |
| `operator` | 与 `admin` 等权：角色会被解析并写入请求上下文，但当前实现除 `viewer` 外没有差异化的端点强制（细分权限是预留位，规划中） |
| `viewer` | 仅读（GET/HEAD），任何变更返回 `403 ROLE_NOT_ALLOWED`——这是唯一被强制区分的角色 |

```bash
ATLAS_ADMIN_API_KEYS=key-read,key-write,key-full
ATLAS_ADMIN_ROLES=key-read:viewer,key-write:operator,key-full:admin
```

- 未出现在 `ATLAS_ADMIN_ROLES` 的 Key 默认 `admin`（向后兼容只配 `ATLAS_ADMIN_API_KEYS` 的部署）。
- 冒号缺失的条目按 admin 处理；未知角色名导致启动失败。
- **gRPC 同规**：AdminService 的 10 个 RPC（:9090）走同一套 Key + 角色，viewer 仅可调 4 个读 RPC（GetStats / GetMigration / ListMigrations / SearchCharacters），写 RPC 返回 gRPC `PermissionDenied ROLE_NOT_ALLOWED`。RegistryService 的 3 个 RPC 同样受 `ATLAS_REGISTRY_TOKENS` + IP 白名单保护。两条传输共用同一批环境变量。

## 3. 操作审计

开启后（默认开启，`ATLAS_AUDIT_ENABLED=0` 关闭），每一条 Admin 请求都会被记录：

- **actor**：`role:key指纹`（key 的 SHA-256 前 12 位十六进制，不可逆推原 key）；
- **timestamp**：RFC3339 纳秒时间；
- **method / path / query / status**；
- **diff**：变更类方法（POST/PUT/PATCH/DELETE）的请求体（上限 4KB，非法 JSON 包装为字符串存储）。

记录同时落入结构化日志（`slog.Info`，`msg=admin audit`）与内存环形缓冲（默认 1,000 条），并通过 `GET /v1/admin/audit?limit=50` 提供最近条目：

```json
{
  "entries": [
    {
      "time": "2026-10-01T12:00:00Z",
      "actor": "operator:3f8a1c2b9d0e",
      "method": "POST",
      "path": "/v1/admin/servers/game-1001/drain",
      "status": 200,
      "body": {"reason": "patch"}
    }
  ]
}
```

审计位于认证**之内**（拿到解析后的 actor）、限流**之外**；审计环不落盘，持久化交给日志采集管道（结构化日志行）。

**双传输**：gRPC（:9090）的 AdminService RPC 记入同一个环——`path` 为全方法名（如 `/atlas.v1.AdminService/Disable`），读 RPC 记 `GET`（无 diff）、写 RPC 记 `POST`（diff 为 protojson 请求，同受 4KB 上限），gRPC status code 按 google.rpc 惯例映射为等价 HTTP 状态入环；actor 同为 `role:key指纹`（校验由 gRPC 认证拦截器完成，未配置 Key 的开发模式显示 `:anonymous`）。Registry / Discovery / Directory / Routing 的 RPC 不进审计环（与 REST 只审计 `/v1/admin/*` 一致）。

## 4. 限流（令牌桶）

按**路径前缀**匹配规则（最长前缀优先）、按**客户端 IP** 分桶，单个失控的服务器打不垮邻居。默认关闭，显式配置后启用：

```bash
# 前缀=速率:突发（缺省突发=速率取整），多条用 ; 分隔
ATLAS_RATE_LIMITS="/v1/registry/servers/register=10:20;/v1/registry/=200:400;/v1/admin/=50:100"
ATLAS_RATE_LIMIT_DEFAULT="1000:2000"   # 未命中前缀的兜底规则
```

- 桶按 `客户端IP × 匹配到的规则` 隔离；令牌按时间连续补充，突发上限为 burst。
- 超限返回 `429 RATE_LIMITED` + `Retry-After: 1`。
- 桶表上限 65,536，防 XFF 伪造导致的内存膨胀（超出后拒绝新客户端，fail closed）。
- 规则解析错误启动即退出。

**双传输**：gRPC（:9090）与 REST 共用同一个限流器与桶表——按全方法名做前缀匹配（如 `/atlas.v1.RegistryService/Register=10:20`），未命中规则落 default 桶；Registry / Admin 两域受限，Public 三服务不设限（与公网口 :8080 一致）。拒绝返回 gRPC `ResourceExhausted` + `RATE_LIMITED` 消息（gRPC 无 `Retry-After` 头，按 code 退避），同一条拒绝计入 REST 视图 `GET /v1/admin/rate-limits` 的 `rejected_by_endpoint` / `rejected_by_client`——换传输逃不过桶。

## 配置速查

| 环境变量 | 默认 | 作用 |
| --- | --- | --- |
| `ATLAS_REGISTRY_TLS_CERT` / `_KEY` | （空） | Registry TLS 服务端证书 |
| `ATLAS_REGISTRY_CLIENT_CA` | （空） | 客户端 CA，配置即 mTLS |
| `ATLAS_GRPC_TLS_CERT` / `_KEY` | （空） | gRPC TLS 服务端证书 |
| `ATLAS_GRPC_TLS_CLIENT_CA` | （空） | gRPC 客户端 CA，配置即 mTLS |
| `ATLAS_ADMIN_ROLES` | （空） | Admin RBAC `key:role` 列表 |
| `ATLAS_AUDIT_ENABLED` | `1` | Admin 操作审计 |
| `ATLAS_RATE_LIMITS` | （空） | 按前缀的限流规则 |
| `ATLAS_RATE_LIMIT_DEFAULT` | （空） | 限流兜底规则 |
| `ATLAS_CORS_ORIGINS` | （空） | 跨域白名单（逗号分隔，公网/管理口生效）；空 = 不产生 CORS 头，`*` 仅供开发 |

速率建议：注册接口最严（低 RPS + 小突发），心跳次之（按服务器数 × 心跳频率估算），发现/目录读取最宽。

## 周期审计

依赖漏洞扫描（`govulncheck` + npm audit）由 CI 每周自动执行；逐层人工复查清单与
历次审计结论见 [security-audit.md](security-audit)。
