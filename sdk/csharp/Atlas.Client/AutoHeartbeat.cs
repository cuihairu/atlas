namespace Atlas;

/// <summary>Background heartbeat loop: one beat fires immediately on start,
/// then one every interval. Payload updates via <see cref="Set"/> are safe
/// from any thread. Report interval : suspect/offline thresholds should stay
/// 1:3 (e.g. 10s : 30s : 60s) — a single missed beat stays well inside the
/// suspect window.</summary>
public sealed class AutoHeartbeat : IDisposable, IAsyncDisposable
{
    private readonly AtlasClient _client;
    private readonly string _serverId;
    private readonly Timer _timer;
    private readonly object _lock = new();
    private HeartbeatRequest _payload;
    private Action<AtlasError>? _onError;
    private int _stopped;
    private int _inflight;
    private readonly ManualResetEventSlim _idle = new(initialState: true);

    internal AutoHeartbeat(AtlasClient client, string serverId,
        TimeSpan interval, HeartbeatRequest payload, Action<AtlasError>? onError = null)
    {
        _client = client;
        _serverId = serverId;
        _payload = payload;
        _onError = onError;
        // dueTime zero → first beat fires immediately.
        _timer = new Timer(static s => ((AutoHeartbeat)s!).Beat(),
            this, TimeSpan.Zero, interval);
    }

    /// <summary>Updates the load payload reported by subsequent beats.</summary>
    public void Set(int players, double load)
    {
        lock (_lock)
        {
            _payload = new HeartbeatRequest(players, load);
        }
    }

    /// <summary>Registers a handler invoked with the last error whenever a
    /// beat fails; the loop keeps running.</summary>
    public void OnError(Action<AtlasError> handler)
    {
        lock (_lock)
        {
            _onError = handler;
        }
    }

    /// <summary>Stops the loop, then waits (bounded at 10s) for any
    /// in-flight beat to land, so no heartbeat arrives after Stop
    /// returns — graceful-shutdown parity with the Go SDK
    /// (<c>HeartbeatLoop.Stop</c> waits for its run loop to exit).
    /// Idempotent.</summary>
    public void Stop()
    {
        if (Interlocked.Exchange(ref _stopped, 1) == 1) return;
        _timer.Change(Timeout.InfiniteTimeSpan, Timeout.InfiniteTimeSpan);
        _timer.Dispose();
        // A timer callback may already be mid-beat; waiting keeps a
        // straggler from resurrecting the server after unregister.
        _idle.Wait(TimeSpan.FromSeconds(10));
        _idle.Dispose();
    }

    public void Dispose() => Stop();

    public ValueTask DisposeAsync()
    {
        Stop();
        return ValueTask.CompletedTask;
    }

    private void Beat()
    {
        HeartbeatRequest payload;
        Action<AtlasError>? onError;
        lock (_lock)
        {
            // Stop() may have run between the timer firing and this
            // callback taking the lock — bail so no straggler beat
            // lands after Stop returned.
            if (_stopped == 1) return;
            payload = _payload;
            onError = _onError;
            _idle.Reset(); // busy while at least this beat is in flight
            _inflight++;
        }
        // BeatAsync swallows every failure into the handler, so dropping
        // the Task is safe (it never faults).
        _ = BeatAsync(payload, onError);
    }

    private async Task BeatAsync(HeartbeatRequest payload, Action<AtlasError>? onError)
    {
        try
        {
            await _client.HeartbeatAsync(_serverId, payload).ConfigureAwait(false);
        }
        catch (Exception ex)
        {
            onError?.Invoke(ex as AtlasError ??
                new AtlasError(0, "NETWORK", ex.Message));
        }
        finally
        {
            lock (_lock)
            {
                if (--_inflight == 0) _idle.Set();
            }
        }
    }
}
