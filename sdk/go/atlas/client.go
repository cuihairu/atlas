package atlas

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"
)

// Options configures a Client.
type Options struct {
	// Addr is the Atlas address. REST accepts a full base URL
	// ("http://atlas:8080"); a bare "host:port" gets "http://" prepended.
	// gRPC expects "host:port" (ATLAS_GRPC_ADDR).
	Addr string

	// Transport selects REST (default) or gRPC.
	Transport Transport

	// HTTPClient customizes the REST transport (timeouts, dialer...).
	// A client with a 10s timeout is created when nil.
	HTTPClient *http.Client

	// RegistryToken authenticates Registry calls
	// (ATLAS_REGISTRY_TOKENS counterpart, sent as Bearer).
	RegistryToken string

	// AdminAPIKey authenticates Admin calls (ATLAS_ADMIN_API_KEYS
	// counterpart, sent as Bearer).
	AdminAPIKey string

	// MaxRetries bounds retries for transient failures (network errors
	// and 5xx responses). Zero means 3; negative disables retrying.
	MaxRetries int

	// BaseBackoff is the delay ceiling before the first retry; each
	// attempt doubles it with full jitter. Zero means 100ms.
	BaseBackoff time.Duration
}

// Error is an Atlas API error. Code mirrors the REST error code when
// available ("INVALID_ARGUMENT", "SERVER_NOT_FOUND", ...); gRPC statuses
// are mapped to their uppercase names ("NOT_FOUND", ...).
type Error struct {
	StatusCode int    `json:"-"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("atlas: %s: %s", e.Code, e.Message)
}

// retryPolicy is shared retry configuration for both backends.
type retryPolicy struct {
	max         int
	baseBackoff time.Duration
}

// nextDelay returns the full-jitter delay for attempt n (0-based).
func (p retryPolicy) nextDelay(n int) time.Duration {
	ceiling := p.baseBackoff << n //nolint:gosec // bounded shift
	return time.Duration(rand.Int64N(int64(ceiling) + 1))
}

// backend is the transport abstraction both REST and gRPC implement.
type backend interface {
	register(ctx context.Context, req RegisterRequest) (*RegisterResult, error)
	heartbeat(ctx context.Context, serverID string, req HeartbeatRequest) (*HeartbeatResult, error)
	unregister(ctx context.Context, serverID string) (*StatusResult, error)

	listServers(ctx context.Context, f ServerFilter) ([]Server, error)
	getServer(ctx context.Context, id string) (*Server, error)

	createCharacter(ctx context.Context, req CreateCharacterRequest) (*CharacterWriteResult, error)
	getCharacter(ctx context.Context, characterID int64) (*Character, error)
	listCharactersByAccount(ctx context.Context, accountID int64) ([]Character, error)
	listCharactersByServer(ctx context.Context, serverID string, limit int, cursor string) (*CharacterPage, error)
	updateCharacter(ctx context.Context, characterID int64, req UpdateCharacterRequest) (*CharacterWriteResult, error)
	deleteCharacter(ctx context.Context, characterID int64) (*CharacterWriteResult, error)

	recommend(ctx context.Context, accountID int64, region, version, platform string) (*Recommendation, error)

	setMaintenance(ctx context.Context, serverID string) (*StatusResult, error)
	setDrain(ctx context.Context, serverID string) (*StatusResult, error)
	enable(ctx context.Context, serverID string) (*StatusResult, error)
	disable(ctx context.Context, serverID string) (*StatusResult, error)
	stats(ctx context.Context) (*Stats, error)
	searchCharacters(ctx context.Context, f CharacterFilter) (*CharacterPage, error)
	createMigration(ctx context.Context, req CreateMigrationRequest) (*Migration, error)
	getMigration(ctx context.Context, id string) (*Migration, error)
	listMigrations(ctx context.Context, limit int) ([]Migration, error)
	rollbackMigration(ctx context.Context, id string) (*Migration, error)

	close(ctx context.Context) error
}

// Client is an Atlas SDK client. It is safe for concurrent use.
type Client struct {
	opts    Options
	backend backend
}

// New creates a Client. Connections are established lazily; an error is
// returned only for invalid options.
func New(opts Options) (*Client, error) {
	if opts.Addr == "" {
		return nil, fmt.Errorf("atlas: Options.Addr is required")
	}
	if opts.Transport == "" {
		opts.Transport = TransportREST
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = 3
	}
	if opts.BaseBackoff == 0 {
		opts.BaseBackoff = 100 * time.Millisecond
	}
	policy := retryPolicy{max: opts.MaxRetries, baseBackoff: opts.BaseBackoff}

	c := &Client{opts: opts}
	switch opts.Transport {
	case TransportREST:
		base := opts.Addr
		if !strings.Contains(base, "://") {
			base = "http://" + base
		}
		base = strings.TrimRight(base, "/")
		httpClient := opts.HTTPClient
		if httpClient == nil {
			httpClient = &http.Client{Timeout: 10 * time.Second}
		}
		c.backend = &restBackend{
			base:          base,
			http:          httpClient,
			policy:        policy,
			registryToken: opts.RegistryToken,
			adminAPIKey:   opts.AdminAPIKey,
		}
	case TransportGRPC:
		gb, err := newGRPCBackend(opts, policy)
		if err != nil {
			return nil, err
		}
		c.backend = gb
	default:
		return nil, fmt.Errorf("atlas: unknown transport %q (want rest or grpc)", opts.Transport)
	}
	return c, nil
}

// Close releases the underlying transport (gRPC connection).
func (c *Client) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return c.backend.close(ctx)
}

// decodeJSON decodes a response body into v, translating error envelopes.
func decodeJSON(resp *http.Response, v any) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return apiError(resp)
	}
	if v == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("atlas: decode response: %w", err)
	}
	return nil
}

// apiError converts a non-2xx response into *Error.
func apiError(resp *http.Response) error {
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && body.Error.Code != "" {
		return &Error{StatusCode: resp.StatusCode, Code: body.Error.Code, Message: body.Error.Message}
	}
	return &Error{
		StatusCode: resp.StatusCode,
		Code:       "HTTP_" + fmt.Sprint(resp.StatusCode),
		Message:    resp.Status,
	}
}
