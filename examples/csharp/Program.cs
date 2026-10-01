using System.Runtime.InteropServices;
using Atlas;

// Atlas C# SDK example: register a game server, start auto-heartbeat,
// then look up characters and leave cleanly.
//
// Run against a local Atlas (default ports):
//
//   dotnet run --project examples/csharp
//
// Env: ATLAS_ADDR (default http://localhost:8080),
//      ATLAS_REGISTRY_ADDR (e.g. http://localhost:8081 for the split
//      registry port), ATLAS_REGISTRY_TOKEN, SERVER_ID.

var addr = Environment.GetEnvironmentVariable("ATLAS_ADDR") is { Length: > 0 } a
    ? a : "http://localhost:8080";
var registryAddr = Environment.GetEnvironmentVariable("ATLAS_REGISTRY_ADDR");
var registryToken = Environment.GetEnvironmentVariable("ATLAS_REGISTRY_TOKEN") ?? "";
var serverId = Environment.GetEnvironmentVariable("SERVER_ID") is { Length: > 0 } s
    ? s : "demo-game-1";

// RegistryBaseUrl points at the split registry port when not behind a
// merged proxy (local dev: ATLAS_REGISTRY_ADDR=http://localhost:8081).
var options = new AtlasClientOptions
{
    BaseUrl = addr,
    RegistryBaseUrl = registryAddr,
    RegistryToken = registryToken,
};

using var client = new AtlasClient(options);

// 1. Register this server.
var reg = await client.RegisterAsync(new RegisterRequest(
    serverId, "Demo Game Server", "cn-east",
    new Endpoint { Host = "10.0.0.1", Port = 30001 }, 2000)
{ Type = "game", Version = "1.0.0", Platform = "any" });
Console.WriteLine($"registered: {reg.ServerId} (status={reg.Status})");

// 2. Heartbeat: one synchronous beat first — discovery/routing only see
// this server once it is online — then the auto loop every 10s.
await client.HeartbeatAsync(serverId, new HeartbeatRequest());
var loop = client.StartHeartbeat(serverId, TimeSpan.FromSeconds(10),
    onError: e => Console.Error.WriteLine($"heartbeat failed: {e}"));

// PosixSignalRegistration works with redirected stdin too (unlike
// Console.CancelKeyPress, which is not raised without a tty).
using var cts = new CancellationTokenSource();
using var sigint = PosixSignalRegistration.Create(PosixSignal.SIGINT, _ => cts.Cancel());
using var sigterm = PosixSignalRegistration.Create(PosixSignal.SIGTERM, _ => cts.Cancel());

try
{
    // 3. Discovery: where should account 42 play?
    var rec = await client.RecommendAsync(42, "cn-east");
    Console.WriteLine($"account 42 → {rec.Server.Id} ({rec.Reason})");

    // 4. Directory: characters on that server.
    var page = await client.ListCharactersByServerAsync(rec.Server.Id, limit: 10);
    foreach (var ch in page.Characters)
        Console.WriteLine($"  character {ch.CharacterId} {ch.Name} lv{ch.Level}");
}
catch (Exception ex)
{
    Console.Error.WriteLine($"lookup failed: {ex.Message}");
}

// 5. Stay up until signalled (Ctrl+C / SIGTERM), then shut down gracefully.
// ATLAS_RUN_SECONDS exits after a fixed delay instead (CI / non-tty runs,
// where signal delivery to background processes is unreliable).
if (int.TryParse(Environment.GetEnvironmentVariable("ATLAS_RUN_SECONDS"), out var runSeconds)
    && runSeconds > 0)
{
    await Task.Delay(TimeSpan.FromSeconds(runSeconds));
}
else
{
    try { await Task.Delay(Timeout.Infinite, cts.Token); }
    catch (OperationCanceledException) { }
}

Console.WriteLine("shutting down…");
loop.Stop();
try { await client.UnregisterAsync(serverId); }
catch (Exception ex) { Console.Error.WriteLine($"unregister: {ex.Message}"); }
