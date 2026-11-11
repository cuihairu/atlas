package io.github.cuihairu.atlas;

import com.google.gson.annotations.SerializedName;

/** Reply from the weight endpoint (POST /v1/admin/servers/{id}/weight). */
public class WeightResult {

    @SerializedName("server_id")
    public String serverId;
    public int weight;
}
