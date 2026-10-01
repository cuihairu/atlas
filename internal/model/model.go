// Package model defines Atlas's core domain types: game servers, the character
// index, and the server lifecycle states described in docs/lifecycle.md.
//
// Character records in this package are a Projection, never a Source of Truth.
// The authoritative character database always lives behind the game server.
package model

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// ServerStatus is the lifecycle state of a game server. The full state machine
// is documented in docs/lifecycle.md.
type ServerStatus string

const (
	// StatusStarting means the process is up but not yet ready for traffic.
	StatusStarting ServerStatus = "starting"
	// StatusOnline is the only state that is both visible and accepting traffic.
	StatusOnline ServerStatus = "online"
	// StatusDraining stops accepting new connections while existing players finish.
	StatusDraining ServerStatus = "draining"
	// StatusMaintenance keeps existing players but rejects new ones, and stays
	// visible so the client can show a "under maintenance" badge.
	StatusMaintenance ServerStatus = "maintenance"
	// StatusSuspect means a heartbeat timed out but the server is not yet
	// confirmed dead. It stays visible to avoid client list churn on a blip.
	StatusSuspect ServerStatus = "suspect"
	// StatusOffline means the server is gone.
	StatusOffline ServerStatus = "offline"
	// StatusDisabled is an explicit operator action; the automatic state machine
	// never touches it.
	StatusDisabled ServerStatus = "disabled"
)

// AllServerStatuses lists every valid status in lifecycle order.
var AllServerStatuses = []ServerStatus{
	StatusStarting,
	StatusOnline,
	StatusDraining,
	StatusMaintenance,
	StatusSuspect,
	StatusOffline,
	StatusDisabled,
}

// Valid reports whether s is a recognised status.
func (s ServerStatus) Valid() bool {
	switch s {
	case StatusStarting, StatusOnline, StatusDraining,
		StatusMaintenance, StatusSuspect, StatusOffline, StatusDisabled:
		return true
	}
	return false
}

// Visible reports whether a server in this state should appear in discovery
// results by default.
//
// The rule is deliberate: a client must never see a dead server, but it must
// also never see a server "disappear" without explanation. maintenance and
// suspect stay visible and carry a badge instead.
func (s ServerStatus) Visible() bool {
	switch s {
	case StatusOnline, StatusMaintenance, StatusSuspect:
		return true
	}
	return false
}

// AcceptsTraffic reports whether new connections are allowed.
func (s ServerStatus) AcceptsTraffic() bool {
	return s == StatusOnline || s == StatusSuspect
}

// AutoManaged reports whether the health monitor may transition this status on
// its own. Operator-set states are left alone.
func (s ServerStatus) AutoManaged() bool {
	switch s {
	case StatusStarting, StatusOnline, StatusSuspect:
		return true
	}
	return false
}

// String implements fmt.Stringer.
func (s ServerStatus) String() string { return string(s) }

// Endpoint is the address a game client connects to.
type Endpoint struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// String renders the endpoint as "host:port".
func (e Endpoint) String() string {
	return net.JoinHostPort(e.Host, strconv.Itoa(e.Port))
}

// Server is a registered game server.
//
// Persistent facts (identity, topology, endpoint, capacity) live in PostgreSQL.
// Volatile fields (Players, Load, LastHeartbeatAt, and the live Status) live in
// Redis and are populated on read. See docs/data-model.md.
//
// Metadata carries arbitrary key-value pairs registered by the game server.
// Typical uses:
//   - KBEngine: {"engine":"kbengine", "baseapp_id":"baseapp-01", "cellapp_id":"cellapp-03"}
//   - Unreal:   {"engine":"unreal", "map":"lobby", "game_mode":"tdm"}
//   - Custom:   {"cluster":"shard-07", "zone":"pvp", "language":"zh-CN"}
//
// Metadata is stored as JSON and returned in discovery responses. It is not
// indexed for filtering in v0.1.1; use the server type or region for that.
type Server struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Region     string            `json:"region"`
	RealmID    *string           `json:"realm_id,omitempty"`
	ShardID    *string           `json:"shard_id,omitempty"`
	Version    string            `json:"version"`
	Platform   string            `json:"platform"`
	Endpoint   Endpoint          `json:"endpoint"`
	Capacity   int               `json:"capacity"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Status     ServerStatus      `json:"status"`
	Players    int               `json:"players"`
	Load       float64           `json:"load"`
	LastSeenAt *time.Time        `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// Heartbeat is the periodic report a game server sends to Atlas.
type Heartbeat struct {
	Players int          `json:"players"`
	Load    float64      `json:"load"`
	Status  ServerStatus `json:"status,omitempty"`
}

// Validate checks the heartbeat payload. Load is a ratio in [0, 1].
func (h Heartbeat) Validate() error {
	if h.Players < 0 {
		return fmt.Errorf("%w: players must be >= 0", ErrInvalid)
	}
	if h.Load < 0 || h.Load > 1 {
		return fmt.Errorf("%w: load must be within [0, 1]", ErrInvalid)
	}
	if h.Status != "" && !h.Status.Valid() {
		return fmt.Errorf("%w: unknown status %q", ErrInvalid, h.Status)
	}
	return nil
}

