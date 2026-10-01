// Package httpapi provides the HTTP API handlers for Atlas.
//
// Routes follow Go 1.22+ net/http pattern routing with {name} path parameters.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/event"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store"
)

// Handler holds all Atlas HTTP handlers and their dependencies.
type Handler struct {
	registry  *registry.Service
	discovery *discovery.Service
	directory *directory.Service
	admin     *admin.Service
	routing   *routing.Service
	store     store.Store
	events    event.EventAdapter
	logger    *slog.Logger
}

// New creates a new Handler.
func New(reg *registry.Service, disc *discovery.Service, dir *directory.Service, adm *admin.Service, rt *routing.Service, s store.Store, events event.EventAdapter, logger *slog.Logger) *Handler {
	return &Handler{
		registry:  reg,
		discovery: disc,
		directory: dir,
		admin:     adm,
		routing:   rt,
		store:     s,
		events:    events,
		logger:    logger,
	}
}

// RegisterRoutes registers all Atlas routes on a single mux (legacy, for
// development with a single port). For production, use the zone-specific
// methods below.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	h.RegisterRegistryRoutes(mux)
	h.RegisterPublicRoutes(mux)
	h.RegisterAdminRoutes(mux)
	mux.HandleFunc("GET /healthz", h.handleHealthz)
	mux.HandleFunc("GET /readyz", h.handleReadyz)
}

// RegisterRegistryRoutes registers server registration / heartbeat / unregister
// routes. Mount on the internal-network listener with RegistryAuth middleware.
func (h *Handler) RegisterRegistryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/registry/servers/register", h.handleRegister)
	mux.HandleFunc("POST /v1/registry/servers/{id}/heartbeat", h.handleHeartbeat)
	mux.HandleFunc("POST /v1/registry/servers/{id}/unregister", h.handleUnregister)
}

// RegisterPublicRoutes registers discovery and directory routes. Mount on the
// public-facing listener (behind a gateway that handles player auth).
func (h *Handler) RegisterPublicRoutes(mux *http.ServeMux) {
	// Discovery
	mux.HandleFunc("GET /v1/discovery/servers", h.handleListServers)
	mux.HandleFunc("GET /v1/discovery/servers/{id}", h.handleGetServer)

	// Routing
	mux.HandleFunc("GET /v1/routing/recommended", h.handleRecommended)

	// Directory
	mux.HandleFunc("POST /v1/directory/characters", h.handleCreateCharacter)
	mux.HandleFunc("GET /v1/directory/accounts/{id}/characters", h.handleListCharactersByAccount)
	mux.HandleFunc("GET /v1/directory/characters/{character_id}", h.handleGetCharacter)
	mux.HandleFunc("GET /v1/directory/servers/{id}/characters", h.handleListCharactersByServer)
	mux.HandleFunc("PATCH /v1/directory/characters/{character_id}", h.handlePatchCharacter)
	mux.HandleFunc("DELETE /v1/directory/characters/{character_id}", h.handleDeleteCharacter)
}

// RegisterAdminRoutes registers admin / lifecycle / stats / migration routes.
// Mount on the management-network listener with AdminAuth middleware.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/admin/servers/{id}/maintenance", h.handleAdminMaintenance)
	mux.HandleFunc("POST /v1/admin/servers/{id}/drain", h.handleAdminDrain)
	mux.HandleFunc("POST /v1/admin/servers/{id}/enable", h.handleAdminEnable)
	mux.HandleFunc("POST /v1/admin/servers/{id}/disable", h.handleAdminDisable)
	mux.HandleFunc("GET /v1/admin/stats", h.handleAdminStats)
	mux.HandleFunc("GET /v1/admin/characters/search", h.handleAdminSearchCharacters)
	mux.HandleFunc("POST /v1/admin/migrations", h.handleAdminCreateMigration)
	mux.HandleFunc("GET /v1/admin/migrations", h.handleAdminListMigrations)
	mux.HandleFunc("GET /v1/admin/migrations/{id}", h.handleAdminGetMigration)
	mux.HandleFunc("POST /v1/admin/migrations/{id}/rollback", h.handleAdminRollbackMigration)
}

