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
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/crossserver"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/event"
	"github.com/cuihairu/atlas/internal/fleet"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/telemetry"
)

// Handler holds all Atlas HTTP handlers and their dependencies.
type Handler struct {
	registry    *registry.Service
	discovery   *discovery.Service
	directory   *directory.Service
	admin       *admin.Service
	routing     *routing.Service
	crossserver *crossserver.Service
	store       store.Store
	events      event.EventAdapter
	logger      *slog.Logger
	audit       *AuditLog
	fleet       *fleet.Index
	limiter     *RateLimiter
	telemetry   *telemetry.Sampler
	bus         *event.CountingAdapter
}

// New creates a new Handler.
func New(reg *registry.Service, disc *discovery.Service, dir *directory.Service, adm *admin.Service, rt *routing.Service, cross *crossserver.Service, s store.Store, events event.EventAdapter, logger *slog.Logger) *Handler {
	return &Handler{
		registry:    reg,
		discovery:   disc,
		directory:   dir,
		admin:       adm,
		routing:     rt,
		crossserver: cross,
		store:       s,
		events:      events,
		logger:      logger,
	}
}

// WithAudit attaches the Admin API audit log (TODO v0.1.17). When set,
// RegisterAdminRoutes exposes GET /v1/admin/audit over the recent-entries
// ring; wrap the admin mux in AuditLog.Middleware to record operations.
func (h *Handler) WithAudit(a *AuditLog) *Handler {
	h.audit = a
	return h
}

// WithFleetIndex attaches the in-memory fleet aggregate (BUGS ③). When set,
// admin stats and the admin server list read the same snapshot the index
// maintains on every register / heartbeat / unregister — the decree that
// keeps overview counts, filter-bar facets, and list numbers identical.
func (h *Handler) WithFleetIndex(idx *fleet.Index) *Handler {
	h.fleet = idx
	return h
}

// WithRateLimiter attaches the listener rate limiter for the read-only
// 网关/系统配置 page (TODO 系统配置): parsed effective rules plus 429 hit
// counts. Configuration itself stays env-driven — the page never mutates.
func (h *Handler) WithRateLimiter(rl *RateLimiter) *Handler {
	h.limiter = rl
	return h
}

// WithTelemetry attaches the lightweight series sampler for the 概览负载
// 时间视图 and 消息总线 panels. Without it the endpoints report empty
// series (older embedders keep the same response shape).
func (h *Handler) WithTelemetry(s *telemetry.Sampler) *Handler {
	h.telemetry = s
	return h
}

// WithBusStats attaches the counting event adapter backing the 消息总线
// panel's current counters.
func (h *Handler) WithBusStats(c *event.CountingAdapter) *Handler {
	h.bus = c
	return h
}

// RegisterRoutes registers all Atlas routes on a single mux (legacy, for
// development with a single port). For production, use the zone-specific
// methods below. The shared cross-server pull is mounted exactly once —
// ServeMux rejects duplicate patterns.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	h.registerRegistryRoutes(mux)
	h.RegisterPublicRoutes(mux)
	h.RegisterAdminRoutes(mux)
	mux.HandleFunc("GET /healthz", h.handleHealthz)
	mux.HandleFunc("GET /readyz", h.handleReadyz)
}

// RegisterRegistryRoutes registers server registration / heartbeat /
// unregister routes plus the cross-server config pull. Mount on the
// internal-network listener with RegistryAuth middleware.
func (h *Handler) RegisterRegistryRoutes(mux *http.ServeMux) {
	h.registerRegistryRoutes(mux)
	h.registerCrossServerPull(mux)
}

// registerRegistryRoutes mounts the registry verbs only; each exported
// wrapper adds the shared cross-server pull itself so the legacy
// all-in-one RegisterRoutes ends up mounting it exactly once.
func (h *Handler) registerRegistryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/registry/servers/register", h.handleRegister)
	mux.HandleFunc("POST /v1/registry/servers/{id}/heartbeat", h.handleHeartbeat)
	mux.HandleFunc("POST /v1/registry/servers/{id}/unregister", h.handleUnregister)
}

