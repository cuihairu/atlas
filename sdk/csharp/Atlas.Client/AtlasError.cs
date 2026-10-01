namespace Atlas;

/// <summary>Atlas API error. Code mirrors the REST error code when available
/// ("INVALID_ARGUMENT", "SERVER_NOT_FOUND", ...). Status is the HTTP status;
/// 0 means the request failed at the network layer.</summary>
public sealed class AtlasError : Exception
{
    public int Status { get; }
    public string Code { get; }

    public AtlasError(int status, string code, string message)
        : base(message)
    {
        Status = status;
        Code = code;
    }

    public override string ToString() =>
        Status == 0
            ? $"atlas: {Code}: {Message}"
            : $"atlas: {Code}: {Message} (HTTP {Status})";
}
