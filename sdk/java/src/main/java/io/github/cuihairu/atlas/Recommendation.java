package io.github.cuihairu.atlas;

/** Routing recommendation. */
public class Recommendation {

    public Server server = new Server();
    /** lowest_load | highest_capacity | has_character | fallback */
    public String reason = "";
}
