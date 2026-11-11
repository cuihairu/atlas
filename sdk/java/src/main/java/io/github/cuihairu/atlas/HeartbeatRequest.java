package io.github.cuihairu.atlas;

/** Registry heartbeat body (players / load). */
public class HeartbeatRequest {

    public int players;
    public double load;

    public HeartbeatRequest() {}

    public HeartbeatRequest(int players, double load) {
        this.players = players;
        this.load = load;
    }
}
