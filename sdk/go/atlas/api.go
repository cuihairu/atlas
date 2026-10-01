package atlas

import "context"

// ── Registry ────────────────────────────────────────────────

// Register announces this server to the fleet.
func (c *Client) Register(ctx context.Context, req RegisterRequest) (*RegisterResult, error) {
	return c.backend.register(ctx, req)
}

// Heartbeat reports live players/load. Recommended cadence: report
// every interval while Atlas suspects at 3× and marks offline at 6×
// (ATLAS_SUSPECT_AFTER / ATLAS_OFFLINE_AFTER).
func (c *Client) Heartbeat(ctx context.Context, serverID string, req HeartbeatRequest) (*HeartbeatResult, error) {
	return c.backend.heartbeat(ctx, serverID, req)
}

// Unregister gracefully removes the server from the fleet.
func (c *Client) Unregister(ctx context.Context, serverID string) (*StatusResult, error) {
	return c.backend.unregister(ctx, serverID)
}

// ── Discovery ───────────────────────────────────────────────

// ListServers returns servers matching the filter (empty fields are
// omitted and Atlas applies its own visibility defaults).
func (c *Client) ListServers(ctx context.Context, f ServerFilter) ([]Server, error) {
	return c.backend.listServers(ctx, f)
}

// GetServer fetches one server with runtime state.
func (c *Client) GetServer(ctx context.Context, serverID string) (*Server, error) {
	return c.backend.getServer(ctx, serverID)
}

// ── Directory ───────────────────────────────────────────────

// CreateCharacter adds a character index entry. With an asynchronous
// event adapter the result carries Status "queued" and a nil Character.
func (c *Client) CreateCharacter(ctx context.Context, req CreateCharacterRequest) (*CharacterWriteResult, error) {
	return c.backend.createCharacter(ctx, req)
}

// GetCharacter fetches one character index entry.
func (c *Client) GetCharacter(ctx context.Context, characterID int64) (*Character, error) {
	return c.backend.getCharacter(ctx, characterID)
}

// ListCharactersByAccount lists every character on an account.
func (c *Client) ListCharactersByAccount(ctx context.Context, accountID int64) ([]Character, error) {
	return c.backend.listCharactersByAccount(ctx, accountID)
}

// ListCharactersByServer lists a server's characters with cursor
// pagination (cursor "" starts from the beginning).
func (c *Client) ListCharactersByServer(ctx context.Context, serverID string, limit int, cursor string) (*CharacterPage, error) {
	return c.backend.listCharactersByServer(ctx, serverID, limit, cursor)
}

// UpdateCharacter patches a character; nil fields are unchanged.
func (c *Client) UpdateCharacter(ctx context.Context, characterID int64, req UpdateCharacterRequest) (*CharacterWriteResult, error) {
	return c.backend.updateCharacter(ctx, characterID, req)
}

// DeleteCharacter removes a character index entry.
func (c *Client) DeleteCharacter(ctx context.Context, characterID int64) (*CharacterWriteResult, error) {
	return c.backend.deleteCharacter(ctx, characterID)
}

// ── Routing ─────────────────────────────────────────────────

// Recommend picks the best server for an account. Zero/empty filter
// fields fall back to Atlas's default ranking.
func (c *Client) Recommend(ctx context.Context, accountID int64, region, version, platform string) (*Recommendation, error) {
	return c.backend.recommend(ctx, accountID, region, version, platform)
}

// ── Admin ───────────────────────────────────────────────────

// SetMaintenance switches a server to maintenance.
func (c *Client) SetMaintenance(ctx context.Context, serverID string) (*StatusResult, error) {
	return c.backend.setMaintenance(ctx, serverID)
}

// SetDrain starts draining a server.
func (c *Client) SetDrain(ctx context.Context, serverID string) (*StatusResult, error) {
	return c.backend.setDrain(ctx, serverID)
}

// Enable reactivates a server.
func (c *Client) Enable(ctx context.Context, serverID string) (*StatusResult, error) {
	return c.backend.enable(ctx, serverID)
}

// Disable takes a server out of rotation.
func (c *Client) Disable(ctx context.Context, serverID string) (*StatusResult, error) {
	return c.backend.disable(ctx, serverID)
}

// Stats returns the fleet overview.
func (c *Client) Stats(ctx context.Context) (*Stats, error) {
	return c.backend.stats(ctx)
}

// SearchCharacters runs an admin character search.
func (c *Client) SearchCharacters(ctx context.Context, f CharacterFilter) (*CharacterPage, error) {
	return c.backend.searchCharacters(ctx, f)
}

// CreateMigration starts a character migration.
func (c *Client) CreateMigration(ctx context.Context, req CreateMigrationRequest) (*Migration, error) {
	return c.backend.createMigration(ctx, req)
}

// GetMigration fetches one migration by ID.
func (c *Client) GetMigration(ctx context.Context, id string) (*Migration, error) {
	return c.backend.getMigration(ctx, id)
}

// ListMigrations lists recent migrations (limit <= 0 means server default).
func (c *Client) ListMigrations(ctx context.Context, limit int) ([]Migration, error) {
	return c.backend.listMigrations(ctx, limit)
}

// RollbackMigration reverts a migration to its pre-run state.
func (c *Client) RollbackMigration(ctx context.Context, id string) (*Migration, error) {
	return c.backend.rollbackMigration(ctx, id)
}
