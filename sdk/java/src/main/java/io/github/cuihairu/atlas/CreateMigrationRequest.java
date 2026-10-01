package io.github.cuihairu.atlas;

import java.util.List;

/** Migration create request body. */
public class CreateMigrationRequest {

    public List<String> sourceServers;
    public String targetServer;

    public CreateMigrationRequest() {}

    public CreateMigrationRequest(List<String> sourceServers, String targetServer) {
        this.sourceServers = sourceServers;
        this.targetServer = targetServer;
    }
}
