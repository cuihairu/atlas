package io.github.cuihairu.atlas;

import com.google.gson.annotations.SerializedName;

import java.util.ArrayList;
import java.util.List;

/** Paginated character list (by-server listing and admin search). */
public class CharacterPage {

    public List<Character> characters = new ArrayList<>();
    @SerializedName("next_cursor")
    public String nextCursor = "";
}
