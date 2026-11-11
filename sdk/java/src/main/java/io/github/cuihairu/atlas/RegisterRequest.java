package io.github.cuihairu.atlas;

import com.google.gson.annotations.SerializedName;

import java.util.LinkedHashMap;
import java.util.Map;

/** Registry register request body. */
public class RegisterRequest {

    @SerializedName("server_id")
    public String serverId;
    public String name;
    /** defaults to "game" */
    public String type = "game";
    public String region = "";
    public String version = "";
    public String platform = "";
    public Endpoint endpoint = new Endpoint();
    public int capacity;
    @SerializedName("realm_id")
    public String realmId;
    @SerializedName("shard_id")
    public String shardId;
    public Map<String, String> metadata;

    public RegisterRequest() {}

    public RegisterRequest(String serverId, String name, String region,
                           Endpoint endpoint, int capacity) {
        this.serverId = serverId;
        this.name = name;
        this.region = region;
        this.endpoint = endpoint;
        this.capacity = capacity;
    }
}
