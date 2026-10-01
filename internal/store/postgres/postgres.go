// Package postgres provides a PostgreSQL implementation of store.ServerStore
// and store.CharacterStore using pgxpool.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// compile-time interface checks
var (
	_ store.ServerStore    = (*Store)(nil)
	_ store.CharacterStore = (*Store)(nil)
)

// Store implements store.ServerStore and store.CharacterStore on PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

// New creates a new PostgreSQL store from an existing pgxpool.Pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
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
                     endpoint_host, endpoint_port, capacity, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
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
    status        = CASE WHEN EXCLUDED.status = 'starting' THEN servers.status ELSE EXCLUDED.status END,
    updated_at    = EXCLUDED.updated_at
`
	_, err := s.pool.Exec(ctx, q,
		srv.ID, srv.Name, srv.Type, srv.Region,
		srv.RealmID, srv.ShardID, srv.Version, srv.Platform,
		srv.Endpoint.Host, srv.Endpoint.Port, srv.Capacity,
		srv.Status, srv.CreatedAt, srv.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("register server %s: %w", srv.ID, err)
	}
	return nil
}

func (s *Store) GetServer(ctx context.Context, id string) (*model.Server, error) {
	const q = `
SELECT id, name, type, region, realm_id, shard_id, version, platform,
       endpoint_host, endpoint_port, capacity, status, created_at, updated_at
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
	if limit > 200 {
		limit = 200
	}

	q := `SELECT id, name, type, region, realm_id, shard_id, version, platform,
       endpoint_host, endpoint_port, capacity, status, created_at, updated_at
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
INSERT INTO character_index (account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (account_id, server_id, character_id) DO UPDATE SET
    name          = EXCLUDED.name,
    level         = EXCLUDED.level,
    class_id      = EXCLUDED.class_id,
    avatar        = EXCLUDED.avatar,
    last_login_at = EXCLUDED.last_login_at,
    updated_at    = EXCLUDED.updated_at
`
	_, err := s.pool.Exec(ctx, q,
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
SELECT account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at
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
SELECT account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at
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
SELECT account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at
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
		&srv.Status, &srv.CreatedAt, &srv.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
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
		if err == pgx.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, err
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