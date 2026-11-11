package io.github.cuihairu.atlas;

import com.google.gson.annotations.SerializedName;

/** Reply from {@code POST /v1/registry/servers/register}. */
public class RegisterResult {

    @SerializedName("server_id")
    public String serverId = "";
    public String status = "";
}
