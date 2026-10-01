// Package mysql provides a MySQL implementation of store.ServerStore,
// store.CharacterStore, store.MigrationStore, and store.StatsStore
// using database/sql with go-sql-driver/mysql.
package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

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

// Store implements the Atlas store interfaces on MySQL.
type Store struct {
	db *sql.DB
}

// New creates a new MySQL store from an existing *sql.DB.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

// Ping checks the database connection.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// Close closes the underlying connection pool.
func (s *Store) Close() error {
	return s.db.Close()
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
                     endpoint_host, endpoint_port, capacity, status, started_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    name          = VALUES(name),
    type          = VALUES(type),
    region        = VALUES(region),
    realm_id      = VALUES(realm_id),
    shard_id      = VALUES(shard_id),
    version       = VALUES(version),
    platform      = VALUES(platform),
    endpoint_host = VALUES(endpoint_host),
    endpoint_port = VALUES(endpoint_port),
    capacity      = VALUES(capacity),
    -- Reset only a dead-ish lifecycle (suspect / offline) so a re-registering
    -- server can recover; keep active and operator-set statuses untouched.
    status        = CASE WHEN servers.status IN ('suspect', 'offline') THEN VALUES(status) ELSE servers.status END,
    started_at    = VALUES(started_at),
    updated_at    = VALUES(updated_at)
`
	_, err := s.db.ExecContext(ctx, q,
		srv.ID, srv.Name, srv.Type, srv.Region,
		srv.RealmID, srv.ShardID, srv.Version, srv.Platform,
		srv.Endpoint.Host, srv.Endpoint.Port, srv.Capacity,
		string(srv.Status), srv.StartedAt, srv.CreatedAt, srv.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("register server %s: %w", srv.ID, err)
	}
	return nil
}

func (s *Store) GetServer(ctx context.Context, id string) (*model.Server, error) {
	const q = `
SELECT id, name, type, region, realm_id, shard_id, version, platform,
       endpoint_host, endpoint_port, capacity, status, started_at, created_at, updated_at
FROM servers WHERE id = ?
`
	srv, err := scanServer(s.db.QueryRowContext(ctx, q, id))
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
	if limit > 200 {
		limit = 200
	}

	q := `SELECT id, name, type, region, realm_id, shard_id, version, platform,
       endpoint_host, endpoint_port, capacity, status, started_at, created_at, updated_at
FROM servers WHERE 1=1`
	args := []any{}

	if f.Region != "" {
		q += " AND region = ?"
		args = append(args, f.Region)
	}
	if f.Realm != "" {
		q += " AND realm_id = ?"
		args = append(args, f.Realm)
	}
	if f.Shard != "" {
		q += " AND shard_id = ?"
		args = append(args, f.Shard)
	}
	if f.Version != "" {
		q += " AND version = ?"
		args = append(args, f.Version)
	}
	if f.Platform != "" {
		q += " AND platform = ?"
		args = append(args, f.Platform)
	}
	if f.Status != "" {
		q += " AND status = ?"
		args = append(args, string(f.Status))
	}
	if f.Cursor != "" {
		q += " AND id > ?"
		args = append(args, f.Cursor)
	}

	q += " ORDER BY id LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
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
	const q = `UPDATE servers SET status = ?, updated_at = ? WHERE id = ?`
	tag, err := s.db.ExecContext(ctx, q, string(status), time.Now(), id)
	if err != nil {
		return fmt.Errorf("update server status %s: %w", id, err)
	}
	n, _ := tag.RowsAffected()
	if n == 0 {
		return fmt.Errorf("server %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func (s *Store) DeleteServer(ctx context.Context, id string) error {
	const q = `DELETE FROM servers WHERE id = ?`
	tag, err := s.db.ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("delete server %s: %w", id, err)
	}
	n, _ := tag.RowsAffected()
	if n == 0 {
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
INSERT INTO character_index (account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    name          = VALUES(name),
    level         = VALUES(level),
    class_id      = VALUES(class_id),
    avatar        = VALUES(avatar),
    last_login_at = VALUES(last_login_at),
    updated_at    = VALUES(updated_at)
`
	_, err := s.db.ExecContext(ctx, q,
		ch.AccountID, ch.ServerID, ch.CharacterID,
		ch.Name, ch.Level, ch.ClassID, ch.Avatar,
		ch.LastLoginAt, ch.CreatedAt, ch.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert character: %w", err)
	}
	return nil
}

