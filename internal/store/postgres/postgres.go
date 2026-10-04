// Package postgres provides a PostgreSQL implementation of store.ServerStore
// and store.CharacterStore using pgxpool.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// compile-time interface checks
var (
	_ store.ServerStore    = (*Store)(nil)
	_ store.CharacterStore = (*Store)(nil)
	_ store.MigrationStore = (*Store)(nil)
	_ store.StatsStore     = (*Store)(nil)
)

// Store implements store.ServerStore and store.CharacterStore on PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
	// runtime is an optional heartbeat store wired by the deployment: when
	// set, GetStats merges its player counts into TotalPlayers. The default
	// deployment wires Redis; without it the SQL side cannot see heartbeats
	// and TotalPlayers reads 0.
	runtime store.RuntimeStore
}

// New creates a new PostgreSQL store from an existing pgxpool.Pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// WithRuntime attaches a heartbeat store for stats aggregation (GetStats
// TotalPlayers). Returns the store for chaining.
func (s *Store) WithRuntime(rt store.RuntimeStore) *Store {
	s.runtime = rt
	return s
}

// Ping checks the database connection.
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Close closes the underlying connection pool.
func (s *Store) Close() error {
	s.pool.Close()
	return nil
}

// ---------------------------------------------------------------------------
// ServerStore
// ---------------------------------------------------------------------------

func (s *Store) RegisterServer(ctx context.Context, srv *model.Server) error {
	if srv.Status == "" {
		srv.Status = model.StatusStarting
	}
	now := time.Now()
	srv.CreatedAt = now
	srv.UpdatedAt = now

	const q = `
INSERT INTO servers (id, name, type, region, realm_id, shard_id, version, platform,
                     endpoint_host, endpoint_port, capacity, status, started_at, created_at, updated_at, source,
                     notify_mode, notify_callback_url, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
ON CONFLICT (id) DO UPDATE SET
    name          = EXCLUDED.name,
    type          = EXCLUDED.type,
    region        = EXCLUDED.region,
    realm_id      = EXCLUDED.realm_id,
    shard_id      = EXCLUDED.shard_id,
    version       = EXCLUDED.version,
    platform      = EXCLUDED.platform,
    endpoint_host = EXCLUDED.endpoint_host,
    endpoint_port = EXCLUDED.endpoint_port,
    capacity      = EXCLUDED.capacity,
    -- Reset only a dead-ish lifecycle (suspect / offline) so a re-registering
    -- server can recover; keep active and operator-set statuses untouched.
    status        = CASE WHEN servers.status IN ('suspect', 'offline') THEN EXCLUDED.status ELSE servers.status END,
    started_at    = EXCLUDED.started_at,
    updated_at    = EXCLUDED.updated_at,
    source        = EXCLUDED.source,
    notify_mode   = EXCLUDED.notify_mode,
    notify_callback_url = EXCLUDED.notify_callback_url,
    metadata      = EXCLUDED.metadata
`
	_, err := s.pool.Exec(ctx, q,
		srv.ID, srv.Name, srv.Type, srv.Region,
		srv.RealmID, srv.ShardID, srv.Version, srv.Platform,
		srv.Endpoint.Host, srv.Endpoint.Port, srv.Capacity,
		srv.Status, srv.StartedAt, srv.CreatedAt, srv.UpdatedAt, srv.Source,
		srv.NotifyMode, srv.NotifyCallbackURL, metadataJSON(srv.Metadata),
	)
	if err != nil {
		return fmt.Errorf("register server %s: %w", srv.ID, err)
	}
	return nil
}

func (s *Store) GetServer(ctx context.Context, id string) (*model.Server, error) {
	const q = `
SELECT id, name, type, region, realm_id, shard_id, version, platform,
       endpoint_host, endpoint_port, capacity, status, started_at, created_at, updated_at, source, tags,
       notify_mode, notify_callback_url, metadata
FROM servers WHERE id = $1
`
	srv, err := scanServer(s.pool.QueryRow(ctx, q, id))
	if err != nil {
		return nil, fmt.Errorf("get server %s: %w", id, err)
	}
	return srv, nil
}

