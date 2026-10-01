using System.Collections.Immutable;
using System.Net;
using System.Net.Sockets;
using System.Text;

namespace Atlas.Client.Tests;

/// <summary>Minimal fake Atlas: a TcpListener speaking just enough HTTP/1.1
/// (one request per connection, Connection: close) for the SDK tests.
/// Routes are predicate handlers run in registration order.</summary>
internal sealed class FakeAtlas : IDisposable
{
    public sealed record Req(
        string Method,
        string Path,
        string Query,
        string? Body,
        ImmutableDictionary<string, string> Headers)
    {
        /// <summary>Prefix match on purpose: Content-Type arrives as
        /// "application/json; charset=utf-8" from HttpClient.</summary>
        public bool HasHeader(string name, string value) =>
            Headers.TryGetValue(name, out var v) &&
            v.StartsWith(value, StringComparison.OrdinalIgnoreCase);
    }

    public sealed record Reply(int Status, string Json);

    private sealed class Route(Func<Req, bool> match, Func<Req, Reply> reply)
    {
        public bool Match(Req r) => match(r);
        public Reply Reply(Req r) => reply(r);
    }

    private readonly TcpListenerEx _listener;
    private readonly List<Route> _routes = [];
    private readonly object _lock = new();
    private readonly List<Req> _requests = [];
    private int _failNext; // force this many 503s before the next route reply

    public FakeAtlas()
    {
        _listener = new TcpListenerEx(IPAddress.Loopback, 0);
        _listener.Start();
        _ = Task.Run(AcceptLoopAsync);
    }

    public string BaseUrl => $"http://127.0.0.1:{_listener.BoundPort}";

    public ImmutableList<Req> Requests
    {
        get { lock (_lock) return [.. _requests]; }
    }

    public int RequestCount => Requests.Count;

    /// <summary>Forces the next <paramref name="n"/> requests to get a 503
    /// before normal routing (retry tests).</summary>
    public void FailNext(int n)
    {
        lock (_lock) _failNext += n;
    }

    public void ClearRequests()
    {
        lock (_lock) _requests.Clear();
    }

    private int TakeFail()
    {
        lock (_lock)
        {
            if (_failNext > 0) { _failNext--; return 503; }
            return 0;
        }
    }

    public void On(Func<Req, bool> match, Func<Req, Reply> reply) =>
        _routes.Add(new Route(match, reply));

    public void On(Func<Req, bool> match, string json) =>
        _routes.Add(new Route(match, _ => new Reply(200, json)));

    private async Task AcceptLoopAsync()
    {
        while (true)
        {
            TcpClient client;
            try { client = await _listener.AcceptTcpClientAsync(); }
            catch (ObjectDisposedException) { return; }
            _ = Task.Run(() => ServeAsync(client));
        }
    }

    private async Task ServeAsync(TcpClient client)
    {
        using (client)
        await using (var stream = client.GetStream())
        {
            var req = await ReadRequestAsync(stream);
            if (req is null) return;
            lock (_lock) _requests.Add(req);

            Reply reply;
            int force = TakeFail();
            if (force != 0)
            {
                reply = new Reply(force, """{"code":"UNAVAILABLE","message":"forced"}""");
            }
            else
            {
                reply = _routes.FirstOrDefault(r => r.Match(req))?.Reply(req)
                    ?? new Reply(404, """{"code":"NOT_FOUND","message":"no route"}""");
            }

            var body = Encoding.UTF8.GetBytes(reply.Json);
            var head = Encoding.ASCII.GetBytes(
                $"HTTP/1.1 {reply.Status} X\r\n" +
                "Content-Type: application/json\r\n" +
                $"Content-Length: {body.Length}\r\n" +
                "Connection: close\r\n\r\n");
            await stream.WriteAsync(head);
            await stream.WriteAsync(body);
        }
    }

    private static async Task<Req?> ReadRequestAsync(NetworkStream stream)
    {
        var buf = new MemoryStream();
        var chunk = new byte[4096];
        int headEnd = -1, total = 0;

        // Read until end of headers.
        while (headEnd < 0)
        {
            int n = await stream.ReadAsync(chunk);
            if (n == 0) return null;
            buf.Write(chunk, 0, n);
            total += n;
            headEnd = IndexOfHeadEnd(buf.GetBuffer().AsSpan(0, total));
        }

        var raw = buf.GetBuffer().AsSpan(0, total).ToArray();
        var headText = Encoding.ASCII.GetString(raw, 0, headEnd);
        var lines = headText.Split("\r\n");
        var parts = lines[0].Split(' ');
        var method = parts[0];
        var target = parts.Length > 1 ? parts[1] : "/";

        var headers = new Dictionary<string, string>(StringComparer.OrdinalIgnoreCase);
        foreach (var line in lines.Skip(1))
        {
            int idx = line.IndexOf(':');
            if (idx > 0)
                headers[line[..idx].Trim()] = line[(idx + 1)..].Trim();
        }

        int contentLength = headers.TryGetValue("Content-Length", out var cl)
            ? int.Parse(cl) : 0;
        var bodyBytes = raw.Skip(headEnd + 4).Take(contentLength).ToArray();
        while (bodyBytes.Length < contentLength)
        {
            int n = await stream.ReadAsync(chunk);
            if (n == 0) break;
            bodyBytes = [.. bodyBytes, .. chunk[..n]];
        }

        string? body = bodyBytes.Length > 0
            ? Encoding.UTF8.GetString(bodyBytes) : null;

        var (path, query) = SplitTarget(target);
        return new Req(method, path, query, body, headers.ToImmutableDictionary());
    }

    private static int IndexOfHeadEnd(ReadOnlySpan<byte> span)
    {
        for (int i = 0; i + 3 < span.Length; i++)
            if (span[i] == 13 && span[i + 1] == 10 && span[i + 2] == 13 && span[i + 3] == 10)
                return i;
        return -1;
    }

    private static (string Path, string Query) SplitTarget(string target)
    {
        int q = target.IndexOf('?');
        return q < 0 ? (target, "") : (target[..q], target[(q + 1)..]);
    }

    public void Dispose() => _listener.Stop();

    /// <summary>TcpListener that remembers the port it bound (port 0 case).</summary>
    private sealed class TcpListenerEx(IPAddress addr, int port) : TcpListener(addr, port)
    {
        public int BoundPort => ((IPEndPoint)LocalEndpoint).Port;
    }
}
