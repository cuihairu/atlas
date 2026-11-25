package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"
)

// The cross-server coordination config is a five-part configuration surface
// (managed by the config center): topology (clusters), participation groups,
// feature switches, match domains, and the crossplay type table. All are
// declarative structures, persisted as canonical JSON and hashed.
type (
	// CrossServerCluster is a cluster in the cross-server topology (a set of
	// servers that jointly take part in cross-server gameplay). Servers is a
	// list of server_ids; a cluster may be created before its members are added.
	CrossServerCluster struct {
		ID      string   `json:"id"`
		Name    string   `json:"name,omitempty"`
		Region  string   `json:"region,omitempty"`
		Status  string   `json:"status,omitempty"` // active (default) / disabled
		Servers []string `json:"servers"`
	}

	// CrossServerGroup is a participation group: a batch of servers scoped by
	// operations criteria (e.g. "cross-server battlefield season 1, East
	// China"), referenced by feature switches and match domains.
	CrossServerGroup struct {
		ID      string   `json:"id"`
		Name    string   `json:"name,omitempty"`
		Servers []string `json:"servers"`
	}

	// CrossServerMatchDomain is a match domain: the set of servers in one
	// matchmaking pool. Params are game-defined parameters (string values, e.g.
	// mmr_range / max_team).
	CrossServerMatchDomain struct {
		ID      string            `json:"id"`
		Name    string            `json:"name,omitempty"`
		Servers []string          `json:"servers"`
		Params  map[string]string `json:"params,omitempty"`
	}

	// CrossPlayType is one row of the crossplay type table (crossplay_types):
	// declarative metadata for a class of cross-server gameplay
	// (docs/config-center.md §2.2). The type table is a fleet-level
	// declaration — any change is a global change (diff targets=["*"]). ID is
	// the code identifier (the naming anchor of the terminology contract);
	// IDPrefix is the type prefix for runtime IDs of this type (§2.1
	// cross-server ID system, unique across the table), so logs/leaderboards
	// can grep back by prefix.
	CrossPlayType struct {
		ID          string `json:"id"`                    // code identifier, e.g. "battlefield" ([a-z0-9._-])
		Name        string `json:"name,omitempty"`        // display name (Chinese in production), e.g. "Cross-Server Battlefield"
		Summary     string `json:"summary,omitempty"`     // one-line definition
		Lifecycle   string `json:"lifecycle,omitempty"`   // persistent (default) / seasonal / ephemeral
		Matchmaking bool   `json:"matchmaking,omitempty"` // participates in matchmaking via match_domains pools
		Ranking     bool   `json:"ranking,omitempty"`     // requires leaderboard data aggregation
		IDPrefix    string `json:"id_prefix,omitempty"`   // runtime ID type prefix (2-8 lowercase letters, unique across the table)
	}

	// CrossServerSpec is the full cross-server configuration document.
	CrossServerSpec struct {
		Topology       CrossServerTopology      `json:"topology"`
		Groups         []CrossServerGroup       `json:"groups"`
		Features       map[string]bool          `json:"features"`
		MatchDomains   []CrossServerMatchDomain `json:"match_domains"`
		CrossPlayTypes []CrossPlayType          `json:"crossplay_types"`
	}

	// CrossServerTopology is the cross-server topology (a list of clusters).
	CrossServerTopology struct {
		Clusters []CrossServerCluster `json:"clusters"`
	}

	// CrossServerConfig is a versioned snapshot of the cross-server config.
	// Version is incremented atomically by the store on every save
	// (monotonically increasing); Hash is the canonical digest of Spec.
	CrossServerConfig struct {
		Version   int             `json:"version"`
		Hash      string          `json:"hash"`
		Spec      CrossServerSpec `json:"spec"`
		UpdatedAt time.Time       `json:"updated_at"`
	}
)

// Valid values of CrossPlayType.Lifecycle (default persistent): always-on
// gameplay / seasonal (leaderboards and match pools reset each season) /
// limited-time event window.
const (
	CrossPlayLifecyclePersistent = "persistent"
	CrossPlayLifecycleSeasonal   = "seasonal"
	CrossPlayLifecycleEphemeral  = "ephemeral"
)

