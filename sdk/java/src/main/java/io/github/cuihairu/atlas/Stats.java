package io.github.cuihairu.atlas;

import com.google.gson.annotations.SerializedName;

import java.util.LinkedHashMap;
import java.util.Map;

/** Global admin stats. */
public class Stats {

    @SerializedName("total_servers")
    public long totalServers;

    @SerializedName("servers_by_status")
    public Map<String, Long> serversByStatus = new LinkedHashMap<>();

    @SerializedName("servers_by_region")
    public Map<String, Long> serversByRegion = new LinkedHashMap<>();

    @SerializedName("servers_by_version")
    public Map<String, Long> serversByVersion = new LinkedHashMap<>();

    @SerializedName("total_players")
    public long totalPlayers;

    @SerializedName("total_capacity")
    public long totalCapacity;

    @SerializedName("total_characters")
    public long totalCharacters;
}
