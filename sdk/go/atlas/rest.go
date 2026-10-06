package atlas

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// restBackend speaks the REST API (docs/api.md). Discovery, Directory,
// Routing and Admin share the main base; Registry may target its own
// split port via registryBase.
type restBackend struct {
	base         string
	registryBase string
	http         *http.Client
	policy       retryPolicy

	registryToken  string
	adminAPIKey    string
	defaultHeaders map[string]string
}

// do performs one JSON round trip against the main base, retrying
// transient failures (network errors and 5xx) with full-jitter
// exponential backoff.
func (b *restBackend) do(ctx context.Context, method, path string, body, out any, bearer string) error {
	return b.doBase(ctx, b.base, method, path, body, out, bearer)
}

func (b *restBackend) doBase(ctx context.Context, base, method, path string, body, out any, bearer string) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("atlas: encode request: %w", err)
		}
	}

	var resp *http.Response
	err := b.retry(ctx, func() error {
		return b.attempt(ctx, base, method, path, payload, bearer, &resp)
	})
	if err != nil {
		return err
	}
	return decodeJSON(resp, out)
}

// retry runs fn up to policy.max+1 times while isTransient holds.
func (b *restBackend) retry(ctx context.Context, fn func() error) error {
	max := b.policy.max
	if max < 0 {
		max = 0
	}
	var err error
	for attempt := 0; ; attempt++ {
		err = fn()
		if err == nil || attempt >= max || !isTransientHTTP(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(b.policy.nextDelay(rand.IntN(attempt + 1))):
		}
	}
}

func (b *restBackend) attempt(ctx context.Context, base, method, path string, payload []byte, bearer string, out **http.Response) error {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for k, v := range b.defaultHeaders {
		req.Header.Set(k, v)
	}
	resp, err := b.http.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 500 {
		io.Copy(io.Discard, resp.Body) //nolint:errcheck // drain for keep-alive
		resp.Body.Close()
		return &Error{StatusCode: resp.StatusCode, Code: "HTTP_" + strconv.Itoa(resp.StatusCode), Message: resp.Status}
	}
	*out = resp
	return nil
}

func (b *restBackend) close(_ context.Context) error { return nil }

// ── Registry (service token) ────────────────────────────────

func (b *restBackend) register(ctx context.Context, req RegisterRequest) (*RegisterResult, error) {
	var out RegisterResult
	err := b.doBase(ctx, b.registryBase, http.MethodPost, "/v1/registry/servers/register", req, &out, b.registryToken)
	return &out, err
}

func (b *restBackend) heartbeat(ctx context.Context, serverID string, req HeartbeatRequest) (*HeartbeatResult, error) {
	var out HeartbeatResult
	err := b.doBase(ctx, b.registryBase, http.MethodPost, "/v1/registry/servers/"+url.PathEscape(serverID)+"/heartbeat", req, &out, b.registryToken)
	return &out, err
}

func (b *restBackend) unregister(ctx context.Context, serverID string) (*StatusResult, error) {
	var out StatusResult
	err := b.doBase(ctx, b.registryBase, http.MethodPost, "/v1/registry/servers/"+url.PathEscape(serverID)+"/unregister", nil, &out, b.registryToken)
	return &out, err
}

func (b *restBackend) fetchCrossServerConfig(ctx context.Context) (*CrossServerConfig, error) {
	var out CrossServerConfig
	// The config pull lives on the Registry listener next to register /
	// heartbeat: it is part of the server-facing control plane, not the
	// player-facing public API.
	if err := b.doBase(ctx, b.registryBase, http.MethodGet, "/v1/crossserver/config", nil, &out, b.registryToken); err != nil {
		return nil, err
	}
	return &out, nil
}

// ── Discovery (public) ──────────────────────────────────────