// The code identifiers of the seven standard cross-server gameplay types
// (terminology contract anchors, docs/config-center.md §2.2). The type table
// is published by operations rather than hard-coded, so these constants
// guarantee that the docs, the admin console and integrators all refer to the
// same word for "the type name".
const (
	CrossPlayBattlefield = "battlefield" // Cross-Server Battlefield (xb)
	CrossPlayDungeon     = "dungeon"     // Cross-Server Dungeon / boss (xd)
	CrossPlayRanking     = "ranking"     // Cross-Server Ranking (xr)
	CrossPlayGuildWar    = "guildwar"    // Cross-Server Guild War / territory war (xg)
	CrossPlayTrade       = "trade"       // Cross-Server Trade / auction (xt)
	CrossPlayChat        = "chat"        // Cross-Server Chat / social (xc)
	CrossPlayTeam        = "team"        // Cross-Server Team Up / recruiting (xp)
)

// TargetAll is the wildcard in a change target: every server should re-pull
// (a global change, e.g. a wholesale edit to the feature switch table).
const TargetAll = "*"

// ETag returns the HTTP strong validator for this config (the hash in quotes).
func (c *CrossServerConfig) ETag() string { return `"` + c.Hash + `"` }

// NormalizeCrossServerSpec replaces nil slices / maps with empty containers.
// In JSON, nil and an empty array serialize differently, so without
// normalization "the same config" could produce two different hashes, and an
// idempotent save (no version bump when content is unchanged) would fail.
func NormalizeCrossServerSpec(spec CrossServerSpec) CrossServerSpec {
	if spec.Topology.Clusters == nil {
		spec.Topology.Clusters = []CrossServerCluster{}
	}
	if spec.Groups == nil {
		spec.Groups = []CrossServerGroup{}
	}
	if spec.Features == nil {
		spec.Features = map[string]bool{}
	}
	if spec.MatchDomains == nil {
		spec.MatchDomains = []CrossServerMatchDomain{}
	}
	if spec.CrossPlayTypes == nil {
		spec.CrossPlayTypes = []CrossPlayType{}
	}
	for i := range spec.MatchDomains {
		if spec.MatchDomains[i].Servers == nil {
			spec.MatchDomains[i].Servers = []string{}
		}
		if len(spec.MatchDomains[i].Params) == 0 {
			spec.MatchDomains[i].Params = nil
		}
	}
	return spec
}

// CloneCrossServerSpec returns a deep copy of spec. Config documents are
// stored and read by value and diffed between the previous and next versions:
// if the store shared the underlying arrays with the caller, the caller's
// later in-place mutation would rewrite the stored history too (the SQL store
// goes through JSON serialization and has no aliasing by nature; the memory
// store must clone explicitly — all three stores honor the same contract).
func CloneCrossServerSpec(spec CrossServerSpec) CrossServerSpec {
	out := spec
	out.Topology.Clusters = make([]CrossServerCluster, len(spec.Topology.Clusters))
	for i, c := range spec.Topology.Clusters {
		c.Servers = slices.Clone(c.Servers)
		out.Topology.Clusters[i] = c
	}
	out.Groups = make([]CrossServerGroup, len(spec.Groups))
	for i, g := range spec.Groups {
		g.Servers = slices.Clone(g.Servers)
		out.Groups[i] = g
	}
	out.Features = maps.Clone(spec.Features)
	out.MatchDomains = make([]CrossServerMatchDomain, len(spec.MatchDomains))
	for i, d := range spec.MatchDomains {
		d.Servers = slices.Clone(d.Servers)
		d.Params = maps.Clone(d.Params)
		out.MatchDomains[i] = d
	}
	// Type table rows are pure value types (string/bool), so cloning the
	// slice makes them fully independent.
	out.CrossPlayTypes = slices.Clone(spec.CrossPlayTypes)
	return out
}