// ---------------------------------------------------------------------------
// Registry handlers
// ---------------------------------------------------------------------------

func (h *Handler) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string         `json:"server_id"`
		Name     string         `json:"name"`
		Type     string         `json:"type"`
		Region   string         `json:"region"`
		RealmID  *string        `json:"realm_id,omitempty"`
		ShardID  *string        `json:"shard_id,omitempty"`
		Version  string         `json:"version"`
		Platform string         `json:"platform"`
		Endpoint model.Endpoint `json:"endpoint"`
		Capacity int            `json:"capacity"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON body")
		return
	}

	regReq := registry.RegisterRequest{
		ID:       req.ID,
		Name:     req.Name,
		Type:     req.Type,
		Region:   req.Region,
		RealmID:  req.RealmID,
		ShardID:  req.ShardID,
		Version:  req.Version,
		Platform: req.Platform,
		Endpoint: req.Endpoint,
		Capacity: req.Capacity,
	}

	srv, err := h.registry.Register(r.Context(), regReq)
	if err != nil {
		if errors.Is(err, model.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"server_id": srv.ID,
		"status":    srv.Status,
	})
}

func (h *Handler) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var hb model.Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON body")
		return
	}

	if err := h.registry.Heartbeat(r.Context(), id, hb); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "SERVER_NOT_FOUND", fmt.Sprintf("server %s not found", id))
			return
		}
		if errors.Is(err, model.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"server_id":          id,
		"status":             "online",
		"next_heartbeat_in":  10,
	})
}

func (h *Handler) handleUnregister(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	if err := h.registry.Unregister(r.Context(), id); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "SERVER_NOT_FOUND", fmt.Sprintf("server %s not found", id))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"server_id": id,
		"status":    "offline",
	})
}

// ---------------------------------------------------------------------------
// Discovery handlers
// ---------------------------------------------------------------------------

func (h *Handler) handleListServers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	f := store.ServerFilter{
		Region:   q.Get("region"),
		Realm:    q.Get("realm"),
		Shard:    q.Get("shard"),
		Version:  q.Get("version"),
		Platform: q.Get("platform"),
		Cursor:   q.Get("cursor"),
	}

	if s := q.Get("status"); s != "" {
		f.Status = model.ServerStatus(s)
		if !f.Status.Valid() {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", fmt.Sprintf("invalid status %q", s))
			return
		}
	}

	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid limit")
			return
		}
		f.Limit = n
	}

	servers, err := h.discovery.ListServers(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"servers": servers,
	})
}

func (h *Handler) handleGetServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	srv, err := h.discovery.GetServer(r.Context(), id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "SERVER_NOT_FOUND", fmt.Sprintf("server %s not found", id))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, srv)
}

// ---------------------------------------------------------------------------
// Directory handlers
// ---------------------------------------------------------------------------

func (h *Handler) handleCreateCharacter(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AccountID   int64  `json:"account_id"`
		ServerID    string `json:"server_id"`
		CharacterID int64  `json:"character_id"`
		Name        string `json:"name"`
		Level       int    `json:"level"`
		ClassID     int    `json:"class_id"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON body")
		return
	}

	// Validate before enqueueing: asynchronous adapters would otherwise
	// accept events that only fail at consumption time.
	if req.AccountID <= 0 || req.ServerID == "" || req.CharacterID <= 0 {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "account_id, server_id and character_id are required")
		return
	}

	level, classID := req.Level, req.ClassID
	evt := &event.Event{
		Type:        event.EventCharacterCreated,
		AccountID:   req.AccountID,
		ServerID:    req.ServerID,
		CharacterID: req.CharacterID,
		Name:        req.Name,
		Level:       &level,
		ClassID:     &classID,
		Timestamp:   time.Now(),
	}
	if err := h.events.Publish(r.Context(), evt); err != nil {
		h.writeEventError(w, err)
		return
	}

	if !h.events.Synchronous() {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
		return
	}

	ch, err := h.directory.GetCharacterByCharacterID(r.Context(), req.CharacterID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, ch)
}

