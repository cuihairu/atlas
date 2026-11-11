package io.github.cuihairu.atlas;

import com.google.gson.annotations.SerializedName;

/** PATCH body — null fields are left unchanged. */
public class UpdateCharacterRequest {

    public String name;
    public Integer level;
    @SerializedName("class_id")
    public Integer classId;
    public String avatar;
}