// EmptyCrossServerConfig is the version=0 empty snapshot: the config center
// has never been written to. When a game server's startup pull gets it, that
// means "no cross-server coordination config yet" and it may start normally
// (no service interruption), rather than treating it as a pull failure.
func EmptyCrossServerConfig() *CrossServerConfig {
	return &CrossServerConfig{
		Version: 0,
		Hash:    HashCrossServerSpec(NormalizeCrossServerSpec(CrossServerSpec{})),
		Spec:    NormalizeCrossServerSpec(CrossServerSpec{}),
	}
}

// HashCrossServerSpec computes a deterministic digest of the spec:
// encoding/json sorts map keys and struct field order is fixed, so the
// serialized bytes of a given spec are stable. The first 16 hex characters of
// the sha256 (64 bits) serve as the version fingerprint.
func HashCrossServerSpec(spec CrossServerSpec) string {
	spec = NormalizeCrossServerSpec(spec)
	payload, err := json.Marshal(spec)
	if err != nil {
		// Spec contains only string/bool/slice/map, so Marshal cannot fail;
		// this defensive fallback keeps the digest stable for unchanged content.
		payload = []byte(fmt.Sprintf("%v", spec))
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])[:16]
}

// ValidateCrossServerSpec performs structural validation: unique IDs and
// valid feature-switch keys. It does not check that servers exist — a
// referenced server may be registered later (topology-first planning phase).
func ValidateCrossServerSpec(spec CrossServerSpec) error {
	clusterIDs := make(map[string]bool, len(spec.Topology.Clusters))
	for i, c := range spec.Topology.Clusters {
		if c.ID == "" {
			return fmt.Errorf("topology.clusters[%d].id: required", i)
		}
		if clusterIDs[c.ID] {
			return fmt.Errorf("topology.clusters[%d].id %q: duplicate", i, c.ID)
		}
		clusterIDs[c.ID] = true
		if c.Status != "" && c.Status != "active" && c.Status != "disabled" {
			return fmt.Errorf("topology.clusters[%d].status %q: must be active or disabled", i, c.Status)
		}
		if err := validateServerIDs(fmt.Sprintf("topology.clusters[%d].servers", i), c.Servers); err != nil {
			return err
		}
	}
	groupIDs := make(map[string]bool, len(spec.Groups))
	for i, g := range spec.Groups {
		if g.ID == "" {
			return fmt.Errorf("groups[%d].id: required", i)
		}
		if groupIDs[g.ID] {
			return fmt.Errorf("groups[%d].id %q: duplicate", i, g.ID)
		}
		groupIDs[g.ID] = true
		if err := validateServerIDs(fmt.Sprintf("groups[%d].servers", i), g.Servers); err != nil {
			return err
		}
	}

	// Topology wins (the adjudication rule for how the three relate:
	// docs/config-center.md §2.3). Clusters are the technical boundary for
	// server interconnection: a server
	// belongs to at most one cluster (overlap is a topology error). A
	// participation group is a soft ops-orchestration set and must sit wholly
	// inside ONE cluster — including a server from another topology or outside
	// any topology is rejected outright, and the error explains what to fix
	// (declare the cluster membership first, or split the group).
	serverCluster := make(map[string]string)
	for _, c := range spec.Topology.Clusters {
		for _, srv := range c.Servers {
			if prev, ok := serverCluster[srv]; ok {
				return fmt.Errorf("topology: server %q belongs to both cluster %q and %q — clusters are disjoint interconnect boundaries", srv, prev, c.ID)
			}
			serverCluster[srv] = c.ID
		}
	}
	for i, g := range spec.Groups {
		if len(g.Servers) == 0 {
			continue
		}
		cluster := ""
		var outside, spanning []string
		for _, srv := range g.Servers {
			c, ok := serverCluster[srv]
			if !ok {
				outside = append(outside, srv)
				continue
			}
			if cluster == "" {
				cluster = c
			} else if c != cluster {
				spanning = append(spanning, srv)
			}
		}
		switch {
		case len(outside) > 0:
			return fmt.Errorf("groups[%d] %q: servers %v are not in any topology cluster — declare the cluster membership first (拓扑为准，分组受拓扑约束)", i, g.ID, outside)
		case len(spanning) > 0:
			return fmt.Errorf("groups[%d] %q: servers %v span clusters %q + others — a group must sit inside ONE cluster (拓扑为准，分组受拓扑约束；跨拓扑请拆成多个分组)", i, g.ID, spanning, cluster)
		}
	}
	for key := range spec.Features {
		if err := validateConfigKey(key); err != nil {
			return fmt.Errorf("features.%s: %w", key, err)
		}
	}
	domainIDs := make(map[string]bool, len(spec.MatchDomains))
	for i, d := range spec.MatchDomains {
		if d.ID == "" {
			return fmt.Errorf("match_domains[%d].id: required", i)
		}
		if domainIDs[d.ID] {
			return fmt.Errorf("match_domains[%d].id %q: duplicate", i, d.ID)
		}
		domainIDs[d.ID] = true
		if err := validateServerIDs(fmt.Sprintf("match_domains[%d].servers", i), d.Servers); err != nil {
			return err
		}
	}
	typeIDs := make(map[string]bool, len(spec.CrossPlayTypes))
	prefixes := make(map[string]bool, len(spec.CrossPlayTypes))
	for i, tp := range spec.CrossPlayTypes {
		if tp.ID == "" {
			return fmt.Errorf("crossplay_types[%d].id: required", i)
		}
		if err := validateConfigKey(tp.ID); err != nil {
			return fmt.Errorf("crossplay_types[%d].id: %w", i, err)
		}
		if typeIDs[tp.ID] {
			return fmt.Errorf("crossplay_types[%d].id %q: duplicate", i, tp.ID)
		}
		typeIDs[tp.ID] = true
		switch tp.Lifecycle {
		case "", CrossPlayLifecyclePersistent, CrossPlayLifecycleSeasonal, CrossPlayLifecycleEphemeral:
		default:
			return fmt.Errorf("crossplay_types[%d].lifecycle %q: must be persistent, seasonal or ephemeral", i, tp.Lifecycle)
		}
		if tp.IDPrefix == "" {
			continue
		}
		if err := validateIDPrefix(tp.IDPrefix); err != nil {
			return fmt.Errorf("crossplay_types[%d].id_prefix: %w", i, err)
		}
		// Prefixes must be unique so a runtime ID greps back to exactly
		// one type (ambiguous prefixes break log/bboard traceability).
		if prefixes[tp.IDPrefix] {
			return fmt.Errorf("crossplay_types[%d].id_prefix %q: duplicate", i, tp.IDPrefix)
		}
		prefixes[tp.IDPrefix] = true
	}
	return nil
}

