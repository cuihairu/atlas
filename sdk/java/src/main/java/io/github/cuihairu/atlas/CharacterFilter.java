package io.github.cuihairu.atlas;

/** Admin character search filters; null/0 values are omitted from the query. */
public class CharacterFilter {

    public String name;
    public String serverId;
    public Integer classId;
    public Integer minLevel;
    public Integer maxLevel;
    public Integer limit;
    public String cursor;
}
