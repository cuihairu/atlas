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

	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/store"
)

// Handler holds all Atlas HTTP handlers and their dependencies.
type Handler struct {
	registry  *registry.Service
	discovery *discovery.Service
	directory *directory.Service
	store     store.Store
	logger    *slog.Logger
}

// New creates a new Handler.
func New(reg *registry.Service, disc *discovery.Service, dir *directory.Service, s store.Store, logger *slog.Logger) *Handler {
	return &Handler{
		registry:  reg,
		discovery: disc,
		directory: dir,
		store:     s,
		logger:    logger,
	}
}

// RegisterRoutes registers all Atlas routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Registry
	mux.HandleFunc("POST /v1/registry/servers/register", h.handleRegister)
	mux.HandleFunc("POST /v1/registry/servers/{id}/heartbeat", h.handleHeartbeat)
	mux.HandleFunc("POST /v1/registry/servers/{id}/unregister", h.handleUnregister)

	// Discovery
	mux.HandleFunc("GET /v1/discovery/servers", h.handleListServers)
	mux.HandleFunc("GET /v1/discovery/servers/{id}", h.handleGetServer)

	// Directory
	mux.HandleFunc("POST /v1/directory/characters", h.handleCreateCharacter)
	mux.HandleFunc("GET /v1/directory/accounts/{id}/characters", h.handleListCharactersByAccount)
	mux.HandleFunc("GET /v1/directory/characters/{character_id}", h.handleGetCharacter)
	mux.HandleFunc("GET /v1/directory/servers/{id}/characters", h.handleListCharactersByServer)
	mux.HandleFunc("PATCH /v1/directory/characters/{character_id}", h.handlePatchCharacter)
	mux.HandleFunc("DELETE /v1/directory/characters/{character_id}", h.handleDeleteCharacter)

	// Health
	mux.HandleFunc("GET /healthz", h.handleHealthz)
	mux.HandleFunc("GET /readyz", h.handleReadyz)
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

	ch, err := h.directory.CreateCharacter(r.Context(), req.AccountID, req.ServerID, req.CharacterID, req.Name, req.Level, req.ClassID)
	if err != nil {
		if errors.Is(err, model.ErrInvalid) {
			writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, ch)
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

	patch := store.CharacterPatch{
		Name:    req.Name,
		Level:   req.Level,
		ClassID: req.ClassID,
		Avatar:  req.Avatar,
	}

	ch, err := h.directory.UpdateCharacter(r.Context(), req.AccountID, req.ServerID, charID, patch)
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

func (h *Handler) handleDeleteCharacter(w http.ResponseWriter, r *http.Request) {
	charIDStr := r.PathValue("character_id")
	charID, err := strconv.ParseInt(charIDStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "invalid character_id")
		return
	}

	// We need account_id and server_id to delete. Try to look up the character first.
	ch, err := h.directory.GetCharacterByCharacterID(r.Context(), charID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			writeError(w, http.StatusNotFound, "CHARACTER_NOT_FOUND", fmt.Sprintf("character %d not found", charID))
			return
		}
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	if err := h.directory.DeleteCharacter(r.Context(), ch.AccountID, ch.ServerID, charID); err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
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