func (s *Store) ListServers(ctx context.Context, f store.ServerFilter) ([]*model.Server, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > store.ListServersMaxLimit {
		limit = store.ListServersMaxLimit
	}

	q := `SELECT id, name, type, region, realm_id, shard_id, version, platform,
       endpoint_host, endpoint_port, capacity, status, started_at, created_at, updated_at, source, tags,
       notify_mode, notify_callback_url, metadata
FROM servers WHERE 1=1`
	args := []any{}
	n := 1

	if f.Region != "" {
		q += fmt.Sprintf(" AND region = $%d", n)
		args = append(args, f.Region)
		n++
	}
	if f.Realm != "" {
		q += fmt.Sprintf(" AND realm_id = $%d", n)
		args = append(args, f.Realm)
		n++
	}
	if f.Shard != "" {
		q += fmt.Sprintf(" AND shard_id = $%d", n)
		args = append(args, f.Shard)
		n++
	}
	if f.Version != "" {
		q += fmt.Sprintf(" AND version = $%d", n)
		args = append(args, f.Version)
		n++
	}
	if f.Platform != "" {
		q += fmt.Sprintf(" AND platform = $%d", n)
		args = append(args, f.Platform)
		n++
	}
	if f.Status != "" {
		q += fmt.Sprintf(" AND status = $%d", n)
		args = append(args, string(f.Status))
		n++
	}
	if f.Cursor != "" {
		q += fmt.Sprintf(" AND id > $%d", n)
		args = append(args, f.Cursor)
		n++
	}

	q += " ORDER BY id"
	q += fmt.Sprintf(" LIMIT $%d", n)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}
	defer rows.Close()

	var result []*model.Server
	for rows.Next() {
		srv, err := scanServer(rows)
		if err != nil {
			return nil, fmt.Errorf("list servers scan: %w", err)
		}
		result = append(result, srv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list servers iteration: %w", err)
	}
	return result, nil
}