// handleRecommended answers GET /v1/routing/recommended with the best
// server for the requested filters and a reason string.
func (h *Handler) handleRecommended(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req := routing.Request{
		Region:   q.Get("region"),
		Version:  q.Get("version"),
		Platform: q.Get("platform"),
	}
	if v := q.Get("account_id"); v != "" {
		accountID, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid account_id")
			return
		}
		req.AccountID = accountID
	}

	srv, reason, err := h.routing.Recommend(r.Context(), req)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "NO_SERVER_AVAILABLE", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"server": srv,
		"reason": reason,
	})
}

// writeEventError maps event application errors to API error responses.
func (h *Handler) writeEventError(w http.ResponseWriter, err error) {
	if errors.Is(err, model.ErrInvalid) {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "CHARACTER_NOT_FOUND", err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
}

func (h *Handler) handleListCharactersByAccount(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	accountID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid account_id")
		return
	}

	chars, err := h.directory.ListByAccount(r.Context(), accountID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"characters": chars,
	})
}

func (h *Handler) handleGetCharacter(w http.ResponseWriter, r *http.Request) {
	charIDStr := r.PathValue("character_id")
	charID, err := strconv.ParseInt(charIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid character_id")
		return
	}

	ch, err := h.directory.GetCharacterByCharacterID(r.Context(), charID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "CHARACTER_NOT_FOUND", fmt.Sprintf("character %d not found", charID))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, ch)
}

