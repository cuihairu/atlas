# Atlas APISIX 插件（TODO v0.1.13）

APISIX 网关侧的 Atlas 接入插件：

| 插件 | 职责 |
| --- | --- |
| `atlas-auth.lua` | 校验玩家 token，把玩家身份注入 Atlas 请求头（`X-Atlas-Player-ID`），可选剥离玩家 token 后再转发 |
| `atlas-ratelimit.lua` | 按 Atlas 端点组（discovery / directory / routing / registry）差异化限流，单路由覆盖整个 `/v1` 面 |

## 安装

1. 把两个 `.lua` 拷进 APISIX 插件搜索路径（默认 `apisix/plugins/`，或通过
   `APISIX_CUSTOM_PLUGINS` / `lua_module_hook` 指向自定义目录）：

   ```bash
   cp atlas-auth.lua atlas-ratelimit.lua /usr/local/apisix/apisix/plugins/
   ```

2. 在 APISIX `config.yaml` 中启用并声明限流共享字典：

   ```yaml
   plugins:
     - atlas-auth          # 加到现有插件列表
     - atlas-ratelimit

   nginx_config:
     http:
       shared_dicts:
         - atlas_ratelimit: 10m
   ```

3. 重载 APISIX：`apisix reload`。

## 路由配置

一条路由即可把整个 `/v1` 转给 Atlas 公网端口，两个插件分别处理鉴权与限流
（完整示例见 [`config-example.yaml`](config-example.yaml)）：

```yaml
uri: /v1/*
upstream:
  nodes: { "atlas:8080": 1 }
plugins:
  atlas-auth:
    token_header: X-Player-Token
    account_header: X-Atlas-Player-ID
    required: true
    strip_token: true
    accounts:
      demo-token-alice: 1001
  atlas-ratelimit:
    groups:
      - { prefix: /v1/routing,   rate: 1000, window: 60 }
      - { prefix: /v1/discovery, rate: 600,  window: 60 }
      - { prefix: /v1/registry,  rate: 50,   window: 60 }
    default_rate: 0
```

### atlas-auth 配置

| 键 | 默认 | 说明 |
| --- | --- | --- |
| `token_header` | `X-Player-Token` | 玩家 token 头；`Authorization: Bearer <token>` 同样接受 |
| `account_header` | `X-Atlas-Player-ID` | 注入 Atlas 的身份头 |
| `accounts` | — | token → account id 静态映射；生产环境应替换为自身 token 服务的校验（插件提供挂载点，业务无侵入） |
| `required` | `true` | 缺 token / 未知 token 返回 401；`false` 时匿名放行（Directory 浏览类接口） |
| `strip_token` | `false` | 转发前剥离玩家 token 头 |

### atlas-ratelimit 配置

| 键 | 默认 | 说明 |
| --- | --- | --- |
| `groups` | `[]` | `{prefix, rate, window}` 数组；**最长前缀匹配**，固定窗口计数 |
| `default_rate` | `0` | 未匹配组的兜底配额（0 = 不限） |
| `rejected_code` | `429` | 超限状态码 |

计数键 = 组前缀 + 客户端 IP；共享字典缺失时**失败放行**（fail open）并记
ERROR 日志，限流故障不阻断网关。

## 验证

```bash
# 命中限流组：快速打满 discovery 配额
for i in $(seq 1 700); do
  curl -s -o /dev/null -w "%{http_code} " -H "X-Player-Token: demo-token-alice" \
    http://127.0.0.1:9080/v1/discovery/servers
done

# 鉴权：无 token / 假 token
curl -i http://127.0.0.1:9080/v1/directory/accounts/1001/characters   # → 401
curl -i -H "X-Player-Token: demo-token-alice" \
  http://127.0.0.1:9080/v1/directory/accounts/1001/characters         # → 200
```

## 测试

插件逻辑不依赖 OpenResty 即可测试（mock `apisix.core` / `ngx`）：

```bash
cd plugins/apisix
lua test/run_tests.lua     # 13 例：注入/401/Bearer/剥离/限流组/最长前缀/失败放行
luac -p *.lua test/*.lua   # 语法检查
```

## 与 Atlas 三端口拓扑的关系

APISIX 作为公网入口反代 Atlas public 端口（:8080）。Registry（:8081）与
Admin（:8082）是服务内部端口，不挂在公网路由上；Registry 的心跳扇入 LB
见 TODO v0.1.19（HAProxy TCP 模式 / APISIX Stream 代理）。