func (s *Store) GetCharacter(ctx context.Context, accountID int64, serverID string, characterID int64) (*model.Character, error) {
	const q = `
SELECT account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at
FROM character_index
WHERE account_id = ? AND server_id = ? AND character_id = ?
`
	ch, err := scanCharacter(s.db.QueryRowContext(ctx, q, accountID, serverID, characterID))
	if err != nil {
		return nil, fmt.Errorf("get character: %w", err)
	}
	return ch, nil
}

func (s *Store) GetCharacterByCharacterID(ctx context.Context, characterID int64) (*model.Character, error) {
	const q = `
SELECT account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at
FROM character_index
WHERE character_id = ?
`
	ch, err := scanCharacter(s.db.QueryRowContext(ctx, q, characterID))
	if err != nil {
		return nil, fmt.Errorf("get character by id: %w", err)
	}
	return ch, nil
}

func (s *Store) UpdateCharacter(ctx context.Context, accountID int64, serverID string, characterID int64, patch store.CharacterPatch) error {
	setClauses := []string{}
	args := []any{}

	if patch.Name != nil {
		setClauses = append(setClauses, "name = ?")
		args = append(args, *patch.Name)
	}
	if patch.Level != nil {
		setClauses = append(setClauses, "level = ?")
		args = append(args, *patch.Level)
	}
	if patch.ClassID != nil {
		setClauses = append(setClauses, "class_id = ?")
		args = append(args, *patch.ClassID)
	}
	if patch.Avatar != nil {
		setClauses = append(setClauses, "avatar = ?")
		args = append(args, *patch.Avatar)
	}
	if patch.LastLoginAt != nil {
		setClauses = append(setClauses, "last_login_at = ?")
		args = append(args, *patch.LastLoginAt)
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, "updated_at = ?")
	args = append(args, time.Now())

	q := fmt.Sprintf("UPDATE character_index SET %s WHERE account_id = ? AND server_id = ? AND character_id = ?",
		joinStrings(setClauses, ", "))
	args = append(args, accountID, serverID, characterID)

	tag, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("update character: %w", err)
	}
	n, _ := tag.RowsAffected()
	if n == 0 {
		return fmt.Errorf("character (account=%d, server=%s, char=%d): %w", accountID, serverID, characterID, store.ErrNotFound)
	}
	return nil
}

func (s *Store) DeleteCharacter(ctx context.Context, accountID int64, serverID string, characterID int64) error {
	const q = `DELETE FROM character_index WHERE account_id = ? AND server_id = ? AND character_id = ?`
	tag, err := s.db.ExecContext(ctx, q, accountID, serverID, characterID)
	if err != nil {
		return fmt.Errorf("delete character: %w", err)
	}
	n, _ := tag.RowsAffected()
	if n == 0 {
		return fmt.Errorf("character (account=%d, server=%s, char=%d): %w", accountID, serverID, characterID, store.ErrNotFound)
	}
	return nil
}

