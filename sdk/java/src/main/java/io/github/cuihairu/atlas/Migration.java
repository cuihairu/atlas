package io.github.cuihairu.atlas;

import com.google.gson.annotations.SerializedName;

import java.time.Instant;
import java.util.ArrayList;
import java.util.List;

/** A server migration (merge / transfer). */
public class Migration {

    public String id = "";

    @SerializedName("source_servers")
    public List<String> sourceServers = new ArrayList<>();

    @SerializedName("target_server")
    public String targetServer = "";

    public String status = "";

    @SerializedName("started_at")
    public Instant startedAt;

    @SerializedName("completed_at")
    public Instant completedAt;
}