// validateIDPrefix checks a runtime-ID type prefix: 2-8 lowercase ASCII
// letters (docs/config-center.md §2.1), e.g. "xb" / "xd". Letters-only
// keeps prefixes unambiguous against the "-" segment separator.
func validateIDPrefix(p string) error {
	if len(p) < 2 || len(p) > 8 {
		return fmt.Errorf("%q: must be 2-8 lowercase letters", p)
	}
	for i := 0; i < len(p); i++ {
		if c := p[i]; c < 'a' || c > 'z' {
			return fmt.Errorf("%q: must be 2-8 lowercase letters", p)
		}
	}
	return nil
}

func validateServerIDs(field string, ids []string) error {
	seen := make(map[string]bool, len(ids))
	for i, id := range ids {
		if id == "" {
			return fmt.Errorf("%s[%d]: empty server id", field, i)
		}
		if seen[id] {
			return fmt.Errorf("%s[%d] %q: duplicate server id", field, i, id)
		}
		seen[id] = true
	}
	return nil
}

func validateConfigKey(key string) error {
	if key == "" {
		return fmt.Errorf("key required")
	}
	if len(key) > 64 {
		return fmt.Errorf("key too long (max 64)")
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case (c == '_' || c == '-' || c == '.') && i > 0:
		default:
			return fmt.Errorf("key %q: only [a-z0-9._-] allowed", key)
		}
	}
	if c := key[0]; (c < 'a' || c > 'z') && (c < '0' || c > '9') {
		return fmt.Errorf("key %q: must start with [a-z0-9]", key)
	}
	return nil
}