func (s *Store) ListCharactersByAccount(ctx context.Context, accountID int64) ([]*model.Character, error) {
	const q = `
SELECT account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at
FROM character_index
WHERE account_id = ?
ORDER BY server_id, character_id
`
	rows, err := s.db.QueryContext(ctx, q, accountID)
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
SELECT account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at
FROM character_index
WHERE server_id = ?`
	args := []any{serverID}

	if cursor != "" {
		q += " AND character_id > ?"
		args = append(args, cursor)
	}

	q += " ORDER BY character_id LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
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

	q := `SELECT account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at
FROM character_index WHERE 1=1`
	args := []any{}

	if filter.Name != "" {
		q += " AND LOWER(name) LIKE LOWER(?)"
		args = append(args, "%"+filter.Name+"%")
	}
	if filter.ServerID != "" {
		q += " AND server_id = ?"
		args = append(args, filter.ServerID)
	}
	if filter.ClassID != nil {
		q += " AND class_id = ?"
		args = append(args, *filter.ClassID)
	}
	if filter.MinLevel != nil {
		q += " AND level >= ?"
		args = append(args, *filter.MinLevel)
	}
	if filter.MaxLevel != nil {
		q += " AND level <= ?"
		args = append(args, *filter.MaxLevel)
	}
	if filter.Cursor != "" {
		q += " AND (server_id, character_id) > (?, ?)"
		parts := splitCursor(filter.Cursor)
		args = append(args, parts[0], parts[1])
	}

	q += " ORDER BY server_id, character_id LIMIT ?"
	args = append(args, filter.Limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
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
	// MySQL doesn't have a native array type; store source_servers as JSON.
	serversJSON, err := json.Marshal(m.SourceServers)
	if err != nil {
		return fmt.Errorf("marshal source_servers: %w", err)
	}

	const q = `
INSERT INTO server_migrations (id, source_servers, target_server, status, started_at, completed_at)
VALUES (?, ?, ?, ?, ?, ?)
`
	_, err = s.db.ExecContext(ctx, q,
		m.ID, string(serversJSON), m.TargetServer,
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
FROM server_migrations WHERE id = ?
`
	var m model.Migration
	var status string
	var serversJSON string
	err := s.db.QueryRowContext(ctx, q, id).Scan(
		&m.ID, &serversJSON, &m.TargetServer,
		&status, &m.StartedAt, &m.CompletedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("migration %s: %w", id, store.ErrNotFound)
		}
		return nil, fmt.Errorf("get migration %s: %w", id, err)
	}
	m.Status = model.MigrationStatus(status)
	if err := json.Unmarshal([]byte(serversJSON), &m.SourceServers); err != nil {
		return nil, fmt.Errorf("unmarshal source_servers: %w", err)
	}
	return &m, nil
}