// registerCrossServerPull mounts GET /v1/crossserver/config — the endpoint
// game servers call at startup and after every change signal (config
// center). ETag/If-None-Match make polling cheap for poll-mode servers.
func (h *Handler) registerCrossServerPull(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/crossserver/config", h.handleGetCrossServerConfig)
}

// RegisterPublicRoutes registers discovery and directory routes. Mount on the
// public-facing listener (behind a gateway that handles player auth).
func (h *Handler) RegisterPublicRoutes(mux *http.ServeMux) {
	// Discovery
	mux.HandleFunc("GET /v1/discovery/servers", h.handleListServers)
	mux.HandleFunc("GET /v1/discovery/servers/{id}", h.handleGetServer)
	mux.HandleFunc("GET /v1/discovery/announcements", h.handleListAnnouncements)

	// Routing
	mux.HandleFunc("GET /v1/routing/recommended", h.handleRecommended)

	// The strict config pull also serves the public listener: game servers
	// with no registry gateway pull it here (docs/config-center.md §4.2/§5
	// — 公网 :8080 / 注册 :8081). Unpublished is 404 CONFIG_NOT_FOUND,
	// never an empty document.
	h.registerCrossServerPull(mux)

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
	mux.HandleFunc("GET /v1/admin/servers", h.handleAdminListServers)
	mux.HandleFunc("GET /v1/admin/characters/search", h.handleAdminSearchCharacters)
	// 玩家视角排查 (TODO 排查/诊断): the routing decision walked through the
	// same Recommend pipeline with intermediates exposed, plus the account's
	// characters / directory entries.
	mux.HandleFunc("GET /v1/admin/diagnose/routing", h.handleAdminDiagnoseRouting)
	// 网关/系统配置只读页: parsed rate-limit rules + 429 hit counts.
	mux.HandleFunc("GET /v1/admin/rate-limits", h.handleAdminRateLimits)
	// 负载时间视图 (概览) 与 消息总线 panels: lightweight ring-buffer series.
	mux.HandleFunc("GET /v1/admin/load-series", h.handleAdminLoadSeries)
	mux.HandleFunc("GET /v1/admin/bus-series", h.handleAdminBusSeries)
	mux.HandleFunc("POST /v1/admin/migrations", h.handleAdminCreateMigration)
	mux.HandleFunc("GET /v1/admin/migrations", h.handleAdminListMigrations)
	mux.HandleFunc("GET /v1/admin/migrations/{id}", h.handleAdminGetMigration)
	mux.HandleFunc("POST /v1/admin/migrations/{id}/rollback", h.handleAdminRollbackMigration)
	mux.HandleFunc("POST /v1/admin/realms", h.handleAdminCreateRealm)
	mux.HandleFunc("GET /v1/admin/realms", h.handleAdminListRealms)
	mux.HandleFunc("POST /v1/admin/shards", h.handleAdminCreateShard)
	mux.HandleFunc("GET /v1/admin/shards", h.handleAdminListShards)
	// Server tags (标记体系): GET shows the full list including internal
	// tags; POST upserts one tag by code; DELETE removes one by code.
	mux.HandleFunc("GET /v1/admin/servers/{id}/tags", h.handleAdminGetServerTags)
	mux.HandleFunc("POST /v1/admin/servers/{id}/tags", h.handleAdminAddServerTag)
	mux.HandleFunc("DELETE /v1/admin/servers/{id}/tags/{code}", h.handleAdminRemoveServerTag)
	// Maintenance windows & announcements (TODO v0.1.20).
	mux.HandleFunc("POST /v1/admin/servers/{id}/maintenance-window", h.handleAdminCreateMaintenanceWindow)
	mux.HandleFunc("GET /v1/admin/maintenance-windows", h.handleAdminListMaintenanceWindows)
	mux.HandleFunc("DELETE /v1/admin/maintenance-windows/{id}", h.handleAdminDeleteMaintenanceWindow)
	mux.HandleFunc("POST /v1/admin/announcements", h.handleAdminCreateAnnouncement)
	mux.HandleFunc("GET /v1/admin/announcements", h.handleAdminListAnnouncements)
	mux.HandleFunc("DELETE /v1/admin/announcements/{id}", h.handleAdminDeleteAnnouncement)
	// Cross-server config (config center): admin-facing read/write of the
	// single versioned config document.
	mux.HandleFunc("GET /v1/admin/crossserver/config", h.handleAdminGetCrossServerConfig)
	mux.HandleFunc("PUT /v1/admin/crossserver/config", h.handleAdminUpdateCrossServerConfig)
	// Instruction queue status (TODO v0.2 ④ dash 队列可观测): memory store
	// exposes queue pressure / watermark / flush stats; SQL stores return
	// 503 INDEX_QUEUE_DISABLED.
	mux.HandleFunc("GET /v1/admin/indexqueue/status", h.handleAdminIndexQueueStatus)
	if h.audit != nil {
		mux.HandleFunc("GET /v1/admin/audit", h.handleAdminAudit)
	}
}

