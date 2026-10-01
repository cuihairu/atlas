# @cuihairu/atlas-client (JavaScript / TypeScript)

Atlas 游戏服务器 Registry / Discovery / Directory / Routing / Admin 的
JS/TS 客户端 SDK，与 [Go](../go/) / [C++](../cpp/) / [Python](../python/)
SDK 同一 API 面：

- 基于 `fetch`，零运行时依赖，Node.js 18+ 与现代浏览器通用
- 五组 API 全量方法（注册心跳 / 发现 / 角色目录 / 推荐接入 / 管理）
- 瞬时失败重试（网络错误 + 超时 + 5xx，全抖动指数退避；4xx 不重试）
- 自动心跳：`startHeartbeat()` 立即首发、按间隔续报，`set()` 更新负载
- Registry 独立端口拆分（`registryBaseUrl`）
- 双格式产物：ESM + CJS（`exports` 自动选择），含 TypeScript 类型

## 安装

```bash
npm install @cuihairu/atlas-client
```

## 快速上手

```ts
import { AtlasClient } from "@cuihairu/atlas-client";

const client = new AtlasClient({
  baseUrl: "http://localhost:8080",
  registryBaseUrl: "http://localhost:8081", // Registry 独立端口（可选）
  registryToken: "...",                     // Registry 域 Bearer
  adminApiKey: "...",                       // Admin 域 API Key
});

// 1. 注册并开启自动心跳（立即首发，之后每 10s 一次）
const reg = await client.register({
  serverId: "game-1001",
  name: "Game 1001",
  region: "cn-east",
  endpoint: { host: "10.0.0.1", port: 30001 },
  capacity: 2000,
});
const loop = client.startHeartbeat("game-1001", { intervalMs: 10_000 });
loop.set(120, 0.35); // 游戏线程随负载更新

// 2. 发现与目录
const rec = await client.recommend({ accountId: 42, region: "cn-east" });
const page = await client.listCharactersByServer(rec.server.id, 10);
const wr = await client.createCharacter({
  accountId: 42,
  serverId: rec.server.id,
  characterId: 1001,
  name: "Hero",
  level: 1,
});

// 3. 优雅下线
loop.stop();
await client.unregister("game-1001");
client.close();
```

所有失败都会抛出 `AtlasError`（`status` / `code` / `message`，`status=0`
表示网络错误）。完整示例见 [`examples/js`](../../examples/js/)，文档见
<https://cuihairu.github.io/atlas/sdk-js>。

## 开发

```bash
npm install
npm run build   # dist/esm + dist/cjs
npm test        # node --test（Node 22.18+ 直接跑 TS 源码，零额外依赖）
```
