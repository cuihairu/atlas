package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/crossserver"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	"github.com/cuihairu/atlas/internal/event"
	httpadapter "github.com/cuihairu/atlas/internal/event/http"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// setupAuthRegistryServer mounts the registry routes behind RegistryAuth with
// a single known token — the production internal-listener wiring.
func setupAuthRegistryServer(t *testing.T) *httptest.Server {
	t.Helper()
	mem := memory.New()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	regSvc := registry.New(mem, mem, logger)
	discSvc := discovery.New(mem, mem)
	dirSvc := directory.New(mem).WithPlayerSessions(mem)
	admSvc := admin.New(mem)
	events := httpadapter.New()
	_ = events.Subscribe(context.Background(), event.TopicCharacters, func(_ context.Context, e *event.Event) error {
		_, err := dirSvc.ApplyEvent(context.Background(), e)
		return err
	})

	rtSvc := routing.New(mem, mem, mem, mem)
	crossSvc := crossserver.New(mem, mem, events, logger)
	handler := New(regSvc, discSvc, dirSvc, admSvc, rtSvc, crossSvc, mem, events, logger)

	registryMux := http.NewServeMux()
	handler.RegisterRegistryRoutes(registryMux)
	return httptest.NewServer(RegistryAuth(RegistryAuthConfig{
		Tokens: map[string]struct{}{"secret-token": {}},
		Logger: logger,
	})(registryMux))
}

// presenceURL posts a presence report for the given server / character.
func presenceURL(t *testing.T, ts *httptest.Server, serverID string, body any) *http.Response {
	t.Helper()
	return postJSON(t, ts, "/v1/registry/servers/"+serverID+"/presence", body)
}

func loginReport(account, char int64, session string) map[string]any {
	return map[string]any{"action": "login", "account_id": account, "character_id": char, "session_id": session}
}

func logoutReport(account, char int64, session, reason string) map[string]any {
	m := map[string]any{"action": "logout", "account_id": account, "character_id": char, "session_id": session}
	if reason != "" {
		m["end_reason"] = reason
	}
	return m
}

