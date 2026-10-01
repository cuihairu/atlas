package io.github.cuihairu.atlas;

/** Directory write reply; {@code character} is null when status == "queued". */
public class CharacterWriteResult {

    public Character character;
    /** created | updated | deleted | queued */
    public String status = "";

    public CharacterWriteResult() {}

    public CharacterWriteResult(Character character, String status) {
        this.character = character;
        this.status = status;
    }
}