// Runtime is the volatile, Redis-backed view of a live server.
type Runtime struct {
	Status     ServerStatus `json:"status"`
	Players    int          `json:"players"`
	Load       float64      `json:"load"`
	LastSeenAt time.Time    `json:"last_seen_at"`
}

// Validate checks a Server for structural correctness before registration.
func (s *Server) Validate() error {
	if strings.TrimSpace(s.ID) == "" {
		return fmt.Errorf("%w: server id is required", ErrInvalid)
	}
	if strings.TrimSpace(s.Region) == "" {
		return fmt.Errorf("%w: region is required", ErrInvalid)
	}
	if strings.TrimSpace(s.Endpoint.Host) == "" {
		return fmt.Errorf("%w: endpoint host is required", ErrInvalid)
	}
	if s.Endpoint.Port <= 0 || s.Endpoint.Port > 65535 {
		return fmt.Errorf("%w: endpoint port must be within [1, 65535]", ErrInvalid)
	}
	if s.Capacity < 0 {
		return fmt.Errorf("%w: capacity must be >= 0", ErrInvalid)
	}
	if s.Status != "" && !s.Status.Valid() {
		return fmt.Errorf("%w: unknown status %q", ErrInvalid, s.Status)
	}
	return nil
}

// OverCapacity reports whether the server is full. Routing uses this to avoid
// recommending a saturated server.
func (s *Server) OverCapacity() bool {
	return s.Capacity > 0 && s.Players >= s.Capacity
}

// Character is a projection of a game character's index entry in Atlas.
//
// This is NOT the authoritative character data. The full character lives in the
// game server's own database. Atlas only keeps enough for cross-server
// character listing and routing decisions. See docs/concepts.md §7.
//
// Metadata carries arbitrary key-value pairs synced from the game server.
// Typical uses:
//   - KBEngine: {"baseapp_id":"baseapp-01", "dbid":"123456"}
//   - General:  {"guild":"星辰", "vip_level":"6", "scene_id":"lobby-03"}
//
// Metadata is stored as JSON and returned in directory responses. It enables
// fast lookup of which backend process a character is currently bound to.
type Character struct {
	AccountID   int64             `json:"account_id"`
	ServerID    string            `json:"server_id"`
	CharacterID int64             `json:"character_id"`
	Name        string            `json:"name"`
	Level       int               `json:"level"`
	ClassID     int               `json:"class_id"`
	Avatar      string            `json:"avatar,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	LastLoginAt *time.Time        `json:"last_login_at,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// MigrationStatus is the lifecycle state of a server migration.
type MigrationStatus string

const (
	MigrationPending    MigrationStatus = "pending"
	MigrationMigrating  MigrationStatus = "migrating"
	MigrationVerifying  MigrationStatus = "verifying"
	MigrationCompleted  MigrationStatus = "completed"
	MigrationFailed     MigrationStatus = "failed"
	MigrationRolledBack MigrationStatus = "rolled_back"
)

// Valid reports whether s is a recognised migration status.
func (s MigrationStatus) Valid() bool {
	switch s {
	case MigrationPending, MigrationMigrating, MigrationVerifying,
		MigrationCompleted, MigrationFailed, MigrationRolledBack:
		return true
	}
	return false
}

// String implements fmt.Stringer.
func (s MigrationStatus) String() string { return string(s) }

// Migration represents a server migration (merge / transfer / relocate).
type Migration struct {
	ID            string          `json:"id"`
	SourceServers []string        `json:"source_servers"`
	TargetServer  string          `json:"target_server"`
	Status        MigrationStatus `json:"status"`
	StartedAt     time.Time       `json:"started_at"`
	CompletedAt   *time.Time      `json:"completed_at,omitempty"`
}

// Stats aggregates server and character counts for the admin dashboard.
type Stats struct {
	TotalServers     int            `json:"total_servers"`
	ServersByStatus  map[string]int `json:"servers_by_status"`
	ServersByRegion  map[string]int `json:"servers_by_region"`
	ServersByVersion map[string]int `json:"servers_by_version"`
	TotalPlayers     int            `json:"total_players"`
	TotalCapacity    int            `json:"total_capacity"`
	TotalCharacters  int            `json:"total_characters"`
}

// Realm is an administrative grouping of servers (a logical game world or
// region above servers). Servers reference realms via RealmID; the schema
// lives in migrations/0001_init.sql.
type Realm struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Region    string    `json:"region"`
	Status    string    `json:"status"` // active | ...
	CreatedAt time.Time `json:"created_at"`
}

// Shard is a subdivision of a realm (a content shard servers can join).
// Servers reference shards via ShardID.
type Shard struct {
	ID        string    `json:"id"`
	RealmID   string    `json:"realm_id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"` // active | ...
	CreatedAt time.Time `json:"created_at"`
}
