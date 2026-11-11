package io.github.cuihairu.atlas;

import com.google.gson.annotations.SerializedName;

/** Reply from lifecycle endpoints (unregister / maintenance / drain / enable / disable). */
public class StatusResult {

    @SerializedName("server_id")
    public String serverId;
    public String status = "";
}