func (b *restBackend) listServers(ctx context.Context, f ServerFilter) ([]Server, error) {
	q := url.Values{}
	if f.Region != "" {
		q.Set("region", f.Region)
	}
	if f.Version != "" {
		q.Set("version", f.Version)
	}
	if f.Platform != "" {
		q.Set("platform", f.Platform)
	}
	if f.Status != "" {
		q.Set("status", f.Status)
	}
	if f.Limit > 0 {
		q.Set("limit", strconv.Itoa(f.Limit))
	}
	var out struct {
		Servers []Server `json:"servers"`
	}
	err := b.do(ctx, http.MethodGet, "/v1/discovery/servers?"+q.Encode(), nil, &out, "")
	return out.Servers, err
}

func (b *restBackend) getServer(ctx context.Context, id string) (*Server, error) {
	var out Server
	err := b.do(ctx, http.MethodGet, "/v1/discovery/servers/"+url.PathEscape(id), nil, &out, "")
	return &out, err
}

// ── Directory (public) ──────────────────────────────────────

func (b *restBackend) createCharacter(ctx context.Context, req CreateCharacterRequest) (*CharacterWriteResult, error) {
	var out CharacterWriteResult
	err := b.do(ctx, http.MethodPost, "/v1/directory/characters", req, &out, "")
	if out.Status == "" && out.Character != nil {
		out.Status = "created" // flat synchronous reply carries no status
	}
	return &out, err
}

func (b *restBackend) getCharacter(ctx context.Context, characterID int64) (*Character, error) {
	var out Character
	err := b.do(ctx, http.MethodGet, "/v1/directory/characters/"+strconv.FormatInt(characterID, 10), nil, &out, "")
	return &out, err
}

func (b *restBackend) listCharactersByAccount(ctx context.Context, accountID int64) ([]Character, error) {
	var out struct {
		Characters []Character `json:"characters"`
	}
	err := b.do(ctx, http.MethodGet, "/v1/directory/accounts/"+strconv.FormatInt(accountID, 10)+"/characters", nil, &out, "")
	return out.Characters, err
}

func (b *restBackend) listCharactersByServer(ctx context.Context, serverID string, limit int, cursor string) (*CharacterPage, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var out struct {
		Characters []Character `json:"characters"`
		NextCursor string      `json:"next_cursor"`
	}
	err := b.do(ctx, http.MethodGet, "/v1/directory/servers/"+url.PathEscape(serverID)+"/characters?"+q.Encode(), nil, &out, "")
	return &CharacterPage{Characters: out.Characters, NextCursor: out.NextCursor}, err
}

func (b *restBackend) updateCharacter(ctx context.Context, characterID int64, req UpdateCharacterRequest) (*CharacterWriteResult, error) {
	var out CharacterWriteResult
	err := b.do(ctx, http.MethodPatch, "/v1/directory/characters/"+strconv.FormatInt(characterID, 10), req, &out, "")
	if out.Status == "" && out.Character != nil {
		out.Status = "updated" // flat synchronous reply carries no status
	}
	return &out, err
}

func (b *restBackend) deleteCharacter(ctx context.Context, characterID int64) (*CharacterWriteResult, error) {
	var out CharacterWriteResult
	err := b.do(ctx, http.MethodDelete, "/v1/directory/characters/"+strconv.FormatInt(characterID, 10), nil, &out, "")
	return &out, err
}

// ── Routing (public) ────────────────────────────────────────

func (b *restBackend) recommend(ctx context.Context, accountID int64, region, version, platform string) (*Recommendation, error) {
	q := url.Values{}
	if accountID > 0 {
		q.Set("account_id", strconv.FormatInt(accountID, 10))
	}
	if region != "" {
		q.Set("region", region)
	}
	if version != "" {
		q.Set("version", version)
	}
	if platform != "" {
		q.Set("platform", platform)
	}
	var out Recommendation
	err := b.do(ctx, http.MethodGet, "/v1/routing/recommended?"+q.Encode(), nil, &out, "")
	return &out, err
}

// ── Admin (API key) ─────────────────────────────────────────

func (b *restBackend) setMaintenance(ctx context.Context, serverID string) (*StatusResult, error) {
	return b.lifecycle(ctx, "maintenance", serverID)
}