func createTestCharacter(t *testing.T, ts *httptest.Server, serverID string, account, char int64, name string) {
	t.Helper()
	resp := postJSON(t, ts, "/v1/directory/characters", map[string]any{
		"account_id":   account,
		"server_id":    serverID,
		"character_id": char,
		"name":         name,
		"level":        1,
		"class_id":     1,
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create char: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// A game server reports login → duplicate login (idempotent) → logout →
// duplicate logout (idempotent); the presence status follows the state
// machine and the session history stays consistent.
func TestPresenceLoginLogoutFlow(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "game-1001")
	createTestCharacter(t, ts, "game-1001", 10001, 823712, "PresenceChar")

	// Login opens a session and flips online.
	resp := presenceURL(t, ts, "game-1001", loginReport(10001, 823712, "sess-a"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", resp.StatusCode)
	}
	var body struct {
		PresenceStatus string `json:"presence_status"`
		Presence       struct {
			PresenceStatus string `json:"presence_status"`
			ActiveSession  struct {
				SessionID string `json:"session_id"`
			} `json:"active_session"`
		} `json:"presence"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode login resp: %v", err)
	}
	resp.Body.Close()
	if body.PresenceStatus != "online" || body.Presence.PresenceStatus != "online" {
		t.Errorf("presence_status = %q, want online", body.PresenceStatus)
	}
	if body.Presence.ActiveSession.SessionID != "sess-a" {
		t.Errorf("active session = %q, want sess-a", body.Presence.ActiveSession.SessionID)
	}

	// Duplicate login (same session) is idempotent: still online, no error.
	resp = presenceURL(t, ts, "game-1001", loginReport(10001, 823712, "sess-a"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("duplicate login: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Logout closes the session and flips offline.
	resp = presenceURL(t, ts, "game-1001", logoutReport(10001, 823712, "sess-a", "normal"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout: expected 200, got %d", resp.StatusCode)
	}
	body = struct {
		PresenceStatus string `json:"presence_status"`
		Presence       struct {
			PresenceStatus string `json:"presence_status"`
			ActiveSession  struct {
				SessionID string `json:"session_id"`
			} `json:"active_session"`
		} `json:"presence"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode logout resp: %v", err)
	}
	resp.Body.Close()
	if body.PresenceStatus != "offline" {
		t.Errorf("presence_status after logout = %q, want offline", body.PresenceStatus)
	}

	// Duplicate logout (already offline) is idempotent.
	resp = presenceURL(t, ts, "game-1001", logoutReport(10001, 823712, "sess-a", "normal"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("duplicate logout: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Re-login after logout opens a fresh session.
	resp = presenceURL(t, ts, "game-1001", loginReport(10001, 823712, "sess-b"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("re-login: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// Login while online in a different session is an illegal transition → 409.
func TestPresenceLoginDifferentSessionConflict(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "game-1001")
	createTestCharacter(t, ts, "game-1001", 10001, 823712, "PresenceChar")

	resp := presenceURL(t, ts, "game-1001", loginReport(10001, 823712, "sess-a"))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	resp = presenceURL(t, ts, "game-1001", loginReport(10001, 823712, "sess-b"))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("login different session: expected 409, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// Logout naming a session that is not the open one is an illegal transition
// → 409; logout on a character that never logged in → 400 (invalid).
func TestPresenceIllegalTransitions(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "game-1001")
	createTestCharacter(t, ts, "game-1001", 10001, 823712, "PresenceChar")
	createTestCharacter(t, ts, "game-1001", 10002, 823713, "NeverLoggedIn")

	// Wrong session on logout.
	resp := presenceURL(t, ts, "game-1001", loginReport(10001, 823712, "sess-a"))
	resp.Body.Close()
	resp = presenceURL(t, ts, "game-1001", logoutReport(10001, 823712, "sess-other", "kicked"))
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("logout wrong session: expected 409, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Logout on a character that never logged in (unknown).
	resp = presenceURL(t, ts, "game-1001", logoutReport(10002, 823713, "", "normal"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("logout unknown: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// Invalid reports are rejected before enqueue: bad action, missing
// session_id on login, invalid end_reason, unknown server.
func TestPresenceInvalidReports(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "game-1001")
	createTestCharacter(t, ts, "game-1001", 10001, 823712, "PresenceChar")

	cases := []struct {
		name string
		path string
		body any
		want int
	}{
		{"bad action", "/v1/registry/servers/game-1001/presence",
			map[string]any{"action": "teleport", "account_id": 10001, "character_id": 823712}, http.StatusBadRequest},
		{"missing session_id on login", "/v1/registry/servers/game-1001/presence",
			map[string]any{"action": "login", "account_id": 10001, "character_id": 823712}, http.StatusBadRequest},
		{"invalid end_reason", "/v1/registry/servers/game-1001/presence",
			logoutReport(10001, 823712, "sess-a", "banana"), http.StatusBadRequest},
		{"missing account_id", "/v1/registry/servers/game-1001/presence",
			map[string]any{"action": "login", "character_id": 823712, "session_id": "s"}, http.StatusBadRequest},
		{"unknown server", "/v1/registry/servers/game-9999/presence",
			loginReport(10001, 823712, "sess-a"), http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := postJSON(t, ts, tc.path, tc.body)
			if resp.StatusCode != tc.want {
				t.Errorf("expected %d, got %d", tc.want, resp.StatusCode)
			}
			resp.Body.Close()
		})
	}
}

// The report channel rides the registry trust zone: without a service token
// the endpoint is rejected before it reaches the handler. This mirrors the
// production wiring (cmd/atlas/main.go mounts RegisterRegistryRoutes behind
// RegistryAuth on the internal listener), not the legacy single-port mux.
func TestPresenceRequiresAuth(t *testing.T) {
	ts := setupAuthRegistryServer(t)
	defer ts.Close()

	// No token → 401.
	resp := presenceURL(t, ts, "game-1001", loginReport(1, 2, "s"))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: expected 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Wrong token → 401.
	req, _ := http.NewRequest("POST", ts.URL+"/v1/registry/servers/game-1001/presence",
		strings.NewReader(`{"action":"login","account_id":1,"character_id":2,"session_id":"s"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token: expected 401, got %d", resp.StatusCode)
	}

	// Valid token → passes the token gate; 404 because the server is not
	// registered (the server gate, not the auth gate, decided).
	req, _ = http.NewRequest("POST", ts.URL+"/v1/registry/servers/game-1001/presence",
		strings.NewReader(`{"action":"login","account_id":1,"character_id":2,"session_id":"s"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err = ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("valid token: expected 404 (server not registered), got %d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// PP-3 query surface
// ---------------------------------------------------------------------------

type characterPresence struct {
	PresenceStatus string `json:"presence_status"`
	ActiveSession  *struct {
		SessionID string `json:"session_id"`
	} `json:"active_session"`
	Character *struct {
		CharacterID int64 `json:"character_id"`
	} `json:"character"`
}

// 角色在线查询: GET /v1/directory/characters/{character_id}/presence
func TestPresenceCharacterQuery(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "game-1001")
	createTestCharacter(t, ts, "game-1001", 10001, 823712, "PresenceChar")

	// Unknown character → 404.
	resp := getJSON(t, ts, "/v1/directory/characters/999999/presence")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("unknown char: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Invalid character_id → 400.
	resp = getJSON(t, ts, "/v1/directory/characters/abc/presence")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid char id: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Never-logged-in character → 200, presence unknown, no session.
	resp = getJSON(t, ts, "/v1/directory/characters/823712/presence")
	var body characterPresence
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode presence: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("presence: expected 200, got %d", resp.StatusCode)
	}
	if body.PresenceStatus != "unknown" || body.ActiveSession != nil || body.Character == nil {
		t.Errorf("fresh presence = %+v, want unknown / no session / character", body)
	}

	// Login → online with the active session.
	presenceURL(t, ts, "game-1001", loginReport(10001, 823712, "sess-a"))
	resp = getJSON(t, ts, "/v1/directory/characters/823712/presence")
	body = characterPresence{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode presence: %v", err)
	}
	resp.Body.Close()
	if body.PresenceStatus != "online" {
		t.Errorf("presence_status = %q, want online", body.PresenceStatus)
	}
	if body.ActiveSession == nil || body.ActiveSession.SessionID != "sess-a" {
		t.Errorf("active_session = %+v, want sess-a", body.ActiveSession)
	}

	// Logout → offline, no session.
	presenceURL(t, ts, "game-1001", logoutReport(10001, 823712, "sess-a", "normal"))
	resp = getJSON(t, ts, "/v1/directory/characters/823712/presence")
	body = characterPresence{}
	json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	if body.PresenceStatus != "offline" || body.ActiveSession != nil {
		t.Errorf("after logout = %+v, want offline / no session", body)
	}
}

// 某服在线名单与计数: GET /v1/directory/servers/{id}/presence
func TestPresenceServerRoster(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "game-1001")
	registerTestServer(t, ts, "game-1002")

	for _, c := range []struct {
		server        string
		account, char int64
		name          string
	}{
		{"game-1001", 10001, 823712, "Alpha"},
		{"game-1001", 10002, 823713, "Bravo"},
		{"game-1001", 10003, 823714, "Charlie"},
		{"game-1002", 10004, 823715, "Delta"},
	} {
		createTestCharacter(t, ts, c.server, c.account, c.char, c.name)
	}

	// Only Alpha and Charlie on game-1001 are online; Delta on game-1002 is online.
	presenceURL(t, ts, "game-1001", loginReport(10001, 823712, "s1"))
	presenceURL(t, ts, "game-1001", loginReport(10003, 823714, "s3"))
	presenceURL(t, ts, "game-1002", loginReport(10004, 823715, "s4"))

	resp := getJSON(t, ts, "/v1/directory/servers/game-1001/presence")
	var roster struct {
		ServerID    string `json:"server_id"`
		OnlineCount int    `json:"online_count"`
		Characters  []struct {
			CharacterID int64 `json:"character_id"`
		} `json:"characters"`
		NextCursor string `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&roster); err != nil {
		t.Fatalf("decode roster: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("roster: expected 200, got %d", resp.StatusCode)
	}
	if roster.ServerID != "game-1001" || roster.OnlineCount != 2 || len(roster.Characters) != 2 {
		t.Errorf("roster = %+v, want game-1001 with 2 online", roster)
	}
	gotIDs := []int64{roster.Characters[0].CharacterID, roster.Characters[1].CharacterID}
	if gotIDs[0] != 823712 || gotIDs[1] != 823714 {
		t.Errorf("roster ids = %v, want [823712, 823714]", gotIDs)
	}
	// The cursor is a resume token (last returned character_id), populated
	// even on the final page: reading past it yields an empty roster.
	if roster.NextCursor != "823714" {
		t.Errorf("next_cursor = %q, want 823714 (resume token)", roster.NextCursor)
	}
	resp = getJSON(t, ts, "/v1/directory/servers/game-1001/presence?cursor="+roster.NextCursor)
	roster = struct {
		ServerID    string `json:"server_id"`
		OnlineCount int    `json:"online_count"`
		Characters  []struct {
			CharacterID int64 `json:"character_id"`
		} `json:"characters"`
		NextCursor string `json:"next_cursor"`
	}{}
	json.NewDecoder(resp.Body).Decode(&roster)
	resp.Body.Close()
	if len(roster.Characters) != 0 {
		t.Errorf("roster after cursor = %+v, want empty", roster.Characters)
	}

	// The other server keeps its own roster.
	resp = getJSON(t, ts, "/v1/directory/servers/game-1002/presence")
	roster = struct {
		ServerID    string `json:"server_id"`
		OnlineCount int    `json:"online_count"`
		Characters  []struct {
			CharacterID int64 `json:"character_id"`
		} `json:"characters"`
		NextCursor string `json:"next_cursor"`
	}{}
	json.NewDecoder(resp.Body).Decode(&roster)
	resp.Body.Close()
	if roster.OnlineCount != 1 || len(roster.Characters) != 1 || roster.Characters[0].CharacterID != 823715 {
		t.Errorf("game-1002 roster = %+v, want [823715] online", roster)
	}

	// Cursor pagination walks the roster.
	resp = getJSON(t, ts, "/v1/directory/servers/game-1001/presence?limit=1")
	roster = struct {
		ServerID    string `json:"server_id"`
		OnlineCount int    `json:"online_count"`
		Characters  []struct {
			CharacterID int64 `json:"character_id"`
		} `json:"characters"`
		NextCursor string `json:"next_cursor"`
	}{}
	json.NewDecoder(resp.Body).Decode(&roster)
	resp.Body.Close()
	if len(roster.Characters) != 1 || roster.Characters[0].CharacterID != 823712 || roster.NextCursor == "" {
		t.Errorf("page1 = %+v, want [823712] with a cursor", roster)
	}
	resp = getJSON(t, ts, "/v1/directory/servers/game-1001/presence?limit=1&cursor="+roster.NextCursor)
	roster = struct {
		ServerID    string `json:"server_id"`
		OnlineCount int    `json:"online_count"`
		Characters  []struct {
			CharacterID int64 `json:"character_id"`
		} `json:"characters"`
		NextCursor string `json:"next_cursor"`
	}{}
	json.NewDecoder(resp.Body).Decode(&roster)
	resp.Body.Close()
	if len(roster.Characters) != 1 || roster.Characters[0].CharacterID != 823714 {
		t.Errorf("page2 = %+v, want [823714]", roster)
	}

	// Invalid limit → 400.
	resp = getJSON(t, ts, "/v1/directory/servers/game-1001/presence?limit=0")
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("invalid limit: expected 400, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// A login for a character that is not in the index surfaces the store's
// ErrNotFound through the event path as 404 CHARACTER_NOT_FOUND — distinct
// from the SERVER_NOT_FOUND an unregistered path server yields (docs
// presence.md §2 error table).
func TestPresenceUnknownCharacterNotFound(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "game-1001")

	resp := presenceURL(t, ts, "game-1001", loginReport(10001, 999999, "sess-x"))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown character: expected 404, got %d", resp.StatusCode)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	resp.Body.Close()
	if body.Error.Code != "CHARACTER_NOT_FOUND" {
		t.Errorf("error code = %q, want CHARACTER_NOT_FOUND", body.Error.Code)
	}
}

// With an asynchronous adapter the report is queued, not applied: 202 with
// {"status":"queued"} (docs presence.md §2) — the state-dependent checks run
// at consumption time and surface as handler failures, not HTTP errors.
func TestPresenceAsyncQueues(t *testing.T) {
	ts, _ := setupServerWithEvents(t, &stubEventAdapter{sync: false}, nil)
	defer ts.Close()
	registerTestServer(t, ts, "game-1001")

	// The async path never consults the store: character existence is only
	// checked at consumption time, so the report queues regardless.
	resp := presenceURL(t, ts, "game-1001", loginReport(10001, 823712, "sess-a"))
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("async login: expected 202, got %d", resp.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode queued body: %v", err)
	}
	resp.Body.Close()
	if body.Status != "queued" {
		t.Errorf("status = %q, want queued", body.Status)
	}

	// Logout queues the same way.
	resp = presenceURL(t, ts, "game-1001", logoutReport(10001, 823712, "sess-a", "normal"))
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("async logout: expected 202, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}
