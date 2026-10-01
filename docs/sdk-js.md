# JavaScript/TypeScript SDK

`sdk/js/`（TODO v0.1.9）。基于 `fetch` 的 REST 客户端，与 Go / C++ /
Python SDK 同一 API 面。零运行时依赖，Node.js 18+ 与现代浏览器通用；
npm 产物 ESM + CJS 双格式，含完整 TypeScript 类型。

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

await client.register({                     // 注册
  serverId: "game-1001", name: "Game 1001",
  region: "cn-east", capacity: 2000,
  endpoint: { host: "10.0.0.1", port: 30001 },
});
await client.heartbeat("game-1001", {});    // 首跳同步：在线后才可被发现
const loop = client.startHeartbeat("game-1001", { intervalMs: 10_000 }); // 自动心跳
loop.set(120, 0.35);                        // 游戏线程更新负载

const rec = await client.recommend({ accountId: 42, region: "cn-east" }); // 接入推荐
const page = await client.listCharactersByServer(rec.server.id, 10);      // 角色目录
const wr = await client.createCharacter({
  accountId: 42, serverId: rec.server.id,
  characterId: 1001, name: "Hero", level: 1,
});

loop.stop();                                // 优雅下线
await client.unregister("game-1001");
client.close();
```

## 浏览器

`fetch` / `AbortSignal` / `URL` 全部为平台内置，打包器（Vite / webpack /
esbuild）直接可用；推荐接入、服务器发现、角色目录等公开端点天然适合
客户端调用，Registry / Admin 域请勿把令牌下发到浏览器。

完整示例见 [`examples/js`](https://github.com/cuihairu/atlas/blob/main/examples/js/main.ts)：

```bash
cd examples/js && npm install && npm start   # Node 22.18+ 直接运行 TS
ATLAS_ADDR=http://localhost:8080 ATLAS_REGISTRY_ADDR=http://localhost:8081 npm start
```

## 关键语义

| 主题 | 说明 |
| --- | --- |
| 错误处理 | 所有方法失败抛 `AtlasError`（`status` / `code` / `message`），code 与 REST 错误码一致，`status=0` 为网络错误 |
| 重试 | 网络错误 + 超时 + 5xx 自动全抖动指数退避（`maxRetries` 默认 3）；4xx 不重试 |
| 超时 | 每次尝试独立 `AbortSignal.timeout`（`timeoutMs` 默认 10000，0 关闭） |
| 心跳节奏 | 上报间隔 : 判死阈值 = 1:3（默认 10s / suspect 30s / offline 60s）；示例先同步首发再开循环，规避"注册未在线即推荐"竞态 |
| 自动心跳 | `AutoHeartbeat`：立即首发、按间隔续报；`onError` 回调上报失败，循环不停；`stop()` 幂等 |
| 目录写回复 | 同步 REST 返回扁平 Character 对象（SDK 归一化为 `status="created"/"updated"`）；异步适配器返回 `{"status":"queued"}`（`character` 为 undefined） |
| 三端口 | `baseUrl` 覆盖公网/Admin；`registryBaseUrl` 覆盖 Registry 独立端口；反代合并部署时两者同值即可 |
| 空集合 | 服务端空列表可能序列化为 `null`，SDK 按 `[]` 处理 |
| 命名 | 传输层 snake_case，SDK 层 camelCase（`parseServer` 等转换函数亦可独立使用） |
