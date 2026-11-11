// Package atlas is the Go client SDK for Atlas (TODO v0.1.6).
//
// It speaks both REST and gRPC transports (Options.Transport) against a
// running Atlas instance and mirrors the five API groups from docs/api.md:
// Registry, Discovery, Directory, Routing and Admin.
package atlas

import (
	"encoding/json"
	"time"
)

// Transport selects the wire protocol the Client uses.
type Transport string

const (
	// TransportREST talks HTTP JSON on the public/registry/admin ports
	// (default). All endpoints share one base address in a typical
	// same-host deployment; per-group base URLs can be overridden.
	TransportREST Transport = "rest"

	// TransportGRPC talks gRPC on the ATLAS_GRPC_ADDR port.
	TransportGRPC Transport = "grpc"
)

// Endpoint is a game server's connect address.
type Endpoint struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// Server is a registered server as returned by Discovery.
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
	Status     string            `json:"status"`
	Players    int               `json:"players"`
	Load       float64           `json:"load"`
	LastSeenAt *time.Time        `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// ServerFilter narrows ListServers. Empty fields are not sent.
type ServerFilter struct {
	Region   string `json:"region,omitempty"`
	Version  string `json:"version,omitempty"`
	Platform string `json:"platform,omitempty"`
	Status   string `json:"status,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// RegisterRequest describes a server joining the fleet.
type RegisterRequest struct {
	ServerID string   `json:"server_id"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Region   string   `json:"region"`
	RealmID  *string  `json:"realm_id,omitempty"`
	ShardID  *string  `json:"shard_id,omitempty"`
	Version  string   `json:"version"`
	Platform string   `json:"platform"`
	Endpoint Endpoint `json:"endpoint"`
	Capacity int      `json:"capacity"`
	// NotifyMode declares how this server receives cross-server config
	// change signals: subscribe | callback | poll (config.go).
	NotifyMode string `json:"notify_mode,omitempty"`
	// NotifyCallbackURL is required with NotifyModeCallback: the absolute
	// http(s) URL Atlas POSTs the change signal to.
	NotifyCallbackURL string `json:"notify_callback_url,omitempty"`
}

// RegisterResult is the response to Register.
type RegisterResult struct {
	ServerID string `json:"server_id"`
	Status   string `json:"status"`
	// CrossServerConfig is the coordination config version in force at
	// registration time (version 0 = nothing published yet). A server can
	// compare it against its cached copy and skip a pointless pull.
	CrossServerConfig *ConfigVersionRef `json:"crossserver_config,omitempty"`
}

// ConfigVersionRef is the version/hash pair carried by registration and
// by config change signals.
type ConfigVersionRef struct {
	Version int    `json:"version"`
	Hash    string `json:"hash"`
}

// HeartbeatRequest carries live load metadata (see TODO v0.1.20 for
// planned extensions such as initial player counts).
type HeartbeatRequest struct {
	Players int     `json:"players"`
	Load    float64 `json:"load"`
}

// HeartbeatResult is the response to Heartbeat.
type HeartbeatResult struct {
	ServerID        string `json:"server_id"`
	Status          string `json:"status"`
	NextHeartbeatIn int    `json:"next_heartbeat_in"`
}

// Character is a directory index entry.
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

// CreateCharacterRequest registers a new character on a server.
type CreateCharacterRequest struct {
	AccountID   int64  `json:"account_id"`
	ServerID    string `json:"server_id"`
	CharacterID int64  `json:"character_id"`
	Name        string `json:"name"`
	Level       int    `json:"level,omitempty"`
	ClassID     int    `json:"class_id,omitempty"`
	Avatar      string `json:"avatar,omitempty"`
}

// UpdateCharacterRequest patches a character; nil fields are left
// unchanged (PATCH semantics).
type UpdateCharacterRequest struct {
	Name    *string `json:"name,omitempty"`
	Level   *int    `json:"level,omitempty"`
	ClassID *int    `json:"class_id,omitempty"`
	Avatar  *string `json:"avatar,omitempty"`
}

// CharacterWriteResult is the response of a directory write. Character is
// nil when Status is "queued" (asynchronous event adapter).
type CharacterWriteResult struct {
	Character *Character `json:"character,omitempty"`
	Status    string     `json:"status"` // created | updated | deleted | queued
}

// UnmarshalJSON accepts both reply shapes Atlas produces: the nested
// {"character":...,"status":...} envelope (gRPC-style payloads) and the
// flat character object the synchronous REST path returns. Callers fill
// in Status for the flat shape ("created"/"updated" by endpoint).
func (r *CharacterWriteResult) UnmarshalJSON(data []byte) error {
	var probe struct {
		Character   *Character `json:"character"`
		CharacterID *int64     `json:"character_id"`
		Status      string     `json:"status"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	switch {
	case probe.Character != nil:
		r.Character, r.Status = probe.Character, probe.Status
	case probe.CharacterID != nil:
		var ch Character
		if err := json.Unmarshal(data, &ch); err != nil {
			return err
		}
		r.Character = &ch
	default:
		r.Status = probe.Status
	}
	return nil
}

// CharacterPage is a cursor-paginated character listing.
type CharacterPage struct {
	Characters []Character `json:"characters"`
	NextCursor string
}

// CharacterFilter narrows SearchCharacters (Admin API).
type CharacterFilter struct {
	Name     string
	ServerID string
	ClassID  *int
	MinLevel *int
	MaxLevel *int
	Limit    int
	Cursor   string
}

// Recommendation is the Routing decision for an account.
type Recommendation struct {
	Server Server `json:"server"`
	Reason string `json:"reason"` // lowest_load | highest_capacity | has_character | fallback
}

// Stats is the Admin fleet overview.
type Stats struct {
	TotalServers     int            `json:"total_servers"`
	ServersByStatus  map[string]int `json:"servers_by_status"`
	ServersByRegion  map[string]int `json:"servers_by_region"`
	ServersByVersion map[string]int `json:"servers_by_version"`
	TotalPlayers     int            `json:"total_players"`
	TotalCapacity    int            `json:"total_capacity"`
	TotalCharacters  int            `json:"total_characters"`
}

// Migration is a character migration job.
type Migration struct {
	ID            string     `json:"id"`
	SourceServers []string   `json:"source_servers"`
	TargetServer  string     `json:"target_server"`
	Status        string     `json:"status"`
	StartedAt     time.Time  `json:"started_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
}

// CreateMigrationRequest starts a migration from source servers to one target.
type CreateMigrationRequest struct {
	SourceServers []string `json:"source_servers"`
	TargetServer  string   `json:"target_server"`
}

// StatusResult is the common {server_id,status} / {status} reply.
type StatusResult struct {
	ServerID string `json:"server_id,omitempty"`
	Status   string `json:"status"`
}
