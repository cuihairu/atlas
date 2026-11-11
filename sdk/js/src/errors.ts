// Atlas API error — thrown by every client method on failure.

export class AtlasError extends Error {
  /** HTTP status, or 0 for network/transport errors. */
  readonly status: number;
  /** REST error code ("SERVER_NOT_FOUND", ...) or "HTTP_<status>" / "NETWORK". */
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(`${code}: ${message}`);
    this.name = "AtlasError";
    this.status = status;
    this.code = code;
  }
}

/** Map an error reply body onto AtlasError; non-JSON bodies become
 * "HTTP_<status>" with the raw text truncated. */
export function parseError(status: number, body: unknown): AtlasError {
  if (body && typeof body === "object") {
    const err = (body as Record<string, any>).error;
    if (err && typeof err === "object") {
      return new AtlasError(status, err.code ?? "", err.message ?? "");
    }
  }
  const text = typeof body === "string" ? body : JSON.stringify(body);
  return new AtlasError(status, `HTTP_${status}`, (text ?? "").slice(0, 200));
}
