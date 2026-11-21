package io.github.cuihairu.atlas;

import com.google.gson.annotations.SerializedName;
import java.util.List;

/** Migration create request body. */
public class CreateMigrationRequest {

    // Gson falls back to the field name without an explicit name, which would
    // emit sourceServers/targetServer; the server reads source_servers/
    // target_server and rejects an empty source_servers as INVALID_ARGUMENT.
    @SerializedName("source_servers")
    public List<String> sourceServers;

    @SerializedName("target_server")
    public String targetServer;

    public CreateMigrationRequest() {}

    public CreateMigrationRequest(List<String> sourceServers, String targetServer) {
        this.sourceServers = sourceServers;
        this.targetServer = targetServer;
    }
}
