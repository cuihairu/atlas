package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/crossserver"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	httpadapter "github.com/cuihairu/atlas/internal/event/http"
	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// presenceResp decodes the presence endpoint's per-event results.
type presenceResp struct {
	ServerID string                 `json:"server_id"`
	Results  []model.PresenceResult `json:"results"`
}

func decodePresence(t *testing.T, resp *http.Response) presenceResp {
	t.Helper()
	defer resp.Body.Close()
	var pr presenceResp
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		t.Fatalf("decode presence response: %v", err)
	}
	return pr
}

// seedChar creates a character through the directory endpoint so the session
// store finds it in the index.
func seedChar(t *testing.T, ts *httptest.Server, acct, chID int64, serverID string) {
	t.Helper()
	body := map[string]any{
		"account_id":   acct,
		"server_id":    serverID,
		"character_id": chID,
		"name":         "presence-char",
		"level":        1,
	}
	resp := postJSON(t, ts, "/v1/directory/characters", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create char: expected 201, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func charPresence(t *testing.T, ts *httptest.Server, chID int64) model.PresenceStatus {
	t.Helper()
	resp := getJSON(t, ts, fmt.Sprintf("/v1/directory/characters/%d", chID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get char: expected 200, got %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	var ch model.Character
	if err := json.NewDecoder(resp.Body).Decode(&ch); err != nil {
		t.Fatalf("decode character: %v", err)
	}
	return ch.PresenceStatus
}

func loginEvent(sessionID string, acct, chID int64) model.PresenceEvent {
	return model.PresenceEvent{
		Kind: model.PresenceEventLogin, SessionID: sessionID,
		AccountID: acct, CharacterID: chID,
	}
}

func logoutEvent(sessionID string, reason model.SessionEndReason) model.PresenceEvent {
	now := time.Now().UTC()
	return model.PresenceEvent{
		Kind: model.PresenceEventLogout, SessionID: sessionID,
		EndedAt: &now, EndReason: reason,
	}
}

// TestPresenceLoginLogoutLifecycle drives a full open/close/reopen cycle over
// the endpoint and asserts the presence projection follows the session.
func TestPresenceLoginLogoutLifecycle(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "game-1001")
	const (
		acct = 2001001
		chID = 9001001
	)
	seedChar(t, ts, acct, chID, "game-1001")

	resp := postJSON(t, ts, "/v1/registry/servers/game-1001/presence",
		model.PresenceReport{Events: []model.PresenceEvent{loginEvent("ps-1", acct, chID)}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", resp.StatusCode)
	}
	pr := decodePresence(t, resp)
	if len(pr.Results) != 1 || pr.Results[0].Status != model.PresenceApplied {
		t.Fatalf("login results = %+v, want one applied", pr.Results)
	}
	if got := charPresence(t, ts, chID); got != model.PresenceOnline {
		t.Fatalf("presence after login = %q, want online", got)
	}

	resp = postJSON(t, ts, "/v1/registry/servers/game-1001/presence",
		model.PresenceReport{Events: []model.PresenceEvent{logoutEvent("ps-1", model.SessionEndNormal)}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout: expected 200, got %d", resp.StatusCode)
	}
	pr = decodePresence(t, resp)
	if len(pr.Results) != 1 || pr.Results[0].Status != model.PresenceApplied {
		t.Fatalf("logout results = %+v, want one applied", pr.Results)
	}
	if got := charPresence(t, ts, chID); got != model.PresenceOffline {
		t.Fatalf("presence after logout = %q, want offline", got)
	}

	resp = postJSON(t, ts, "/v1/registry/servers/game-1001/presence",
		model.PresenceReport{Events: []model.PresenceEvent{loginEvent("ps-2", acct, chID)}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reopen: expected 200, got %d", resp.StatusCode)
	}
	if got := charPresence(t, ts, chID); got != model.PresenceOnline {
		t.Fatalf("presence after reopen = %q, want online", got)
	}
}

// TestPresenceDuplicateIdempotent pins the retry-safe semantics: re-reporting
// a login for the already-open session or a logout for the already-closed
// session is classified already_applied, not an error, and the projection is
// unchanged.
func TestPresenceDuplicateIdempotent(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "game-1001")
	const (
		acct = 2001002
		chID = 9001002
	)
	seedChar(t, ts, acct, chID, "game-1001")

	login := model.PresenceReport{Events: []model.PresenceEvent{loginEvent("dup-1", acct, chID)}}
	resp := postJSON(t, ts, "/v1/registry/servers/game-1001/presence", login)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first login: expected 200, got %d", resp.StatusCode)
	}
	decodePresence(t, resp)

	// Same session id re-reported: idempotent.
	resp = postJSON(t, ts, "/v1/registry/servers/game-1001/presence", login)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("duplicate login: expected 200, got %d", resp.StatusCode)
	}
	pr := decodePresence(t, resp)
	if len(pr.Results) != 1 || pr.Results[0].Status != model.PresenceAlreadyApplied {
		t.Fatalf("duplicate login = %+v, want already_applied", pr.Results)
	}
	if got := charPresence(t, ts, chID); got != model.PresenceOnline {
		t.Fatalf("presence after duplicate login = %q, want online (unchanged)", got)
	}

	logout := model.PresenceReport{Events: []model.PresenceEvent{logoutEvent("dup-1", model.SessionEndNormal)}}
	resp = postJSON(t, ts, "/v1/registry/servers/game-1001/presence", logout)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout: expected 200, got %d", resp.StatusCode)
	}
	decodePresence(t, resp)

	// Same logout re-reported: idempotent.
	resp = postJSON(t, ts, "/v1/registry/servers/game-1001/presence", logout)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("duplicate logout: expected 200, got %d", resp.StatusCode)
	}
	pr = decodePresence(t, resp)
	if len(pr.Results) != 1 || pr.Results[0].Status != model.PresenceAlreadyApplied {
		t.Fatalf("duplicate logout = %+v, want already_applied", pr.Results)
	}
}

// TestPresenceInvalidTransition pins the rejection paths: closing an unknown
// session, opening a second session while one is open, and opening a session
// for a character that is not in the index.
func TestPresenceInvalidTransition(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "game-1001")
	const (
		acct = 2001003
		chID = 9001003
	)
	seedChar(t, ts, acct, chID, "game-1001")

	// Logout of an unknown session: rejected.
	resp := postJSON(t, ts, "/v1/registry/servers/game-1001/presence",
		model.PresenceReport{Events: []model.PresenceEvent{logoutEvent("ghost-session", model.SessionEndNormal)}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout unknown: expected 200, got %d", resp.StatusCode)
	}
	pr := decodePresence(t, resp)
	if len(pr.Results) != 1 || pr.Results[0].Status != model.PresenceRejected {
		t.Fatalf("logout unknown = %+v, want rejected", pr.Results)
	}

	// Open a session, then try to open a different one for the same character.
	resp = postJSON(t, ts, "/v1/registry/servers/game-1001/presence",
		model.PresenceReport{Events: []model.PresenceEvent{loginEvent("t-1", acct, chID)}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first open: expected 200, got %d", resp.StatusCode)
	}
	decodePresence(t, resp)
	resp = postJSON(t, ts, "/v1/registry/servers/game-1001/presence",
		model.PresenceReport{Events: []model.PresenceEvent{loginEvent("t-2", acct, chID)}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second open: expected 200, got %d", resp.StatusCode)
	}
	pr = decodePresence(t, resp)
	if len(pr.Results) != 1 || pr.Results[0].Status != model.PresenceRejected {
		t.Fatalf("second open = %+v, want rejected", pr.Results)
	}
	if pr.Results[0].Detail == "" {
		t.Errorf("second open: want a detail explaining the conflict")
	}

	// Login for a character that is not in the index: rejected.
	resp = postJSON(t, ts, "/v1/registry/servers/game-1001/presence",
		model.PresenceReport{Events: []model.PresenceEvent{loginEvent("t-3", acct, 999999999)}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login missing char: expected 200, got %d", resp.StatusCode)
	}
	pr = decodePresence(t, resp)
	if len(pr.Results) != 1 || pr.Results[0].Status != model.PresenceRejected {
		t.Fatalf("login missing char = %+v, want rejected", pr.Results)
	}
}

// TestPresenceMalformedBody rejects a structurally bad batch with 400 before
// any event applies.
func TestPresenceMalformedBody(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "game-1001")

	cases := []map[string]any{
		{"events": []map[string]any{{"kind": "login", "character_id": 1}}},                                                             // no account_id
		{"events": []map[string]any{{"kind": "login", "account_id": 1}}},                                                               // no character_id
		{"events": []map[string]any{{"kind": "logout", "session_id": "s", "end_reason": "normal"}}},                                    // no ended_at
		{"events": []map[string]any{{"kind": "logout", "session_id": "s", "end_reason": "bogus", "ended_at": "2026-01-01T00:00:00Z"}}}, // bad reason
		{"events": []map[string]any{{"kind": "teleport", "session_id": "s"}}},                                                          // unknown kind
		{"events": []map[string]any{{"kind": "login", "account_id": 1, "character_id": 1, "session_id": ""}}},                          // empty session id
	}
	for i, body := range cases {
		resp := postJSON(t, ts, "/v1/registry/servers/game-1001/presence", body)
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("case %d: expected 400, got %d", i, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

// TestPresenceServerNotFound: reporting for an unregistered server is 404.
func TestPresenceServerNotFound(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	resp := postJSON(t, ts, "/v1/registry/servers/ghost/presence",
		model.PresenceReport{Events: []model.PresenceEvent{loginEvent("s", 1, 1)}})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

// postJSONAuth posts with a Bearer token (registry auth-zone tests).
func postJSONAuth(t *testing.T, ts *httptest.Server, path string, body any, token string) *http.Response {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL+path, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

// TestPresenceUnauthenticated pins the auth zone: the presence endpoint rides
// the registry listener, so without a valid service token it is rejected and
// no event applies.
func TestPresenceUnauthenticated(t *testing.T) {
	mem := memory.New()
	defer mem.Close()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	regSvc := registry.New(mem, mem, logger).WithSessions(mem)
	discSvc := discovery.New(mem, mem)
	dirSvc := directory.New(mem)
	events := httpadapter.New()
	rtSvc := routing.New(mem, mem, mem, mem)
	crossSvc := crossserver.New(mem, mem, events, logger)
	handler := New(regSvc, discSvc, dirSvc, nil, rtSvc, crossSvc, mem, events, logger)

	regMux := http.NewServeMux()
	handler.RegisterRegistryRoutes(regMux)
	ts := httptest.NewServer(RegistryAuth(RegistryAuthConfig{
		Tokens: map[string]struct{}{"secret-token": {}},
		Logger: logger,
	})(regMux))
	defer ts.Close()

	login := model.PresenceReport{Events: []model.PresenceEvent{loginEvent("auth-1", 2001999, 9001999)}}

	// No token.
	resp := postJSONAuth(t, ts, "/v1/registry/servers/game-1001/presence", login, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: expected 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Wrong token.
	resp = postJSONAuth(t, ts, "/v1/registry/servers/game-1001/presence", login, "wrong")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: expected 401, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Valid token: the request reaches the handler. Register the server
	// first so the trust anchor (GetServer) passes; the character is not in
	// the index, so the login is classified rejected — proving the auth gate
	// passed and the store path executed.
	regBody := map[string]any{
		"server_id": "game-1001", "name": "Test Server", "type": "game",
		"region": "cn-east", "version": "1.0.0", "platform": "android",
		"endpoint": map[string]any{"host": "10.0.0.1", "port": 30001},
	}
	if resp := postJSONAuth(t, ts, "/v1/registry/servers/register", regBody, "secret-token"); resp.StatusCode != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", resp.StatusCode)
	}
	resp = postJSONAuth(t, ts, "/v1/registry/servers/game-1001/presence", login, "secret-token")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("valid token: expected 200, got %d", resp.StatusCode)
	}
	pr := decodePresence(t, resp)
	if len(pr.Results) != 1 || pr.Results[0].Status != model.PresenceRejected {
		t.Fatalf("valid token (unseeded char) = %+v, want rejected for missing char", pr.Results)
	}
}