func (s *Store) UpdateMigrationStatus(ctx context.Context, id string, status model.MigrationStatus, completedAt *time.Time) error {
	const q = `UPDATE server_migrations SET status = ?, completed_at = ? WHERE id = ?`
	tag, err := s.db.ExecContext(ctx, q, string(status), completedAt, id)
	if err != nil {
		return fmt.Errorf("update migration status %s: %w", id, err)
	}
	n, _ := tag.RowsAffected()
	if n == 0 {
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
LIMIT ?
`
	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	defer rows.Close()

	var result []*model.Migration
	for rows.Next() {
		var m model.Migration
		var status string
		var serversJSON string
		if err := rows.Scan(&m.ID, &serversJSON, &m.TargetServer, &status, &m.StartedAt, &m.CompletedAt); err != nil {
			return nil, fmt.Errorf("list migrations scan: %w", err)
		}
		m.Status = model.MigrationStatus(status)
		if err := json.Unmarshal([]byte(serversJSON), &m.SourceServers); err != nil {
			return nil, fmt.Errorf("unmarshal source_servers: %w", err)
		}
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

	const serverQ = `SELECT status, region, version, capacity FROM servers`
	rows, err := s.db.QueryContext(ctx, serverQ)
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

	var totalChars int
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM character_index`).Scan(&totalChars)
	if err != nil {
		return nil, fmt.Errorf("get stats character count: %w", err)
	}
	stats.TotalCharacters = totalChars

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
	err := row.Scan(
		&srv.ID, &srv.Name, &srv.Type, &srv.Region,
		&srv.RealmID, &srv.ShardID, &srv.Version, &srv.Platform,
		&srv.Endpoint.Host, &srv.Endpoint.Port, &srv.Capacity,
		&srv.Status, &srv.StartedAt, &srv.CreatedAt, &srv.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &srv, nil
}

func scanCharacter(row scannable) (*model.Character, error) {
	var ch model.Character
	err := row.Scan(
		&ch.AccountID, &ch.ServerID, &ch.CharacterID,
		&ch.Name, &ch.Level, &ch.ClassID, &ch.Avatar,
		&ch.LastLoginAt, &ch.CreatedAt, &ch.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &ch, nil
}

func scanCharacters(rows *sql.Rows) ([]*model.Character, error) {
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
VALUES (?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, q, r.ID, r.Name, r.Region, r.Status, r.CreatedAt); err != nil {
		_, lookupErr := s.GetRealm(ctx, r.ID)
		return conflictIfExists(err, lookupErr, "create realm", r.ID)
	}
	return nil
}

func (s *Store) GetRealm(ctx context.Context, id string) (*model.Realm, error) {
	const q = `SELECT id, name, region, status, created_at FROM realms WHERE id = ?`
	var r model.Realm
	err := s.db.QueryRowContext(ctx, q, id).Scan(&r.ID, &r.Name, &r.Region, &r.Status, &r.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("realm %s: %w", id, store.ErrNotFound)
		}
		return nil, fmt.Errorf("get realm %s: %w", id, err)
	}
	return &r, nil
}

func (s *Store) ListRealms(ctx context.Context, limit int) ([]*model.Realm, error) {
	q := `SELECT id, name, region, status, created_at FROM realms ORDER BY created_at DESC`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list realms: %w", err)
	}
	defer rows.Close()

	var out []*model.Realm
	for rows.Next() {
		var r model.Realm
		if err := rows.Scan(&r.ID, &r.Name, &r.Region, &r.Status, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan realm: %w", err)
		}
		out = append(out, &r)
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
VALUES (?, ?, ?, ?, ?)`
	if _, err := s.db.ExecContext(ctx, q, sh.ID, sh.RealmID, sh.Name, sh.Status, sh.CreatedAt); err != nil {
		_, lookupErr := s.GetShard(ctx, sh.ID)
		return conflictIfExists(err, lookupErr, "create shard", sh.ID)
	}
	return nil
}

func (s *Store) GetShard(ctx context.Context, id string) (*model.Shard, error) {
	const q = `SELECT id, realm_id, name, status, created_at FROM shards WHERE id = ?`
	var sh model.Shard
	err := s.db.QueryRowContext(ctx, q, id).Scan(&sh.ID, &sh.RealmID, &sh.Name, &sh.Status, &sh.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("shard %s: %w", id, store.ErrNotFound)
		}
		return nil, fmt.Errorf("get shard %s: %w", id, err)
	}
	return &sh, nil
}

func (s *Store) ListShards(ctx context.Context, realmID string, limit int) ([]*model.Shard, error) {
	q := `SELECT id, realm_id, name, status, created_at FROM shards`
	args := []any{}
	if realmID != "" {
		q += ` WHERE realm_id = ?`
		args = append(args, realmID)
	}
	q += ` ORDER BY created_at DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list shards: %w", err)
	}
	defer rows.Close()

	var out []*model.Shard
	for rows.Next() {
		var sh model.Shard
		if err := rows.Scan(&sh.ID, &sh.RealmID, &sh.Name, &sh.Status, &sh.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan shard: %w", err)
		}
		out = append(out, &sh)
	}
	return out, rows.Err()
}

// conflictIfExists maps an INSERT failure to ErrConflict when a follow-up
// lookup finds the row (driver-neutral duplicate detection: the sql driver
// is supplied by the embedding application, not this package).
func conflictIfExists(execErr error, lookupErr error, op, id string) error {
	if lookupErr == nil {
		return fmt.Errorf("%s %s: %w", op, id, store.ErrConflict)
	}
	return fmt.Errorf("%s %s: %w", op, id, execErr)
}

