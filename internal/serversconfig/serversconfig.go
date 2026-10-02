// Package serversconfig loads config-declared servers from a JSON file
// (docs/server-config.md): a fleet that exists as soon as Atlas starts,
// without a register call. It also defines the ownership rules between the
// file and the register API — profile fields of declared servers are
// config-owned, status and runtime data are never touched here.
package serversconfig

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// Endpoint is the client-facing address of a declared server. It mirrors
// model.Endpoint so the config file and the register API use the same shape.
type Endpoint struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// Server is one declared server entry. Fields mirror the register API
// surface (registry.RegisterRequest) one-to-one: whatever a game server
// would pass to POST /v1/registry/servers/register can be declared here.
type Server struct {
	ID       string   `json:"server_id"`
	Name     string   `json:"name,omitempty"`
	Type     string   `json:"type,omitempty"`
	Region   string   `json:"region"`
	RealmID  *string  `json:"realm_id,omitempty"`
	ShardID  *string  `json:"shard_id,omitempty"`
	Version  string   `json:"version,omitempty"`
	Platform string   `json:"platform,omitempty"`
	Endpoint Endpoint `json:"endpoint"`
	Capacity int      `json:"capacity,omitempty"`
}

// Profile is a named server group inside a config file.
type Profile struct {
	Servers []Server `json:"servers"`
}

// File is the top-level config file shape. Either a flat fleet ("servers")
// or multiple named groups ("profiles") — not both.
type File struct {
	// Profile names the active profile inside Profiles. Optional when
	// ATLAS_SERVERS_PROFILE is set or a "default" profile exists.
	Profile  string             `json:"profile,omitempty"`
	Profiles map[string]Profile `json:"profiles,omitempty"`
	Servers  []Server           `json:"servers,omitempty"`
}

// ApplyResult reports what one Apply pass did.
type ApplyResult struct {
	// Created is the number of declared servers that did not exist yet.
	Created int
	// Updated is the number of declared servers whose profile fields were
	// refreshed (already existed, from config or from a register call).
	Updated int
	// Released is the number of previously config-managed servers that are
	// no longer declared and were handed back to API ownership. Their records
	// and statuses are preserved; only the source marker flips.
	Released int
}

// Load reads and validates a servers config file. profileOverride
// (ATLAS_SERVERS_PROFILE) wins over the file's own "profile" field; with
// neither set, a profile named "default" is used when present. Every failure
// is returned as an error — callers must abort startup, never half-apply.
func Load(path, profileOverride string) (*Profile, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read servers config: %w", err)
	}

	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields() // typos must fail at startup, not vanish
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, "", fmt.Errorf("parse servers config %s: %w", path, err)
	}

	switch {
	case len(f.Profiles) > 0 && len(f.Servers) > 0:
		return nil, "", fmt.Errorf("servers config %s: use either \"profiles\" or \"servers\", not both", path)
	case len(f.Profiles) > 0:
		name, prof, err := selectProfile(path, f, profileOverride)
		if err != nil {
			return nil, "", err
		}
		if err := validate(prof); err != nil {
			return nil, "", err
		}
		return prof, name, nil
	case len(f.Servers) > 0:
		if profileOverride != "" {
			return nil, "", fmt.Errorf("servers config %s declares no profiles; ATLAS_SERVERS_PROFILE=%q has nothing to select", path, profileOverride)
		}
		prof := &Profile{Servers: f.Servers}
		if err := validate(prof); err != nil {
			return nil, "", err
		}
		return prof, "", nil
	default:
		return nil, "", fmt.Errorf("servers config %s: no \"servers\" and no \"profiles\" declared", path)
	}
}

// selectProfile resolves the active profile: env override > file "profile"
// field > "default" entry > explicit error listing the available names.
func selectProfile(path string, f File, override string) (string, *Profile, error) {
	names := make([]string, 0, len(f.Profiles))
	for n := range f.Profiles {
		names = append(names, n)
	}
	sort.Strings(names)

	name := override
	if name == "" {
		name = f.Profile
	}
	if name == "" {
		if _, ok := f.Profiles["default"]; ok {
			name = "default"
		}
	}
	if name == "" {
		return "", nil, fmt.Errorf("servers config %s: profile not selected — set ATLAS_SERVERS_PROFILE to one of: %s", path, strings.Join(names, ", "))
	}
	prof, ok := f.Profiles[name]
	if !ok {
		return "", nil, fmt.Errorf("servers config %s: profile %q not found (available: %s)", path, name, strings.Join(names, ", "))
	}
	return name, &prof, nil
}

