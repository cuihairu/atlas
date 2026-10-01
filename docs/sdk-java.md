# Java SDK

`sdk/java/`（v0.1.10 交付）。OkHttp + Gson 的 REST 客户端，与 Go / C++ /
Python / JS SDK 同一 API 面。Java 17+，同步阻塞接口，发布坐标
`io.github.cuihairu:atlas-client`，Maven + Gradle 双构建配置。

## 安装

```xml
<dependency>
  <groupId>io.github.cuihairu</groupId>
  <artifactId>atlas-client</artifactId>
  <version>0.1.10</version>
</dependency>
```

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

    var reg = client.register(new RegisterRequest(      // 注册
            "game-1001", "Game 1001", "cn-east",
            new Endpoint("10.0.0.1", 30001), 2000));

    client.heartbeat("game-1001", new HeartbeatRequest()); // 首跳同步：在线后才可被发现
    AutoHeartbeat loop = client.startHeartbeat("game-1001", 10_000, // 自动心跳
            new HeartbeatRequest(), err -> log.warning(err.getMessage()));
    loop.set(120, 0.35);                                // 游戏线程更新负载

    Recommendation rec = client.recommend(42, "cn-east", null, null);   // 接入推荐
    CharacterPage page = client.listCharactersByServer(rec.server.id, 10, ""); // 角色目录
    var wr = client.createCharacter(new CreateCharacterRequest(
            42, rec.server.id, 1001, "Hero"));

    loop.stop();                                        // 优雅下线
    client.unregister("game-1001");
}
```

完整示例见 [`examples/java`](https://github.com/cuihairu/atlas/blob/main/examples/java/src/main/java/io/github/cuihairu/example/AtlasExample.java)：

```bash
mvn install -f sdk/java/pom.xml          # SDK 进本地仓库
cd examples/java
ATLAS_ADDR=http://localhost:8080 ATLAS_REGISTRY_ADDR=http://localhost:8081 mvn exec:java
```

## 关键语义

| 主题 | 说明 |
| --- | --- |
| 错误处理 | 所有方法失败抛 `AtlasError`（unchecked：`getStatus()` / `getCode()` / `getMessage()`），code 与 REST 错误码一致，status=0 为网络错误 |
| 重试 | 网络错误 + 5xx 自动全抖动指数退避（`maxRetries` 默认 3）；4xx 不重试 |
| 超时 | OkHttp `callTimeout`（`timeoutMs` 默认 10000，0 关闭） |
| 心跳节奏 | 上报间隔 : 判死阈值 = 1:3（默认 10s / suspect 30s / offline 60s）；示例先同步首发再开循环，规避"注册未在线即推荐"竞态 |
| 自动心跳 | `AutoHeartbeat`：守护线程 + `scheduleAtFixedRate` 立即首发；`set()` 原子更新负载；`onError` 回调上报失败，循环不停；`stop()` 幂等 |
| 目录写回复 | 同步 REST 返回扁平 Character 对象（SDK 归一化为 `status="created"/"updated"`）；异步适配器返回 `{"status":"queued"}`（`character` 为 null） |
| 三端口 | `baseUrl` 覆盖公网/Admin；`registryBaseUrl` 覆盖 Registry 独立端口；反代合并部署时两者同值即可 |
| 空集合 | 服务端空列表可能序列化为 `null`，SDK 按 `[]` 处理；显式 null 字段同样兜底 |
| 命名 | 传输层 snake_case，SDK 层 camelCase（Gson `@SerializedName` 双向映射），时间戳为 `java.time.Instant` |
