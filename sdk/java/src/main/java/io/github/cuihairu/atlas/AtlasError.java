package io.github.cuihairu.atlas;

/** Atlas API error — thrown by every client method on failure. */
public class AtlasError extends RuntimeException {

    /** HTTP status, or 0 for network/transport errors. */
    private final int status;

    /** REST error code ("SERVER_NOT_FOUND", ...) or "HTTP_&lt;status&gt;" / "NETWORK". */
    private final String code;

    public AtlasError(int status, String code, String message) {
        super(code + ": " + message);
        this.status = status;
        this.code = code;
    }

    public int getStatus() {
        return status;
    }

    public String getCode() {
        return code;
    }
}
