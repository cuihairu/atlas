package io.github.cuihairu.atlas;

import com.google.gson.annotations.SerializedName;

/** Directory create request body; null optionals are omitted. */
public class CreateCharacterRequest {

    @SerializedName("account_id")
    public long accountId;
    @SerializedName("server_id")
    public String serverId;
    @SerializedName("character_id")
    public long characterId;
    public String name;
    public Integer level;
    @SerializedName("class_id")
    public Integer classId;
    public String avatar;

    public CreateCharacterRequest() {}

    public CreateCharacterRequest(long accountId, String serverId,
                                  long characterId, String name) {
        this.accountId = accountId;
        this.serverId = serverId;
        this.characterId = characterId;
        this.name = name;
    }
}