func (s *Store) UpdateServerStatus(ctx context.Context, id string, status model.ServerStatus) error {
	const q = `UPDATE servers SET status = $1, updated_at = $2 WHERE id = $3`
	tag, err := s.pool.Exec(ctx, q, string(status), time.Now(), id)
	if err != nil {
		return fmt.Errorf("update server status %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("server %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func (s *Store) UpdateServerTags(ctx context.Context, id string, tags []model.ServerTag) error {
	if tags == nil {
		tags = []model.ServerTag{}
	}
	b, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("marshal server %s tags: %w", id, err)
	}
	const q = `UPDATE servers SET tags = $1::jsonb, updated_at = $2 WHERE id = $3`
	tag, err := s.pool.Exec(ctx, q, string(b), time.Now(), id)
	if err != nil {
		return fmt.Errorf("update server tags %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("server %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func (s *Store) DeleteServer(ctx context.Context, id string) error {
	const q = `DELETE FROM servers WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("delete server %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("server %s: %w", id, store.ErrNotFound)
	}
	return nil
}

// ---------------------------------------------------------------------------
// CharacterStore
// ---------------------------------------------------------------------------

func (s *Store) UpsertCharacter(ctx context.Context, ch *model.Character) error {
	now := time.Now()
	ch.CreatedAt = now
	ch.UpdatedAt = now

	const q = `
INSERT INTO character_index (account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
ON CONFLICT (account_id, server_id, character_id) DO UPDATE SET
    name          = EXCLUDED.name,
    level         = EXCLUDED.level,
    class_id      = EXCLUDED.class_id,
    avatar        = EXCLUDED.avatar,
    last_login_at = EXCLUDED.last_login_at,
    updated_at    = EXCLUDED.updated_at,
    metadata      = EXCLUDED.metadata
`
	_, err := s.pool.Exec(ctx, q,
		ch.AccountID, ch.ServerID, ch.CharacterID,
		ch.Name, ch.Level, ch.ClassID, ch.Avatar,
		ch.LastLoginAt, ch.CreatedAt, ch.UpdatedAt, metadataJSON(ch.Metadata),
	)
	if err != nil {
		return fmt.Errorf("upsert character: %w", err)
	}
	return nil
}

func (s *Store) GetCharacter(ctx context.Context, accountID int64, serverID string, characterID int64) (*model.Character, error) {
	const q = `
SELECT account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at, metadata
FROM character_index
WHERE account_id = $1 AND server_id = $2 AND character_id = $3
`
	ch, err := scanCharacter(s.pool.QueryRow(ctx, q, accountID, serverID, characterID))
	if err != nil {
		return nil, fmt.Errorf("get character: %w", err)
	}
	return ch, nil
}

func (s *Store) GetCharacterByCharacterID(ctx context.Context, characterID int64) (*model.Character, error) {
	const q = `
SELECT account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at, metadata
FROM character_index
WHERE character_id = $1
`
	ch, err := scanCharacter(s.pool.QueryRow(ctx, q, characterID))
	if err != nil {
		return nil, fmt.Errorf("get character by id: %w", err)
	}
	return ch, nil
}

func (s *Store) UpdateCharacter(ctx context.Context, accountID int64, serverID string, characterID int64, patch store.CharacterPatch) error {
	// Build dynamic SET clause.
	setClauses := []string{}
	args := []any{}
	n := 1

	if patch.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", n))
		args = append(args, *patch.Name)
		n++
	}
	if patch.Level != nil {
		setClauses = append(setClauses, fmt.Sprintf("level = $%d", n))
		args = append(args, *patch.Level)
		n++
	}
	if patch.ClassID != nil {
		setClauses = append(setClauses, fmt.Sprintf("class_id = $%d", n))
		args = append(args, *patch.ClassID)
		n++
	}
	if patch.Avatar != nil {
		setClauses = append(setClauses, fmt.Sprintf("avatar = $%d", n))
		args = append(args, *patch.Avatar)
		n++
	}
	if patch.Metadata != nil {
		setClauses = append(setClauses, fmt.Sprintf("metadata = $%d", n))
		args = append(args, metadataJSON(*patch.Metadata))
		n++
	}
	if patch.LastLoginAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_login_at = $%d", n))
		args = append(args, *patch.LastLoginAt)
		n++
	}

	if len(setClauses) == 0 {
		return nil // nothing to update
	}

	setClauses = append(setClauses, fmt.Sprintf("updated_at = $%d", n))
	args = append(args, time.Now())
	n++

	q := fmt.Sprintf("UPDATE character_index SET %s WHERE account_id = $%d AND server_id = $%d AND character_id = $%d",
		joinStrings(setClauses, ", "), n, n+1, n+2)
	args = append(args, accountID, serverID, characterID)

	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("update character: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("character (account=%d, server=%s, char=%d): %w", accountID, serverID, characterID, store.ErrNotFound)
	}
	return nil
}

func (s *Store) DeleteCharacter(ctx context.Context, accountID int64, serverID string, characterID int64) error {
	const q = `DELETE FROM character_index WHERE account_id = $1 AND server_id = $2 AND character_id = $3`
	tag, err := s.pool.Exec(ctx, q, accountID, serverID, characterID)
	if err != nil {
		return fmt.Errorf("delete character: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("character (account=%d, server=%s, char=%d): %w", accountID, serverID, characterID, store.ErrNotFound)
	}
	return nil
}

func (s *Store) ListCharactersByAccount(ctx context.Context, accountID int64) ([]*model.Character, error) {
	const q = `
SELECT account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at, metadata
FROM character_index
WHERE account_id = $1
ORDER BY server_id, character_id
`
	rows, err := s.pool.Query(ctx, q, accountID)
	if err != nil {
		return nil, fmt.Errorf("list characters by account: %w", err)
	}
	defer rows.Close()

	return scanCharacters(rows)
}

func (s *Store) ListCharactersByServer(ctx context.Context, serverID string, limit int, cursor string) ([]*model.Character, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	q := `
SELECT account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at, metadata
FROM character_index
WHERE server_id = $1`
	args := []any{serverID}
	n := 2

	if cursor != "" {
		q += fmt.Sprintf(" AND character_id > $%d", n)
		args = append(args, cursor)
		n++
	}

	q += " ORDER BY character_id"
	q += fmt.Sprintf(" LIMIT $%d", n)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list characters by server: %w", err)
	}
	defer rows.Close()

	return scanCharacters(rows)
}

// ---------------------------------------------------------------------------
// SearchCharacters
// ---------------------------------------------------------------------------

func (s *Store) SearchCharacters(ctx context.Context, filter store.CharacterSearchFilter) ([]*model.Character, string, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 200 {
		filter.Limit = 200
	}

	q := `SELECT account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at, metadata
FROM character_index WHERE 1=1`
	args := []any{}
	n := 1

	if filter.Name != "" {
		q += fmt.Sprintf(" AND LOWER(name) LIKE LOWER($%d)", n)
		args = append(args, "%"+filter.Name+"%")
		n++
	}
	if filter.ServerID != "" {
		q += fmt.Sprintf(" AND server_id = $%d", n)
		args = append(args, filter.ServerID)
		n++
	}
	if filter.AccountID != 0 {
		q += fmt.Sprintf(" AND account_id = $%d", n)
		args = append(args, filter.AccountID)
		n++
	}
	if filter.MetadataKey != "" {
		q += fmt.Sprintf(" AND metadata->>$%d = $%d", n, n+1)
		args = append(args, filter.MetadataKey, filter.MetadataValue)
		n += 2
	}
	if filter.MinLevel != nil {
		q += fmt.Sprintf(" AND level >= $%d", n)
		args = append(args, *filter.MinLevel)
		n++
	}
	if filter.MaxLevel != nil {
		q += fmt.Sprintf(" AND level <= $%d", n)
		args = append(args, *filter.MaxLevel)
		n++
	}
	if filter.Cursor != "" {
		// Cursor format: "server_id:character_id"
		q += fmt.Sprintf(" AND (server_id, character_id) > ($%d, $%d)", n, n+1)
		parts := splitCursor(filter.Cursor)
		args = append(args, parts[0], parts[1])
		n += 2
	}

	q += " ORDER BY server_id, character_id"
	q += fmt.Sprintf(" LIMIT $%d", n)
	args = append(args, filter.Limit)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", fmt.Errorf("search characters: %w", err)
	}
	defer rows.Close()

	result, err := scanCharacters(rows)
	if err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if len(result) > 0 {
		last := result[len(result)-1]
		nextCursor = last.ServerID + ":" + fmt.Sprintf("%d", last.CharacterID)
	}

	return result, nextCursor, nil
}

// ---------------------------------------------------------------------------
// MigrationStore
// ---------------------------------------------------------------------------

func (s *Store) CreateMigration(ctx context.Context, m *model.Migration) error {
	const q = `
INSERT INTO server_migrations (id, source_servers, target_server, status, started_at, completed_at)
VALUES ($1, $2, $3, $4, $5, $6)
`
	_, err := s.pool.Exec(ctx, q,
		m.ID, m.SourceServers, m.TargetServer,
		string(m.Status), m.StartedAt, m.CompletedAt,
	)
	if err != nil {
		return fmt.Errorf("create migration: %w", err)
	}
	return nil
}

func (s *Store) GetMigration(ctx context.Context, id string) (*model.Migration, error) {
	const q = `
SELECT id, source_servers, target_server, status, started_at, completed_at
FROM server_migrations WHERE id = $1
`
	var m model.Migration
	var status string
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&m.ID, &m.SourceServers, &m.TargetServer,
		&status, &m.StartedAt, &m.CompletedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("migration %s: %w", id, store.ErrNotFound)
		}
		return nil, fmt.Errorf("get migration %s: %w", id, err)
	}
	m.Status = model.MigrationStatus(status)
	return &m, nil
}

func (s *Store) UpdateMigrationStatus(ctx context.Context, id string, status model.MigrationStatus, completedAt *time.Time) error {
	const q = `UPDATE server_migrations SET status = $1, completed_at = $2 WHERE id = $3`
	tag, err := s.pool.Exec(ctx, q, string(status), completedAt, id)
	if err != nil {
		return fmt.Errorf("update migration status %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("migration %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func (s *Store) ListMigrations(ctx context.Context, limit int) ([]*model.Migration, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	const q = `
SELECT id, source_servers, target_server, status, started_at, completed_at
FROM server_migrations
ORDER BY started_at DESC
LIMIT $1
`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	defer rows.Close()

	var result []*model.Migration
	for rows.Next() {
		var m model.Migration
		var status string
		if err := rows.Scan(&m.ID, &m.SourceServers, &m.TargetServer, &status, &m.StartedAt, &m.CompletedAt); err != nil {
			return nil, fmt.Errorf("list migrations scan: %w", err)
		}
		m.Status = model.MigrationStatus(status)
		result = append(result, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list migrations iteration: %w", err)
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// StatsStore
// ---------------------------------------------------------------------------

func (s *Store) GetStats(ctx context.Context) (*model.Stats, error) {
	stats := &model.Stats{
		ServersByStatus:  make(map[string]int),
		ServersByRegion:  make(map[string]int),
		ServersByVersion: make(map[string]int),
	}

	// Total servers and aggregates.
	const serverQ = `SELECT status, region, version, capacity FROM servers`
	rows, err := s.pool.Query(ctx, serverQ)
	if err != nil {
		return nil, fmt.Errorf("get stats servers: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var status, region, version string
		var capacity int
		if err := rows.Scan(&status, &region, &version, &capacity); err != nil {
			return nil, fmt.Errorf("get stats scan: %w", err)
		}
		stats.TotalServers++
		stats.ServersByStatus[status]++
		stats.ServersByRegion[region]++
		stats.ServersByVersion[version]++
		stats.TotalCapacity += capacity
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get stats iteration: %w", err)
	}

	// Total characters.
	var totalChars int
	err = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM character_index`).Scan(&totalChars)
	if err != nil {
		return nil, fmt.Errorf("get stats character count: %w", err)
	}
	stats.TotalCharacters = totalChars

	// Player counts live in the runtime store (Redis in the default
	// deployment), not in SQL — merge them in when one is wired.
	if s.runtime != nil {
		runtimes, err := s.runtime.ListRuntimes(ctx)
		if err != nil {
			return nil, fmt.Errorf("get stats runtime players: %w", err)
		}
		for _, rt := range runtimes {
			stats.TotalPlayers += rt.Players
		}
	}

	stats.Finalize()

	return stats, nil
}

// ---------------------------------------------------------------------------
// scanner helpers
// ---------------------------------------------------------------------------

type scannable interface {
	Scan(dest ...any) error
}

func scanServer(row scannable) (*model.Server, error) {
	var srv model.Server
	var tagsRaw, metaRaw []byte
	err := row.Scan(
		&srv.ID, &srv.Name, &srv.Type, &srv.Region,
		&srv.RealmID, &srv.ShardID, &srv.Version, &srv.Platform,
		&srv.Endpoint.Host, &srv.Endpoint.Port, &srv.Capacity,
		&srv.Status, &srv.StartedAt, &srv.CreatedAt, &srv.UpdatedAt, &srv.Source,
		&tagsRaw,
		&srv.NotifyMode, &srv.NotifyCallbackURL, &metaRaw,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	if len(tagsRaw) > 0 {
		if err := json.Unmarshal(tagsRaw, &srv.Tags); err != nil {
			return nil, fmt.Errorf("decode server %s tags: %w", srv.ID, err)
		}
	}
	if len(metaRaw) > 2 { // skip NULL ('') and '{}' — leave Metadata nil
		if err := json.Unmarshal(metaRaw, &srv.Metadata); err != nil {
			return nil, fmt.Errorf("decode server %s metadata: %w", srv.ID, err)
		}
	}
	return &srv, nil
}

func scanCharacter(row scannable) (*model.Character, error) {
	var ch model.Character
	var metaRaw []byte
	err := row.Scan(
		&ch.AccountID, &ch.ServerID, &ch.CharacterID,
		&ch.Name, &ch.Level, &ch.ClassID, &ch.Avatar,
		&ch.LastLoginAt, &ch.CreatedAt, &ch.UpdatedAt, &metaRaw,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	if len(metaRaw) > 2 { // skip NULL ('') and '{}' — leave Metadata nil
		if err := json.Unmarshal(metaRaw, &ch.Metadata); err != nil {
			return nil, fmt.Errorf("decode character %d metadata: %w", ch.CharacterID, err)
		}
	}
	return &ch, nil
}

func scanCharacters(rows pgx.Rows) ([]*model.Character, error) {
	var result []*model.Character
	for rows.Next() {
		ch, err := scanCharacter(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func joinStrings(ss []string, sep string) string {
	if len(ss) == 0 {
		return ""
	}
	result := ss[0]
	for _, s := range ss[1:] {
		result += sep + s
	}
	return result
}

// splitCursor splits a "server_id:character_id" cursor into its parts.
func splitCursor(cursor string) []any {
	parts := strings.SplitN(cursor, ":", 2)
	if len(parts) == 2 {
		if charID, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			return []any{parts[0], charID}
		}
	}
	return []any{"", int64(0)}
}

// ── Realms & Shards (TODO v0.1.14) ──────────────────────────────

var (
	_ store.RealmStore = (*Store)(nil)
	_ store.ShardStore = (*Store)(nil)
)

func (s *Store) CreateRealm(ctx context.Context, r *model.Realm) error {
	if r.Status == "" {
		r.Status = "active"
	}
	// Stamp on the caller's object (parity with maintenance windows) so the
	// HTTP response carries the real created_at instead of a zero time.
	r.CreatedAt = time.Now()
	const q = `
INSERT INTO realms (id, name, region, status, created_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO NOTHING`
	tag, err := s.pool.Exec(ctx, q, r.ID, r.Name, r.Region, r.Status, r.CreatedAt)
	if err != nil {
		return fmt.Errorf("create realm: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("realm %s: %w", r.ID, store.ErrConflict)
	}
	return nil
}

func (s *Store) GetRealm(ctx context.Context, id string) (*model.Realm, error) {
	row := s.pool.QueryRow(ctx, `
SELECT id, name, region, status, created_at FROM realms WHERE id = $1`, id)
	return scanRealm(row)
}

func (s *Store) ListRealms(ctx context.Context, limit int) ([]*model.Realm, error) {
	q := `SELECT id, name, region, status, created_at FROM realms ORDER BY created_at DESC`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT $1`
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list realms: %w", err)
	}
	defer rows.Close()

	var out []*model.Realm
	for rows.Next() {
		r, err := scanRealm(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CreateShard(ctx context.Context, sh *model.Shard) error {
	if sh.Status == "" {
		sh.Status = "active"
	}
	// Stamp on the caller's object (parity with maintenance windows) so the
	// HTTP response carries the real created_at instead of a zero time.
	sh.CreatedAt = time.Now()
	const q = `
INSERT INTO shards (id, realm_id, name, status, created_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (id) DO NOTHING`
	tag, err := s.pool.Exec(ctx, q, sh.ID, sh.RealmID, sh.Name, sh.Status, sh.CreatedAt)
	if err != nil {
		// SQLSTATE 23503 = foreign_key_violation: the referenced realm does
		// not exist. Map it to ErrNotFound so callers see the same contract
		// as the memory store (which validates the realm explicitly).
		var fkErr *pgconn.PgError
		if errors.As(err, &fkErr) && fkErr.Code == "23503" {
			return fmt.Errorf("shard %s: realm %s: %w", sh.ID, sh.RealmID, store.ErrNotFound)
		}
		return fmt.Errorf("create shard: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("shard %s: %w", sh.ID, store.ErrConflict)
	}
	return nil
}

func (s *Store) GetShard(ctx context.Context, id string) (*model.Shard, error) {
	row := s.pool.QueryRow(ctx, `
SELECT id, realm_id, name, status, created_at FROM shards WHERE id = $1`, id)
	return scanShard(row)
}

func (s *Store) ListShards(ctx context.Context, realmID string, limit int) ([]*model.Shard, error) {
	q := `SELECT id, realm_id, name, status, created_at FROM shards`
	args := []any{}
	if realmID != "" {
		q += ` WHERE realm_id = $1`
		args = append(args, realmID)
	}
	q += ` ORDER BY created_at DESC`
	if limit > 0 {
		args = append(args, limit)
		q += fmt.Sprintf(` LIMIT $%d`, len(args))
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list shards: %w", err)
	}
	defer rows.Close()

	var out []*model.Shard
	for rows.Next() {
		sh, err := scanShard(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

func scanRealm(row scannable) (*model.Realm, error) {
	var r model.Realm
	if err := row.Scan(&r.ID, &r.Name, &r.Region, &r.Status, &r.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("realm: %w", store.ErrNotFound)
		}
		return nil, err
	}
	return &r, nil
}

func scanShard(row scannable) (*model.Shard, error) {
	var sh model.Shard
	if err := row.Scan(&sh.ID, &sh.RealmID, &sh.Name, &sh.Status, &sh.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("shard: %w", store.ErrNotFound)
		}
		return nil, err
	}
	return &sh, nil
}

// ── Maintenance windows & announcements (TODO v0.1.20) ──────────

func (s *Store) CreateMaintenanceWindow(ctx context.Context, w *model.MaintenanceWindow) error {
	w.CreatedAt = time.Now()
	const q = `
INSERT INTO maintenance_windows (id, server_id, start_at, end_at, previous_status, announcement_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO NOTHING`
	tag, err := s.pool.Exec(ctx, q,
		w.ID, w.ServerID, w.StartAt, w.EndAt, string(w.PreviousStatus), w.AnnouncementID, w.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create maintenance window %s: %w", w.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("maintenance window %s: %w", w.ID, store.ErrConflict)
	}
	return nil
}

func (s *Store) GetMaintenanceWindow(ctx context.Context, id string) (*model.MaintenanceWindow, error) {
	const q = `
SELECT id, server_id, start_at, end_at, previous_status, announcement_id, created_at
FROM maintenance_windows WHERE id = $1`
	return scanMaintenanceWindow(s.pool.QueryRow(ctx, q, id))
}

func (s *Store) DeleteMaintenanceWindow(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM maintenance_windows WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete maintenance window %s: %w", id, err)
	}
	return nil
}

func (s *Store) ListMaintenanceWindows(ctx context.Context, serverID string, limit int) ([]*model.MaintenanceWindow, error) {
	q := `SELECT id, server_id, start_at, end_at, previous_status, announcement_id, created_at
FROM maintenance_windows WHERE 1=1`
	args := []any{}
	n := 1
	if serverID != "" {
		q += fmt.Sprintf(" AND server_id = $%d", n)
		args = append(args, serverID)
		n++
	}
	q += " ORDER BY created_at DESC"
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT $%d", n)
		args = append(args, limit)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list maintenance windows: %w", err)
	}
	defer rows.Close()

	var out []*model.MaintenanceWindow
	for rows.Next() {
		w, err := scanMaintenanceWindow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) MarkMaintenanceWindowApplied(ctx context.Context, id string, previous model.ServerStatus) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE maintenance_windows SET previous_status = $2 WHERE id = $1`, id, string(previous))
	if err != nil {
		return fmt.Errorf("mark maintenance window %s applied: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("maintenance window %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func scanMaintenanceWindow(row scannable) (*model.MaintenanceWindow, error) {
	var w model.MaintenanceWindow
	var prev string
	err := row.Scan(&w.ID, &w.ServerID, &w.StartAt, &w.EndAt, &prev, &w.AnnouncementID, &w.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	w.PreviousStatus = model.ServerStatus(prev)
	return &w, nil
}

func (s *Store) CreateAnnouncement(ctx context.Context, a *model.Announcement) error {
	a.CreatedAt = time.Now()
	const q = `
INSERT INTO announcements (id, server_id, title, body, level, starts_at, ends_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO NOTHING`
	tag, err := s.pool.Exec(ctx, q,
		a.ID, a.ServerID, a.Title, a.Body, a.Level, a.StartsAt, a.EndsAt, a.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create announcement %s: %w", a.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("announcement %s: %w", a.ID, store.ErrConflict)
	}
	return nil
}

func (s *Store) GetAnnouncement(ctx context.Context, id string) (*model.Announcement, error) {
	const q = `
SELECT id, server_id, title, body, level, starts_at, ends_at, created_at
FROM announcements WHERE id = $1`
	return scanAnnouncement(s.pool.QueryRow(ctx, q, id))
}

func (s *Store) DeleteAnnouncement(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM announcements WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete announcement %s: %w", id, err)
	}
	return nil
}

func (s *Store) ListAnnouncements(ctx context.Context, f store.AnnouncementFilter) ([]*model.Announcement, error) {
	q := `SELECT id, server_id, title, body, level, starts_at, ends_at, created_at
FROM announcements WHERE 1=1`
	args := []any{}
	n := 1
	if f.ServerID != "" {
		// Global + the requested server.
		q += fmt.Sprintf(" AND (server_id IS NULL OR server_id = $%d)", n)
		args = append(args, f.ServerID)
		n++
	}
	if f.ActiveOnly {
		q += fmt.Sprintf(" AND starts_at <= now() AND ends_at > now()")
	}
	q += " ORDER BY created_at DESC"
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT $%d", n)
		args = append(args, f.Limit)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list announcements: %w", err)
	}
	defer rows.Close()

	var out []*model.Announcement
	for rows.Next() {
		a, err := scanAnnouncement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanAnnouncement(row scannable) (*model.Announcement, error) {
	var a model.Announcement
	err := row.Scan(&a.ID, &a.ServerID, &a.Title, &a.Body, &a.Level, &a.StartsAt, &a.EndsAt, &a.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}

// ── Cross-server config (config center) ──────────────────────────

// SaveCrossServerConfig persists spec as the single config row. The version
// is incremented atomically inside the upsert (version + 1 on conflict), so
// concurrent writers can never regress it and stale snapshots can't win.
func (s *Store) SaveCrossServerConfig(ctx context.Context, cfg *model.CrossServerConfig) (*model.CrossServerConfig, error) {
	specJSON, err := json.Marshal(cfg.Spec)
	if err != nil {
		return nil, fmt.Errorf("encode cross-server config: %w", err)
	}
	const q = `
INSERT INTO crossserver_config (id, version, hash, spec, updated_at)
VALUES ('default', 1, $1, $2, now())
ON CONFLICT (id) DO UPDATE SET
    version    = crossserver_config.version + 1,
    hash       = EXCLUDED.hash,
    spec       = EXCLUDED.spec,
    updated_at = now()
RETURNING version, updated_at
`
	saved := *cfg
	err = s.pool.QueryRow(ctx, q, cfg.Hash, specJSON).Scan(&saved.Version, &saved.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("save cross-server config: %w", err)
	}
	return &saved, nil
}

// GetCrossServerConfig returns the current config (ErrNotFound before the
// first save).
func (s *Store) GetCrossServerConfig(ctx context.Context) (*model.CrossServerConfig, error) {
	const q = `SELECT version, hash, spec, updated_at FROM crossserver_config WHERE id = 'default'`
	var cfg model.CrossServerConfig
	var specRaw []byte
	err := s.pool.QueryRow(ctx, q).Scan(&cfg.Version, &cfg.Hash, &specRaw, &cfg.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, fmt.Errorf("get cross-server config: %w", err)
	}
	if err := json.Unmarshal(specRaw, &cfg.Spec); err != nil {
		return nil, fmt.Errorf("decode cross-server config: %w", err)
	}
	return &cfg, nil
}

// metadataJSON marshals a metadata map for the JSONB columns; nil and empty
// maps both write '{}' so rows never carry NULL metadata.
func metadataJSON(m map[string]string) []byte {
	if len(m) == 0 {
		return []byte("{}")
	}
	b, err := json.Marshal(m)
	if err != nil {
		return []byte("{}")
	}
	return b
}
