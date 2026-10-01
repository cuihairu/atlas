// Package store defines the storage interfaces for Atlas.
//
// Implementations live in sub-packages (memory, postgres, redisstore).
package store

import (
	"context"
	"errors"
	"time"

	"github.com/cuihairu/atlas/internal/model"
)

// Re-export sentinel errors from the model package for convenience.
var (
	ErrNotFound = model.ErrNotFound
	ErrConflict = model.ErrConflict
	ErrInvalid  = model.ErrInvalid
)

// ServerFilter controls which servers are returned by ListServers.
type ServerFilter struct {
	Region   string
	Realm    string
	Shard    string
	Version  string
	Platform string
	Status   model.ServerStatus
	Limit    int
	Cursor   string
}

// CharacterPatch contains optional fields for updating a character index entry.
// A nil pointer means "do not change this field".
type CharacterPatch struct {
	Name        *string
	Level       *int
	ClassID     *int
	Avatar      *string
	Metadata    *map[string]string
	LastLoginAt *time.Time
}

// CharacterSearchFilter controls which characters are returned by SearchCharacters.
type CharacterSearchFilter struct {
	Name     string
	ServerID string
	ClassID  *int
	MinLevel *int
	MaxLevel *int
	Limit    int
	Cursor   string
}

// ServerStore persists game server records.
type ServerStore interface {
	// RegisterServer upserts a server record. It is idempotent: if a server
	// with the same ID already exists, its mutable fields are updated and nil
	// is returned (not ErrConflict).
	RegisterServer(ctx context.Context, s *model.Server) error

	// GetServer returns a single server by ID.
	GetServer(ctx context.Context, id string) (*model.Server, error)

	// ListServers returns servers matching the filter, sorted by ID,
	// paginated with cursor-based pagination.
	ListServers(ctx context.Context, f ServerFilter) ([]*model.Server, error)

	// UpdateServerStatus changes the status field of a server.
	UpdateServerStatus(ctx context.Context, id string, status model.ServerStatus) error

	// DeleteServer removes a server record entirely.
	DeleteServer(ctx context.Context, id string) error
}

// CharacterStore persists the character index (projection).
type CharacterStore interface {
	// UpsertCharacter inserts or updates a character index entry keyed by
	// (accountID, serverID, characterID).
	UpsertCharacter(ctx context.Context, ch *model.Character) error

	// GetCharacter returns a character by its composite key.
	GetCharacter(ctx context.Context, accountID int64, serverID string, characterID int64) (*model.Character, error)

	// GetCharacterByCharacterID returns a character by its global character ID.
	GetCharacterByCharacterID(ctx context.Context, characterID int64) (*model.Character, error)

	// UpdateCharacter applies a partial patch to a character.
	UpdateCharacter(ctx context.Context, accountID int64, serverID string, characterID int64, patch CharacterPatch) error

	// DeleteCharacter removes a character index entry.
	DeleteCharacter(ctx context.Context, accountID int64, serverID string, characterID int64) error

	// ListCharactersByAccount returns all characters belonging to an account.
	ListCharactersByAccount(ctx context.Context, accountID int64) ([]*model.Character, error)

	// ListCharactersByServer returns characters on a given server, with
	// cursor-based pagination.
	ListCharactersByServer(ctx context.Context, serverID string, limit int, cursor string) ([]*model.Character, error)

	// SearchCharacters searches characters with filters and cursor pagination.
	// Returns the matching characters and a cursor for the next page.
	SearchCharacters(ctx context.Context, filter CharacterSearchFilter) ([]*model.Character, string, error)
}

// MigrationStore persists server migration records.
type MigrationStore interface {
	// CreateMigration inserts a new migration record.
	CreateMigration(ctx context.Context, m *model.Migration) error

	// GetMigration returns a migration by ID.
	GetMigration(ctx context.Context, id string) (*model.Migration, error)

	// UpdateMigrationStatus changes the status of a migration.
	UpdateMigrationStatus(ctx context.Context, id string, status model.MigrationStatus, completedAt *time.Time) error

	// ListMigrations returns recent migrations, ordered by start time descending.
	ListMigrations(ctx context.Context, limit int) ([]*model.Migration, error)
}

// StatsStore provides aggregate statistics about servers and characters.
type StatsStore interface {
	// GetStats returns aggregated counts across all servers and characters.
	GetStats(ctx context.Context) (*model.Stats, error)
}

// RealmStore manages realm records (administrative server groupings,
// TODO v0.1.14).
type RealmStore interface {
	// CreateRealm inserts a realm. ErrConflict when the ID already exists.
	CreateRealm(ctx context.Context, r *model.Realm) error

	// GetRealm returns a realm by ID (ErrNotFound when absent).
	GetRealm(ctx context.Context, id string) (*model.Realm, error)

	// ListRealms returns realms ordered by created_at descending, capped
	// at limit (<= 0 means no cap).
	ListRealms(ctx context.Context, limit int) ([]*model.Realm, error)
}

// ShardStore manages shard records (realm subdivisions, TODO v0.1.14).
type ShardStore interface {
	// CreateShard inserts a shard. ErrConflict when the ID already exists.
	CreateShard(ctx context.Context, s *model.Shard) error

	// GetShard returns a shard by ID (ErrNotFound when absent).
	GetShard(ctx context.Context, id string) (*model.Shard, error)

	// ListShards returns shards ordered by created_at descending, capped
	// at limit (<= 0 means no cap). A non-empty realmID narrows to that
	// realm's shards.
	ListShards(ctx context.Context, realmID string, limit int) ([]*model.Shard, error)
}

// RuntimeStore persists volatile server runtime data (heartbeat, load, etc.).
type RuntimeStore interface {
	// RecordHeartbeat writes the runtime snapshot for a server.
	RecordHeartbeat(ctx context.Context, id string, hb model.Heartbeat) error

	// GetRuntime returns the latest runtime snapshot for a server.
	GetRuntime(ctx context.Context, id string) (*model.Runtime, error)

	// DeleteRuntime removes the runtime data for a server.
	DeleteRuntime(ctx context.Context, id string) error
}

// Store composes all storage interfaces.
type Store interface {
	ServerStore
	CharacterStore
	RuntimeStore
	MigrationStore
	StatsStore
	RealmStore
	ShardStore

	// Ping verifies the store is reachable.
	Ping(ctx context.Context) error

	// Close releases resources held by the store.
	Close() error
}

// IsNotFound reports whether err is or wraps ErrNotFound.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}