// ── Maintenance windows & announcements (TODO v0.1.20) ──────────

func (s *Store) CreateMaintenanceWindow(ctx context.Context, w *model.MaintenanceWindow) error {
	w.CreatedAt = time.Now()
	const q = `
INSERT INTO maintenance_windows (id, server_id, start_at, end_at, previous_status, announcement_id, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE id = id`
	res, err := s.db.ExecContext(ctx, q,
		w.ID, w.ServerID, w.StartAt, w.EndAt, string(w.PreviousStatus), w.AnnouncementID, w.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create maintenance window %s: %w", w.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("maintenance window %s: %w", w.ID, store.ErrConflict)
	}
	return nil
}

func (s *Store) GetMaintenanceWindow(ctx context.Context, id string) (*model.MaintenanceWindow, error) {
	const q = `
SELECT id, server_id, start_at, end_at, previous_status, announcement_id, created_at
FROM maintenance_windows WHERE id = ?`
	return scanMaintenanceWindow(s.db.QueryRowContext(ctx, q, id))
}

func (s *Store) DeleteMaintenanceWindow(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM maintenance_windows WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete maintenance window %s: %w", id, err)
	}
	return nil
}

func (s *Store) ListMaintenanceWindows(ctx context.Context, serverID string, limit int) ([]*model.MaintenanceWindow, error) {
	q := `SELECT id, server_id, start_at, end_at, previous_status, announcement_id, created_at
FROM maintenance_windows WHERE 1=1`
	args := []any{}
	if serverID != "" {
		q += " AND server_id = ?"
		args = append(args, serverID)
	}
	q += " ORDER BY created_at DESC"
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
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
	res, err := s.db.ExecContext(ctx,
		`UPDATE maintenance_windows SET previous_status = ? WHERE id = ?`, string(previous), id)
	if err != nil {
		return fmt.Errorf("mark maintenance window %s applied: %w", id, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("maintenance window %s: %w", id, store.ErrNotFound)
	}
	return nil
}

func scanMaintenanceWindow(row scannable) (*model.MaintenanceWindow, error) {
	var w model.MaintenanceWindow
	var prev string
	err := row.Scan(&w.ID, &w.ServerID, &w.StartAt, &w.EndAt, &prev, &w.AnnouncementID, &w.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
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
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE id = id`
	res, err := s.db.ExecContext(ctx, q,
		a.ID, a.ServerID, a.Title, a.Body, a.Level, a.StartsAt, a.EndsAt, a.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create announcement %s: %w", a.ID, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("announcement %s: %w", a.ID, store.ErrConflict)
	}
	return nil
}

func (s *Store) GetAnnouncement(ctx context.Context, id string) (*model.Announcement, error) {
	const q = `
SELECT id, server_id, title, body, level, starts_at, ends_at, created_at
FROM announcements WHERE id = ?`
	return scanAnnouncement(s.db.QueryRowContext(ctx, q, id))
}

func (s *Store) DeleteAnnouncement(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM announcements WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete announcement %s: %w", id, err)
	}
	return nil
}

func (s *Store) ListAnnouncements(ctx context.Context, f store.AnnouncementFilter) ([]*model.Announcement, error) {
	q := `SELECT id, server_id, title, body, level, starts_at, ends_at, created_at
FROM announcements WHERE 1=1`
	args := []any{}
	if f.ServerID != "" {
		// Global + the requested server.
		q += " AND (server_id IS NULL OR server_id = ?)"
		args = append(args, f.ServerID)
	}
	if f.ActiveOnly {
		q += " AND starts_at <= NOW(6) AND ends_at > NOW(6)"
	}
	q += " ORDER BY created_at DESC"
	if f.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
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
		if err == sql.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	return &a, nil
}