// handleAdminAudit serves the recent audit ring (TODO v0.1.17).
func (h *Handler) handleAdminAudit(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"entries": h.audit.Recent(limit),
	})
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
		// Config-center notify declaration (subscribe | callback | poll);
		// callback mode also carries the URL atlas signals.
		NotifyMode        string            `json:"notify_mode,omitempty"`
		NotifyCallbackURL string            `json:"notify_callback_url,omitempty"`
		Metadata          map[string]string `json:"metadata,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON body")
		return
	}

	regReq := registry.RegisterRequest{
		ID:                req.ID,
		Name:              req.Name,
		Type:              req.Type,
		Region:            req.Region,
		RealmID:           req.RealmID,
		ShardID:           req.ShardID,
		Version:           req.Version,
		Platform:          req.Platform,
		Endpoint:          req.Endpoint,
		Capacity:          req.Capacity,
		NotifyMode:        req.NotifyMode,
		NotifyCallbackURL: req.NotifyCallbackURL,
		Metadata:          req.Metadata,
	}

	srv, err := h.registry.Register(r.Context(), regReq)
	if err != nil {
		if errors.Is(err, model.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		if errors.Is(err, model.ErrConflict) {
			writeError(w, http.StatusConflict, "SERVER_MANAGED_BY_CONFIG", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	// Startup pull hookup: the registration response carries the current
	// cross-server config version so a booting server knows what to fetch
	// (and can compare against a cached copy). nil = no config saved yet.
	resp := map[string]any{
		"server_id": srv.ID,
		"status":    srv.Status,
	}
	if h.crossserver != nil {
		if cfg, err := h.crossserver.Snapshot(r.Context()); err == nil {
			// version 0 = nothing published yet; the server starts with the
			// empty snapshot and converges on the first change signal.
			resp["crossserver_config"] = map[string]any{
				"version": cfg.Version,
				"hash":    cfg.Hash,
			}
		}
	}

	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	var hb model.Heartbeat
	if err := json.NewDecoder(r.Body).Decode(&hb); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON body")
		return
	}

	status, err := h.registry.Heartbeat(r.Context(), id, hb)
	if err != nil {
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
		"server_id":         id,
		"status":            status,
		"next_heartbeat_in": 10,
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
		AccountID   int64             `json:"account_id"`
		ServerID    string            `json:"server_id"`
		CharacterID int64             `json:"character_id"`
		Name        string            `json:"name"`
		Level       int               `json:"level"`
		ClassID     int               `json:"class_id"`
		Metadata    map[string]string `json:"metadata,omitempty"`
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

	// Registration gating (server tags): 禁止注册 always rejects; 维护中
	// rejects or warns per ATLAS_MAINTENANCE_ENFORCE. Checked before
	// enqueueing so async adapters never accept a gated request.
	verdict, err := h.registry.CheckRegistration(r.Context(), req.ServerID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	if verdict.Code != "" {
		writeError(w, http.StatusForbidden, verdict.Code, verdict.Message)
		return
	}
	if verdict.Warn {
		// Warn mode: the request proceeds; the standard Warning header (and
		// the log line) carry the notice.
		w.Header().Set("Warning", `299 atlas "server under maintenance (维护中)"`)
		h.logger.Warn("character created on server in maintenance (warn mode)",
			"server_id", req.ServerID, "account_id", req.AccountID)
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
		Metadata:    &req.Metadata,
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

	// The recommendation is player-facing: keep only public tags (internal
	// markers never leave the admin surface).
	srv.Tags = model.PublicTags(srv.Tags)
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
		AccountID int64             `json:"account_id"`
		ServerID  string            `json:"server_id"`
		Name      *string           `json:"name,omitempty"`
		Level     *int              `json:"level,omitempty"`
		ClassID   *int              `json:"class_id,omitempty"`
		Avatar    *string           `json:"avatar,omitempty"`
		Metadata  map[string]string `json:"metadata,omitempty"`
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
	if req.Metadata != nil {
		evt.Metadata = &req.Metadata
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
	// BUGS ③: with the fleet index wired, the aggregate facets come from the
	// single in-memory snapshot every admin view reads. The store remains
	// the source for character totals — the index does not track characters.
	if h.fleet != nil {
		stats := h.fleet.Stats()
		if st, err := h.admin.GetStats(r.Context()); err == nil {
			stats.TotalCharacters = st.TotalCharacters
		}
		writeJSON(w, http.StatusOK, stats)
		return
	}
	stats, err := h.admin.GetStats(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// handleAdminDiagnoseRouting is the 玩家视角排查 entry (排查/诊断): given an
// account (and optional client filters mirroring the recommend request), it
// returns the routing walkthrough — same pipeline as /v1/routing/recommended
// with the intermediates exposed — plus the account's characters / directory
// entries so the operator sees both sides of the decision.
func (h *Handler) handleAdminDiagnoseRouting(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	req := routing.Request{
		Region:   q.Get("region"),
		Version:  q.Get("version"),
		Platform: q.Get("platform"),
	}
	if acct := q.Get("account_id"); acct != "" {
		n, err := strconv.ParseInt(acct, 10, 64)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid account_id")
			return
		}
		req.AccountID = n
	}
	if st := q.Get("status"); st != "" {
		status := model.ServerStatus(st)
		if !status.Valid() {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid status")
			return
		}
		req.Status = status
	}

	diag, err := h.routing.Diagnose(r.Context(), req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	resp := map[string]any{"diagnosis": diag}
	// Account dimension: characters (the directory projection IS the index
	// entry list — account_id, server_id, character_id, last_login_at).
	if req.AccountID > 0 {
		chars, err := h.directory.ListByAccount(r.Context(), req.AccountID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
			return
		}
		resp["characters"] = chars
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleAdminRateLimits serves the read-only rate-limit view: the parsed
// effective rules (prefix / rps / burst, longest-prefix-first) plus 429
// counts by endpoint rule and client IP. Changes go through env config and
// a restart — this endpoint never mutates.
func (h *Handler) handleAdminRateLimits(w http.ResponseWriter, r *http.Request) {
	if h.limiter == nil {
		writeJSON(w, http.StatusOK, RateLimitStats{
			Enabled:            false,
			RejectedByEndpoint: map[string]int{},
			RejectedByClient:   map[string]int{},
		})
		return
	}
	writeJSON(w, http.StatusOK, h.limiter.Stats())
}

// parseSeriesWindow maps the chart window tiers (5m/10m/30m/1h/10h) shared
// by the 负载时间视图 and 消息总线 panels. Empty defaults to 1h.
func parseSeriesWindow(raw string) (time.Duration, bool) {
	switch raw {
	case "", "1h":
		return time.Hour, true
	case "5m":
		return 5 * time.Minute, true
	case "10m":
		return 10 * time.Minute, true
	case "30m":
		return 30 * time.Minute, true
	case "10h":
		return 10 * time.Hour, true
	}
	return 0, false
}

// loadPoint is one merged sample of the two chart lines.
type loadPoint struct {
	T       time.Time `json:"t"`
	Players float64   `json:"players"`
	Load    float64   `json:"load"`
}

// handleAdminLoadSeries serves the 概览负载时间视图: players / load series
// for the whole fleet, one region, or one server (drill-down), over the
// shared window tiers. Scope resolution: server_id > region > fleet.
func (h *Handler) handleAdminLoadSeries(w http.ResponseWriter, r *http.Request) {
	window, ok := parseSeriesWindow(r.URL.Query().Get("window"))
	if !ok {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "window must be one of 5m/10m/30m/1h/10h")
		return
	}
	q := r.URL.Query()
	scope, prefix := "fleet", "load.fleet."
	if id := q.Get("server_id"); id != "" {
		scope, prefix = "server", "load.server."+id+"."
	} else if region := q.Get("region"); region != "" {
		scope, prefix = "region", "load.region."+region+"."
	}

	players, load := []telemetry.Point{}, []telemetry.Point{}
	if h.telemetry != nil {
		players = h.telemetry.Series(prefix+"players", window)
		load = h.telemetry.Series(prefix+"load", window)
	}

	// Same-probe rings tick in lockstep: zip index-wise, shortest wins.
	n := len(players)
	if len(load) < n {
		n = len(load)
	}
	points := make([]loadPoint, 0, n)
	for i := 0; i < n; i++ {
		points = append(points, loadPoint{T: players[i].T, Players: players[i].V, Load: load[i].V})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"scope":  scope,
		"window": r.URL.Query().Get("window"),
		"points": points,
	})
}

// handleAdminBusSeries serves the 消息总线 panel: per topic the backlog
// depth curve plus produce / consume rates computed from the cumulative
// counters over the window. Current totals come from the counting adapter.
func (h *Handler) handleAdminBusSeries(w http.ResponseWriter, r *http.Request) {
	window, ok := parseSeriesWindow(r.URL.Query().Get("window"))
	if !ok {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "window must be one of 5m/10m/30m/1h/10h")
		return
	}

	type topicView struct {
		Topic       string            `json:"topic"`
		Published   int64             `json:"published"`
		Consumed    int64             `json:"consumed"`
		InFlight    int64             `json:"in_flight"`
		ProduceRate float64           `json:"produce_rate"`
		ConsumeRate float64           `json:"consume_rate"`
		Depth       []telemetry.Point `json:"depth"`
	}
	topics := map[string]*topicView{}
	if h.telemetry != nil {
		for _, name := range h.telemetry.Names("bus.") {
			// name = bus.<topic>.<field> — topics themselves contain
			// dots (atlas.characters…), so the field is the LAST segment.
			rest := name[len("bus."):]
			field := rest[strings.LastIndex(rest, ".")+1:]
			topic := strings.TrimSuffix(rest, "."+field)
			if topic == "" || field == "" {
				continue
			}
			tv := topics[topic]
			if tv == nil {
				tv = &topicView{Topic: topic}
				topics[topic] = tv
			}
			switch field {
			case "depth":
				tv.Depth = h.telemetry.Series(name, window)
			case "produced":
				tv.ProduceRate = telemetry.Rate(h.telemetry.Series(name, window))
			case "consumed":
				tv.ConsumeRate = telemetry.Rate(h.telemetry.Series(name, window))
			}
		}
	}

	out := make([]*topicView, 0, len(topics))
	for _, tv := range topics {
		out = append(out, tv)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Topic < out[j].Topic })

	// Current counters straight from the adapter (exact even before the
	// next sampler tick).
	adapter := ""
	if h.bus != nil {
		adapter = h.bus.Name()
		for _, st := range h.bus.Stats() {
			tv := topics[st.Topic]
			if tv == nil {
				tv = &topicView{Topic: st.Topic}
				topics[st.Topic] = tv
				out = append(out, tv)
			}
			tv.Published, tv.Consumed, tv.InFlight = st.Published, st.Consumed, st.InFlight
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"adapter": adapter,
		"window":  r.URL.Query().Get("window"),
		"topics":  out,
	})
}

// handleAdminListServers serves the admin fleet list with the full filter
// bar: status / region / type / realm / shard / version / platform / tag,
// plus a server-ID substring search and a metadata key=value pair — the same
// dimensions the /servers page composes. Reads the fleet index when wired so
// list, stats, and filter facets share one aggregate (BUGS ③).
func (h *Handler) handleAdminListServers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	status := model.ServerStatus(q.Get("status"))
	if status != "" && !status.Valid() {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid status")
		return
	}
	limit := 0
	if l := q.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n <= 0 || n > 200 {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid limit")
			return
		}
		limit = n
	}
	filter := fleet.ListFilter{
		ID:            q.Get("id"),
		Status:        status,
		Region:        q.Get("region"),
		Realm:         q.Get("realm"),
		Shard:         q.Get("shard"),
		Version:       q.Get("version"),
		Type:          q.Get("type"),
		Platform:      q.Get("platform"),
		Tag:           q.Get("tag"),
		MetadataKey:   q.Get("metadata_key"),
		MetadataValue: q.Get("metadata_value"),
		Limit:         limit,
		Cursor:        q.Get("cursor"),
	}

	if h.fleet != nil {
		servers, next := h.fleet.List(filter)
		resp := map[string]any{"servers": servers}
		if next != "" {
			resp["next_cursor"] = next
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// No index wired (embedders, bare tests): fall back to the store with
	// the subset ServerFilter understands, post-filtering the rest so the
	// response contract stays identical.
	servers, err := h.store.ListServers(r.Context(), store.ServerFilter{
		Region:   filter.Region,
		Realm:    filter.Realm,
		Shard:    filter.Shard,
		Version:  filter.Version,
		Platform: filter.Platform,
		Status:   filter.Status,
		// Single page: the client walks with cursor + next_cursor.
		Limit: store.ListServersMaxLimit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	matched := make([]*model.Server, 0, len(servers))
	for _, srv := range servers {
		if fleet.Matches(srv, filter) {
			matched = append(matched, srv)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })
	if filter.Cursor != "" {
		start := 0
		for i, srv := range matched {
			if srv.ID > filter.Cursor {
				start = i
				break
			}
			start = i + 1
		}
		matched = matched[start:]
	}
	if limit <= 0 {
		limit = 50
	}
	next := ""
	if len(matched) > limit {
		matched = matched[:limit]
		next = matched[limit-1].ID
	}
	resp := map[string]any{"servers": matched}
	if next != "" {
		resp["next_cursor"] = next
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) handleAdminSearchCharacters(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := store.CharacterSearchFilter{
		Name:          q.Get("q"),
		ServerID:      q.Get("server_id"),
		MetadataKey:   q.Get("metadata_key"),
		MetadataValue: q.Get("metadata_value"),
		Cursor:        q.Get("cursor"),
	}

	// 玩家 ID 搜索: the opaque account reference. class_id is no longer a
	// built-in parameter (platform de-hardening — filter via
	// metadata_key=class instead).
	if acctStr := q.Get("account_id"); acctStr != "" {
		n, err := strconv.ParseInt(acctStr, 10, 64)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid account_id")
			return
		}
		filter.AccountID = n
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

// ── Realms & Shards (TODO v0.1.14) ──────────────────────────────

func (h *Handler) handleAdminCreateRealm(w http.ResponseWriter, r *http.Request) {
	var req admin.CreateRealmRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON body")
		return
	}

	realm, err := h.admin.CreateRealm(r.Context(), req)
	if err != nil {
		if errors.Is(err, model.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		if errors.Is(err, model.ErrConflict) {
			writeError(w, http.StatusConflict, "REALM_EXISTS", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, realm)
}

func (h *Handler) handleAdminListRealms(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	realms, err := h.admin.ListRealms(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"realms": realms,
	})
}

func (h *Handler) handleAdminCreateShard(w http.ResponseWriter, r *http.Request) {
	var req admin.CreateShardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON body")
		return
	}

	shard, err := h.admin.CreateShard(r.Context(), req)
	if err != nil {
		if errors.Is(err, model.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "REALM_NOT_FOUND", err.Error())
			return
		}
		if errors.Is(err, model.ErrConflict) {
			writeError(w, http.StatusConflict, "SHARD_EXISTS", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, shard)
}

func (h *Handler) handleAdminListShards(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	realmID := r.URL.Query().Get("realm_id")

	shards, err := h.admin.ListShards(r.Context(), realmID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"shards": shards,
	})
}

// ── Server tags (标记体系) ──────────────────────────────────────

func (h *Handler) handleAdminGetServerTags(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tags, err := h.admin.GetServerTags(r.Context(), id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "SERVER_NOT_FOUND", fmt.Sprintf("server %s not found", id))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server_id": id,
		"tags":      tags,
	})
}

func (h *Handler) handleAdminAddServerTag(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req admin.AddServerTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON body")
		return
	}

	tags, err := h.admin.AddServerTag(r.Context(), id, req)
	if err != nil {
		if errors.Is(err, model.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "SERVER_NOT_FOUND", fmt.Sprintf("server %s not found", id))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server_id": id,
		"tags":      tags,
	})
}

func (h *Handler) handleAdminRemoveServerTag(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	code := r.PathValue("code")

	// Distinguish the two 404s: an unknown server vs an unknown tag on a
	// known server.
	if _, err := h.admin.GetServerTags(r.Context(), id); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "SERVER_NOT_FOUND", fmt.Sprintf("server %s not found", id))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	if err := h.admin.RemoveServerTag(r.Context(), id, code); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "TAG_NOT_FOUND", fmt.Sprintf("no tag %q on server %s", code, id))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server_id": id,
		"removed":   code,
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

// CORSMiddleware guards cross-origin browser access with an explicit
// allowlist. Origins come from ATLAS_CORS_ORIGINS (comma-separated):
// empty config emits no CORS headers at all — nothing opens by default,
// an unconfigured Atlas stays same-origin only. An explicit "*" allows
// any origin (development only). Allowlisted origins are echoed back
// with Vary: Origin rather than a blanket "*"; preflight OPTIONS
// short-circuits 204 only for allowlisted origins, everything else
// passes through untouched.
func CORSMiddleware(allowedOrigins string) func(http.Handler) http.Handler {
	allowAll := false
	allowed := make(map[string]struct{})
	for _, o := range strings.Split(allowedOrigins, ",") {
		if o = strings.TrimSpace(o); o == "" {
			continue
		}
		if o == "*" {
			allowAll = true
			continue
		}
		allowed[o] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if allowedOrigins == "" || origin == "" {
				next.ServeHTTP(w, r)
				return
			}
			if allowAll {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else if _, ok := allowed[origin]; !ok {
				// Outside the allowlist: no headers, no preflight blessing.
				next.ServeHTTP(w, r)
				return
			} else {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Add("Vary", "Origin")
			}

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
}

// ── Maintenance windows & announcements (TODO v0.1.20) ──────────

func (h *Handler) handleAdminCreateMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req admin.CreateMaintenanceWindowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON body")
		return
	}

	mw, err := h.admin.CreateMaintenanceWindow(r.Context(), id, req)
	if err != nil {
		if errors.Is(err, model.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		if store.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "SERVER_NOT_FOUND", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, mw)
}

func (h *Handler) handleAdminListMaintenanceWindows(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}

	windows, err := h.admin.ListMaintenanceWindows(r.Context(), r.URL.Query().Get("server_id"), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"maintenance_windows": windows,
	})
}

func (h *Handler) handleAdminDeleteMaintenanceWindow(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.admin.DeleteMaintenanceWindow(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleAdminCreateAnnouncement(w http.ResponseWriter, r *http.Request) {
	var req admin.CreateAnnouncementRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON body")
		return
	}

	a, err := h.admin.CreateAnnouncement(r.Context(), req)
	if err != nil {
		if errors.Is(err, model.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		if store.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "SERVER_NOT_FOUND", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, a)
}

func (h *Handler) handleAdminListAnnouncements(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	f := store.AnnouncementFilter{
		ServerID:   r.URL.Query().Get("server_id"),
		ActiveOnly: r.URL.Query().Get("active") == "true",
		Limit:      limit,
	}

	announcements, err := h.admin.ListAnnouncements(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"announcements": announcements,
	})
}

func (h *Handler) handleAdminDeleteAnnouncement(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.admin.DeleteAnnouncement(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListAnnouncements serves active announcements to game clients: global
// plus, when server_id is given, that server's own notices.
func (h *Handler) handleListAnnouncements(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 {
			limit = n
		}
	}
	f := store.AnnouncementFilter{
		ServerID:   r.URL.Query().Get("server_id"),
		ActiveOnly: true,
		Limit:      limit,
	}

	announcements, err := h.admin.ListAnnouncements(r.Context(), f)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"announcements": announcements,
	})
}

// ── Cross-server config (config center) ─────────────────────────

// handleGetCrossServerConfig serves GET /v1/crossserver/config — the pull
// endpoint game servers call at startup and after every change signal.
//
// Semantics split by caller state:
//   - A bare GET (startup pull) is strict: 404 CONFIG_NOT_FOUND when
//     nothing has been published, so a booting server fails fast and
//     loudly instead of silently running without coordination config.
//   - ?version=N&hash=H (poll-mode fallback) or If-None-Match are
//     conditional: they compare against the current snapshot and answer
//     304 when nothing changed, never 404 — a running server keeps its
//     old config and keeps polling.
func (h *Handler) handleGetCrossServerConfig(w http.ResponseWriter, r *http.Request) {
	if h.crossserver == nil {
		writeError(w, http.StatusServiceUnavailable, "CONFIG_CENTER_DISABLED", "cross-server config service is not wired")
		return
	}

	q := r.URL.Query()
	if q.Get("version") != "" || q.Get("hash") != "" {
		known := 0
		if v := q.Get("version"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid version")
				return
			}
			known = n
		}
		cfg, changed, err := h.crossserver.Since(r.Context(), known, q.Get("hash"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
			return
		}
		etag := cfg.ETag()
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "no-cache")
		if !changed {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		writeJSON(w, http.StatusOK, cfg)
		return
	}

	cfg, err := h.crossserver.Get(r.Context())
	if err != nil {
		if store.IsNotFound(err) || errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "CONFIG_NOT_FOUND", "no cross-server config has been published yet")
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	etag := cfg.ETag()
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-cache")
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// etagMatches reports whether an If-None-Match header covers etag, using the
// weak comparison RFC 7232 §3.2 prescribes: a proxy that re-encodes the body
// (Cloudflare turns "x" into W/"x") still describes the same representation,
// and polling servers behind it must still get their 304 instead of a full
// re-download every tick. The header may also carry a list or "*".
func etagMatches(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return true
		}
		if strings.TrimPrefix(candidate, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
	}
	return false
}

// handleAdminGetCrossServerConfig is the admin read of the same document.
// Unlike the strict startup pull it answers the version-0 empty snapshot
// when nothing was published yet — the dashboard shows a real baseline.
func (h *Handler) handleAdminGetCrossServerConfig(w http.ResponseWriter, r *http.Request) {
	if h.crossserver == nil {
		writeError(w, http.StatusServiceUnavailable, "CONFIG_CENTER_DISABLED", "cross-server config service is not wired")
		return
	}
	cfg, err := h.crossserver.Snapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

// handleAdminUpdateCrossServerConfig replaces the config spec (PUT, full
// document). The store bumps the version atomically and the response
// reports how the change signal fanned out (bus + callbacks). Saving
// identical content is idempotent: no version bump, no signal.
func (h *Handler) handleAdminUpdateCrossServerConfig(w http.ResponseWriter, r *http.Request) {
	if h.crossserver == nil {
		writeError(w, http.StatusServiceUnavailable, "CONFIG_CENTER_DISABLED", "cross-server config service is not wired")
		return
	}
	var spec model.CrossServerSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid JSON body")
		return
	}

	// Save fetches the stored snapshot itself for the idempotency check and
	// the target diff; the store owns the version, so a stale caller can
	// never overwrite a newer one.
	res, err := h.crossserver.Save(r.Context(), spec)
	if err != nil {
		if errors.Is(err, model.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleAdminIndexQueueStatus returns the instruction queue snapshot.
// Only the memory store runs an instruction queue: stores without the
// provider — and composites whose snapshot reports Enabled=false (SQL
// backends) — answer 503 INDEX_QUEUE_DISABLED, never an empty snapshot.
func (h *Handler) handleAdminIndexQueueStatus(w http.ResponseWriter, r *http.Request) {
	qsp, ok := h.store.(store.QueueStatusProvider)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "INDEX_QUEUE_DISABLED", "instruction queue is not available (non-memory store)")
		return
	}
	stats := qsp.QueueStats()
	if !stats.Enabled {
		writeError(w, http.StatusServiceUnavailable, "INDEX_QUEUE_DISABLED", "instruction queue is not available (non-memory store)")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
