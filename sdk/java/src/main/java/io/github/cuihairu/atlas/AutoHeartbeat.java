package io.github.cuihairu.atlas;

import java.util.concurrent.Executors;
import java.util.concurrent.ScheduledExecutorService;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.Consumer;

/** Background heartbeat loop (immediate first report, then every
 * {@code intervalMs}). Update the payload from the game thread with
 * {@link #set(int, double)}; failures are surfaced through the error
 * consumer and the loop keeps running. */
public class AutoHeartbeat {

    private static final long DEFAULT_INTERVAL_MS = 10_000;

    private final AtlasClient client;
    private final String serverId;
    private final long intervalMs;
    private final AtomicReference<HeartbeatRequest> payload;
    private volatile Consumer<AtlasError> onError;
    private final ScheduledExecutorService executor;
    private volatile boolean stopped;

    AutoHeartbeat(AtlasClient client, String serverId, long intervalMs,
                  HeartbeatRequest initial, Consumer<AtlasError> onError) {
        this.client = client;
        this.serverId = serverId;
        this.intervalMs = intervalMs > 0 ? intervalMs : DEFAULT_INTERVAL_MS;
        this.payload = new AtomicReference<>(initial);
        this.onError = onError;
        this.executor = Executors.newSingleThreadScheduledExecutor(runnable -> {
            Thread t = new Thread(runnable, "atlas-heartbeat-" + serverId);
            t.setDaemon(true);
            return t;
        });
        // First report runs immediately, then on a fixed cadence.
        this.executor.scheduleAtFixedRate(this::beat, 0, this.intervalMs, TimeUnit.MILLISECONDS);
    }

    /** Update the payload sent on the next beat (players, load). */
    public void set(int players, double load) {
        payload.set(new HeartbeatRequest(players, load));
    }

    /** Replace the failure callback. */
    public void setOnError(Consumer<AtlasError> onError) {
        this.onError = onError;
    }

    /** Stop the loop; idempotent. */
    public void stop() {
        stopped = true;
        executor.shutdownNow();
    }

    private void beat() {
        if (stopped) {
            return;
        }
        try {
            client.heartbeat(serverId, payload.get());
        } catch (AtlasError e) {
            Consumer<AtlasError> handler = onError;
            if (handler != null) {
                handler.accept(e);
            }
        }
    }
}
