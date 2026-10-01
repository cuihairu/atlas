package io.github.cuihairu.atlas;

import com.google.gson.annotations.SerializedName;

import java.time.Instant;
import java.util.LinkedHashMap;
import java.util.Map;

/** A registered game server (discovery / registry view). */
public class Server {

    @SerializedName("id")
    public String id = "";

    @SerializedName("name")
    public String name = "";

    @SerializedName("type")
    public String type = "";

    @SerializedName("region")
    public String region = "";

    @SerializedName("realm_id")
    public String realmId;

    @SerializedName("shard_id")
    public String shardId;

    @SerializedName("version")
    public String version = "";

    @SerializedName("platform")
    public String platform = "";

    @SerializedName("endpoint")
    public Endpoint endpoint = new Endpoint();

    @SerializedName("capacity")
    public int capacity;

    @SerializedName("metadata")
    public Map<String, String> metadata = new LinkedHashMap<>();

    @SerializedName("status")
    public String status = "";

    @SerializedName("players")
    public int players;

    @SerializedName("load")
    public double load;

    @SerializedName("last_seen_at")
    public Instant lastSeenAt;

    @SerializedName("created_at")
    public Instant createdAt;

    @SerializedName("updated_at")
    public Instant updatedAt;
}