func (b *restBackend) setDrain(ctx context.Context, serverID string) (*StatusResult, error) {
	return b.lifecycle(ctx, "drain", serverID)
}

func (b *restBackend) enable(ctx context.Context, serverID string) (*StatusResult, error) {
	return b.lifecycle(ctx, "enable", serverID)
}

func (b *restBackend) disable(ctx context.Context, serverID string) (*StatusResult, error) {
	return b.lifecycle(ctx, "disable", serverID)
}

func (b *restBackend) lifecycle(ctx context.Context, action, serverID string) (*StatusResult, error) {
	var out StatusResult
	err := b.do(ctx, http.MethodPost, "/v1/admin/servers/"+url.PathEscape(serverID)+"/"+action, nil, &out, b.adminAPIKey)
	return &out, err
}

func (b *restBackend) stats(ctx context.Context) (*Stats, error) {
	var out Stats
	err := b.do(ctx, http.MethodGet, "/v1/admin/stats", nil, &out, b.adminAPIKey)
	return &out, err
}

func (b *restBackend) searchCharacters(ctx context.Context, f CharacterFilter) (*CharacterPage, error) {
	q := url.Values{}
	if f.Name != "" {
		q.Set("name", f.Name)
	}
	if f.ServerID != "" {
		q.Set("server_id", f.ServerID)
	}
	if f.ClassID != nil {
		q.Set("class_id", strconv.Itoa(*f.ClassID))
	}
	if f.MinLevel != nil {
		q.Set("min_level", strconv.Itoa(*f.MinLevel))
	}
	if f.MaxLevel != nil {
		q.Set("max_level", strconv.Itoa(*f.MaxLevel))
	}
	if f.Limit > 0 {
		q.Set("limit", strconv.Itoa(f.Limit))
	}
	if f.Cursor != "" {
		q.Set("cursor", f.Cursor)
	}
	var out struct {
		Characters []Character `json:"characters"`
		NextCursor string      `json:"next_cursor"`
	}
	err := b.do(ctx, http.MethodGet, "/v1/admin/characters/search?"+q.Encode(), nil, &out, b.adminAPIKey)
	return &CharacterPage{Characters: out.Characters, NextCursor: out.NextCursor}, err
}

func (b *restBackend) createMigration(ctx context.Context, req CreateMigrationRequest) (*Migration, error) {
	var out struct {
		Migration Migration `json:"migration"`
	}
	err := b.do(ctx, http.MethodPost, "/v1/admin/migrations", req, &out, b.adminAPIKey)
	return &out.Migration, err
}

func (b *restBackend) getMigration(ctx context.Context, id string) (*Migration, error) {
	var out struct {
		Migration Migration `json:"migration"`
	}
	err := b.do(ctx, http.MethodGet, "/v1/admin/migrations/"+url.PathEscape(id), nil, &out, b.adminAPIKey)
	return &out.Migration, err
}

func (b *restBackend) listMigrations(ctx context.Context, limit int) ([]Migration, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	var out struct {
		Migrations []Migration `json:"migrations"`
	}
	err := b.do(ctx, http.MethodGet, "/v1/admin/migrations?"+q.Encode(), nil, &out, b.adminAPIKey)
	return out.Migrations, err
}

func (b *restBackend) rollbackMigration(ctx context.Context, id string) (*Migration, error) {
	var out struct {
		Migration Migration `json:"migration"`
	}
	err := b.do(ctx, http.MethodPost, "/v1/admin/migrations/"+url.PathEscape(id)+"/rollback", nil, &out, b.adminAPIKey)
	return &out.Migration, err
}

// isTransientHTTP reports whether err is worth retrying: network failures
// and 5xx responses yes, 4xx responses belong to the caller.
func isTransientHTTP(err error) bool {
	if err == nil {
		return false
	}
	apiErr, ok := err.(*Error)
	if !ok {
		return true // transport-level error (timeout, refused, ...)
	}
	return apiErr.StatusCode >= 500
}
