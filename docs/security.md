# 安全

v0.1.17 起 Atlas 内建四层防护：Registry mTLS、Admin RBAC、操作审计与限流。所有能力均为**可选启用**，默认行为与既往版本一致。

## 1. Registry mTLS（服务间）

Registry API 面向内网游戏服务器，配置证书后开启 TLS；再配置客户端 CA 即升级为**双向 TLS**——客户端必须出示受信任 CA 签发的证书。

```bash
ATLAS_REGISTRY_TLS_CERT=/etc/atlas/server.crt.pem
ATLAS_REGISTRY_TLS_KEY=/etc/atlas/server.key.pem
ATLAS_REGISTRY_CLIENT_CA=/etc/atlas/client-ca-bundle.pem   # 可选：配置后为 mTLS
```

- 仅配 cert/key：服务器侧 TLS（客户端可校验服务端身份）。
- 追加 `ATLAS_REGISTRY_CLIENT_CA`：`RequireAndVerifyClientCert`，未持证书的连接在握手层直接失败。
- 最低 TLS 1.2。证书/CA 材料无效时进程启动即报错退出（fail fast）。

## 2. Admin RBAC

Admin API 按 API Key 授予三种角色，角色在中间件内解析并写入请求上下文：

| 角色 | 权限 |
| --- | --- |
| `admin` | 全部 Admin 端点（默认） |
| `operator` | 读全部 + 生命周期变更（maintenance/drain/enable/disable、迁移、realm/shard 创建） |
| `viewer` | 仅读（GET/HEAD），任何变更返回 `403 ROLE_NOT_ALLOWED` |

```bash
ATLAS_ADMIN_API_KEYS=key-read,key-write,key-full
ATLAS_ADMIN_ROLES=key-read:viewer,key-write:operator,key-full:admin
```

- 未出现在 `ATLAS_ADMIN_ROLES` 的 Key 默认 `admin`（向后兼容只配 `ATLAS_ADMIN_API_KEYS` 的部署）。
- 冒号缺失的条目按 admin 处理；未知角色名导致启动失败。

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

## 配置速查

| 环境变量 | 默认 | 作用 |
| --- | --- | --- |
| `ATLAS_REGISTRY_TLS_CERT` / `_KEY` | （空） | Registry TLS 服务端证书 |
| `ATLAS_REGISTRY_CLIENT_CA` | （空） | 客户端 CA，配置即 mTLS |
| `ATLAS_ADMIN_ROLES` | （空） | Admin RBAC `key:role` 列表 |
| `ATLAS_AUDIT_ENABLED` | `1` | Admin 操作审计 |
| `ATLAS_RATE_LIMITS` | （空） | 按前缀的限流规则 |
| `ATLAS_RATE_LIMIT_DEFAULT` | （空） | 限流兜底规则 |

速率建议：注册接口最严（低 RPS + 小突发），心跳次之（按服务器数 × 心跳频率估算），发现/目录读取最宽。

## 周期审计

依赖漏洞扫描（`govulncheck` + npm audit）由 CI 每周自动执行；逐层人工复查清单与
历次审计结论见 [security-audit.md](security-audit)。
