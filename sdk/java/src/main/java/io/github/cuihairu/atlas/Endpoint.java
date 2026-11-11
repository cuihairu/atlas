package io.github.cuihairu.atlas;

import com.google.gson.annotations.SerializedName;

/** Game server endpoint (host:port clients connect to). */
public class Endpoint {

    @SerializedName("host")
    public String host = "";

    @SerializedName("port")
    public int port;

    public Endpoint() {}

    public Endpoint(String host, int port) {
        this.host = host;
        this.port = port;
    }
}