// validate checks every entry with the same rules as the register API
// (model.Server.Validate) plus duplicate-ID detection. The first failure is
// returned with its position so the operator can fix the file in one pass.
func validate(p *Profile) error {
	seen := make(map[string]int, len(p.Servers))
	for i, s := range p.Servers {
		if dup, ok := seen[s.ID]; ok {
			return fmt.Errorf("servers config: server %d (%s) duplicates server %d", i, s.ID, dup)
		}
		seen[s.ID] = i

		ms := s.toModel()
		if err := ms.Validate(); err != nil {
			return fmt.Errorf("servers config: server %d (%s): %w", i, s.ID, err)
		}
	}
	return nil
}

// toModel converts a declared entry into the stored representation, marked
// Source="config" and awaiting its first heartbeat (Status="starting").
func (s Server) toModel() *model.Server {
	var ep model.Endpoint
	ep.Host, ep.Port = s.Endpoint.Host, s.Endpoint.Port
	return &model.Server{
		ID:       s.ID,
		Name:     s.Name,
		Type:     s.Type,
		Region:   s.Region,
		RealmID:  s.RealmID,
		ShardID:  s.ShardID,
		Version:  s.Version,
		Platform: s.Platform,
		Endpoint: ep,
		Capacity: s.Capacity,
		Source:   "config",
		Status:   model.StatusStarting,
	}
}

// Apply upserts the declared fleet into the store and reconciles ownership:
//
//   - A declared server that does not exist is created (status "starting",
//     awaiting its first heartbeat).
//   - A declared server that exists has its profile fields refreshed; its
//     status, players/load and process start time are never touched, so an
//     Atlas restart cannot flap live servers.
//   - A server still marked Source="config" whose ID is no longer declared
//     is released back to API ownership — its record and status survive,
//     only the marker flips, so removing a declaration never deletes data.
//
// The declared set in the file is therefore the single source of truth for
// which servers are config-managed, applied atomically per startup.
func Apply(ctx context.Context, servers store.ServerStore, p *Profile) (ApplyResult, error) {
	var res ApplyResult
	declared := make(map[string]bool, len(p.Servers))

	for i := range p.Servers {
		srv := p.Servers[i].toModel()
		declared[srv.ID] = true

		existing, err := servers.GetServer(ctx, srv.ID)
		switch {
		case errors.Is(err, store.ErrNotFound):
			if err := servers.RegisterServer(ctx, srv); err != nil {
				return res, fmt.Errorf("apply servers config (create %s): %w", srv.ID, err)
			}
			res.Created++
		case err != nil:
			return res, fmt.Errorf("apply servers config (lookup %s): %w", srv.ID, err)
		default:
			// Refresh profile fields only: pass the existing status and
			// process start time straight through so the upsert cannot
			// disturb lifecycle state or uptime.
			srv.Status = existing.Status
			srv.StartedAt = existing.StartedAt
			if err := servers.RegisterServer(ctx, srv); err != nil {
				return res, fmt.Errorf("apply servers config (update %s): %w", srv.ID, err)
			}
			res.Updated++
		}
	}

	// Release config-owned servers that are no longer declared. ListServers
	// is cursor-paginated (ID-ascending, limit-capped), so walk pages until
	// exhausted.
	cursor := ""
	for {
		page, err := servers.ListServers(ctx, store.ServerFilter{Limit: 200, Cursor: cursor})
		if err != nil {
			return res, fmt.Errorf("apply servers config (reconcile): %w", err)
		}
		for _, srv := range page {
			if srv.Source != "config" || declared[srv.ID] {
				continue
			}
			srv.Source = ""
			if err := servers.RegisterServer(ctx, srv); err != nil {
				return res, fmt.Errorf("apply servers config (release %s): %w", srv.ID, err)
			}
			res.Released++
		}
		if len(page) < 200 {
			break
		}
		cursor = page[len(page)-1].ID
	}

	return res, nil
}
