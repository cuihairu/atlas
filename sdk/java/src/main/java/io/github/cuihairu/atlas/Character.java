package io.github.cuihairu.atlas;

import com.google.gson.annotations.SerializedName;

import java.time.Instant;
import java.util.LinkedHashMap;
import java.util.Map;

/** A character in the directory. */
public class Character {

    @SerializedName("account_id")
    public long accountId;

    @SerializedName("server_id")
    public String serverId = "";

    @SerializedName("character_id")
    public long characterId;

    @SerializedName("name")
    public String name = "";

    @SerializedName("level")
    public int level;

    @SerializedName("class_id")
    public int classId;

    @SerializedName("avatar")
    public String avatar = "";

    @SerializedName("metadata")
    public Map<String, String> metadata = new LinkedHashMap<>();

    @SerializedName("last_login_at")
    public Instant lastLoginAt;

    @SerializedName("created_at")
    public Instant createdAt;

    @SerializedName("updated_at")
    public Instant updatedAt;
}
