# atlas-client (Java)

Atlas 游戏服务器 Registry / Discovery / Directory / Routing / Admin 的
Java 客户端 SDK，与 [Go](../go/) / [C++](../cpp/) / [Python](../python/) /
[JS](../js/) SDK 同一 API 面：

- OkHttp + Gson，Java 17+，同步阻塞接口
- 五组 API 全量方法（注册心跳 / 发现 / 角色目录 / 推荐接入 / 管理）
- 瞬时失败重试（网络错误 + 5xx，全抖动指数退避；4xx 不重试）
- 自动心跳：`startHeartbeat()` 立即首发、按间隔续报，`set()` 更新负载
- Registry 独立端口拆分（`registryBaseUrl`）
- Maven + Gradle 双构建配置，发布坐标 `io.github.cuihairu:atlas-client`

## 安装

```xml
<dependency>
  <groupId>io.github.cuihairu</groupId>
  <artifactId>atlas-client</artifactId>
  <version>0.1.10</version>
</dependency>
```

Gradle：

```groovy
implementation 'io.github.cuihairu:atlas-client:0.1.10'
```

## 快速上手

```java
try (AtlasClient client = new AtlasClient(new AtlasClientOptions()
        .setBaseUrl("http://localhost:8080")
        .setRegistryBaseUrl("http://localhost:8081")  // Registry 独立端口（可选）
        .setRegistryToken("...")                      // Registry 域 Bearer
        .setAdminApiKey("..."))) {                    // Admin 域 API Key

    // 1. 注册并开启自动心跳（立即首发，之后每 10s 一次）
    RegisterRequest req = new RegisterRequest(
            "game-1001", "Game 1001", "cn-east",
            new Endpoint("10.0.0.1", 30001), 2000);
    var reg = client.register(req);

    client.heartbeat("game-1001", new HeartbeatRequest()); // 首跳同步：在线后才可被发现
    AutoHeartbeat loop = client.startHeartbeat("game-1001", 10_000,
            new HeartbeatRequest(), err -> log.warning(err.getMessage()));
    loop.set(120, 0.35); // 游戏线程随负载更新

    // 2. 发现与目录
    Recommendation rec = client.recommend(42, "cn-east", null, null);
    CharacterPage page = client.listCharactersByServer(rec.server.id, 10, "");
    var wr = client.createCharacter(new CreateCharacterRequest(
            42, rec.server.id, 1001, "Hero"));

    // 3. 优雅下线
    loop.stop();
    client.unregister("game-1001");
}
```

所有失败都会抛出 `AtlasError`（unchecked，`getStatus()` /
`getCode()` / `getMessage()`，status=0 表示网络错误）。完整示例见
[`examples/java`](../../examples/java/)，文档见
<https://cuihairu.github.io/atlas/sdk-java>。

## 开发

```bash
mvn test        # 或 gradle test
```