func (h *Handler) handleListCharactersByServer(w http.ResponseWriter, r *http.Request) {
	serverID := r.PathValue("id")
	q := r.URL.Query()

	limit := 50
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err == nil && n > 0 {
			limit = n
		}
	}
	cursor := q.Get("cursor")

	chars, nextCursor, err := h.directory.ListByServer(r.Context(), serverID, limit, cursor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	resp := map[string]any{
		"characters": chars,
	}
	if nextCursor != "" {
		resp["next_cursor"] = nextCursor
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handlePatchCharacter(w http.ResponseWriter, r *http.Request) {
	charIDStr := r.PathValue("character_id")
	charID, err := strconv.ParseInt(charIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid character_id")
		return
	}

	var req struct {
		AccountID int64  `json:"account_id"`
		ServerID  string `json:"server_id"`
		Name      *string `json:"name,omitempty"`
		Level     *int    `json:"level,omitempty"`
		ClassID   *int    `json:"class_id,omitempty"`
		Avatar    *string `json:"avatar,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON body")
		return
	}

	evt := &event.Event{
		Type:        event.EventCharacterUpdated,
		AccountID:   req.AccountID,
		ServerID:    req.ServerID,
		CharacterID: charID,
		Name:        deref(req.Name),
		Level:       req.Level,
		ClassID:     req.ClassID,
		Avatar:      req.Avatar,
	}
	if err := h.events.Publish(r.Context(), evt); err != nil {
		h.writeEventError(w, err)
		return
	}

	if !h.events.Synchronous() {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
		return
	}

	ch, err := h.directory.GetCharacterByCharacterID(r.Context(), charID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ch)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func (h *Handler) handleDeleteCharacter(w http.ResponseWriter, r *http.Request) {
	charIDStr := r.PathValue("character_id")
	charID, err := strconv.ParseInt(charIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid character_id")
		return
	}

	// We need account_id and server_id to delete. Try to look up the character
	// first so async adapters don't enqueue deletes for missing characters.
	ch, err := h.directory.GetCharacterByCharacterID(r.Context(), charID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "CHARACTER_NOT_FOUND", fmt.Sprintf("character %d not found", charID))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	evt := &event.Event{
		Type:        event.EventCharacterDeleted,
		AccountID:   ch.AccountID,
		ServerID:    ch.ServerID,
		CharacterID: charID,
	}
	if err := h.events.Publish(r.Context(), evt); err != nil {
		h.writeEventError(w, err)
		return
	}

	if !h.events.Synchronous() {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---------------------------------------------------------------------------
// Admin handlers
// ---------------------------------------------------------------------------

func (h *Handler) handleAdminMaintenance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.admin.SetMaintenance(r.Context(), id); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "SERVER_NOT_FOUND", fmt.Sprintf("server %s not found", id))
			return
		}
		if errors.Is(err, model.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server_id": id,
		"status":    "maintenance",
	})
}

func (h *Handler) handleAdminDrain(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.admin.SetDrain(r.Context(), id); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "SERVER_NOT_FOUND", fmt.Sprintf("server %s not found", id))
			return
		}
		if errors.Is(err, model.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server_id": id,
		"status":    "draining",
	})
}

func (h *Handler) handleAdminEnable(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.admin.Enable(r.Context(), id); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "SERVER_NOT_FOUND", fmt.Sprintf("server %s not found", id))
			return
		}
		if errors.Is(err, model.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server_id": id,
		"status":    "online",
	})
}

func (h *Handler) handleAdminDisable(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.admin.Disable(r.Context(), id); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "SERVER_NOT_FOUND", fmt.Sprintf("server %s not found", id))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server_id": id,
		"status":    "disabled",
	})
}

func (h *Handler) handleAdminStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.admin.GetStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (h *Handler) handleAdminSearchCharacters(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := store.CharacterSearchFilter{
		Name:     q.Get("q"),
		ServerID: q.Get("server_id"),
		Cursor:   q.Get("cursor"),
	}

	if classStr := q.Get("class_id"); classStr != "" {
		n, err := strconv.Atoi(classStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid class_id")
			return
		}
		filter.ClassID = &n
	}
	if minStr := q.Get("min_level"); minStr != "" {
		n, err := strconv.Atoi(minStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid min_level")
			return
		}
		filter.MinLevel = &n
	}
	if maxStr := q.Get("max_level"); maxStr != "" {
		n, err := strconv.Atoi(maxStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid max_level")
			return
		}
		filter.MaxLevel = &n
	}
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid limit")
			return
		}
		filter.Limit = n
	}

	chars, nextCursor, err := h.admin.SearchCharacters(r.Context(), filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	resp := map[string]any{
		"characters": chars,
	}
	if nextCursor != "" {
		resp["next_cursor"] = nextCursor
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleAdminCreateMigration(w http.ResponseWriter, r *http.Request) {
	var req admin.CreateMigrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON body")
		return
	}

	m, err := h.admin.CreateMigration(r.Context(), req)
	if err != nil {
		if errors.Is(err, model.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "SERVER_NOT_FOUND", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, m)
}

func (h *Handler) handleAdminListMigrations(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err == nil && n > 0 {
			limit = n
		}
	}

	migrations, err := h.admin.ListMigrations(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"migrations": migrations,
	})
}

func (h *Handler) handleAdminGetMigration(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	m, err := h.admin.GetMigration(r.Context(), id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "MIGRATION_NOT_FOUND", fmt.Sprintf("migration %s not found", id))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (h *Handler) handleAdminRollbackMigration(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.admin.RollbackMigration(r.Context(), id); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "MIGRATION_NOT_FOUND", fmt.Sprintf("migration %s not found", id))
			return
		}
		if errors.Is(err, model.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":     id,
		"status": "rolled_back",
	})
}

// ---------------------------------------------------------------------------
// Health handlers
// ---------------------------------------------------------------------------

func (h *Handler) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if err := h.store.Ping(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "STORAGE_UNAVAILABLE", "store is not reachable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ---------------------------------------------------------------------------
// JSON helpers
// ---------------------------------------------------------------------------

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(errorBody{
		Error: errorDetail{Code: code, Message: message},
	})
}

// ensure time import is used
var _ = time.Now

// CORSMiddleware adds CORS headers for cross-origin requests from the dashboard.
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}