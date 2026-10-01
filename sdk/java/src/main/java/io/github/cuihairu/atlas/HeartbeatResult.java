package io.github.cuihairu.atlas;

import com.google.gson.annotations.SerializedName;

/** Reply from {@code POST /v1/registry/servers/{id}/heartbeat}. */
public class HeartbeatResult {

    @SerializedName("server_id")
    public String serverId = "";
    public String status = "";
    @SerializedName("next_heartbeat_in")
    public long nextHeartbeatIn;
}
