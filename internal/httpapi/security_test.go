package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// RBAC (TODO v0.1.17)
// ---------------------------------------------------------------------------

func rbacHandler(t *testing.T, roles map[string]string) http.Handler {
	t.Helper()
	cfg := AuthConfig{
		APIKeys: map[string]struct{}{
			"admin-key":  {},
			"op-key":     {},
			"viewer-key": {},
			"legacy-key": {},
			"unlisted-k": {},
		},
		Roles:  roles,
		Logger: slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError})),
	}
	return AdminAuth(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

func TestRBAC_Roles(t *testing.T) {
	roles := map[string]string{
		"admin-key":  RoleAdmin,
		"op-key":     RoleOperator,
		"viewer-key": RoleViewer,
		"unlisted-k": RoleOperator, // listed in APIKeys, no role entry → admin
		// legacy-key absent from Roles → admin (back-compat)
	}
	h := rbacHandler(t, roles)

	tests := []struct {
		name     string
		method   string
		key      string
		wantCode int
	}{
		{"viewer GET allowed", "GET", "viewer-key", 200},
		{"viewer HEAD allowed", "HEAD", "viewer-key", 200},
		{"viewer POST blocked", "POST", "viewer-key", 403},
		{"viewer DELETE blocked", "DELETE", "viewer-key", 403},
		{"operator GET allowed", "GET", "op-key", 200},
		{"operator POST allowed", "POST", "op-key", 200},
		{"admin POST allowed", "POST", "admin-key", 200},
		{"legacy key POST allowed", "POST", "legacy-key", 200},
		{"unlisted role key POST allowed", "POST", "unlisted-k", 200},
		{"invalid key rejected", "GET", "wrong-key", 401},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/v1/admin/stats", nil)
			req.Header.Set("Authorization", "Bearer "+tt.key)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.wantCode {
				t.Errorf("got %d, want %d", rec.Code, tt.wantCode)
			}
			if tt.wantCode == 403 && tt.method == "POST" && rec.Code == 403 {
				var body struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				_ = json.Unmarshal(rec.Body.Bytes(), &body)
				if body.Error.Code != "ROLE_NOT_ALLOWED" {
					t.Errorf("expected ROLE_NOT_ALLOWED, got %q", body.Error.Code)
				}
			}
		})
	}
}

func TestRBAC_ActorInContext(t *testing.T) {
	var got Actor
	h := AdminAuth(rbacRoleConfig(t))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = ActorFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1/admin/stats", nil)
	req.Header.Set("Authorization", "Bearer viewer-key")
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got.Role != RoleViewer {
		t.Errorf("expected viewer role, got %q", got.Role)
	}
	if len(got.KeyFingerprint) != 12 {
		t.Errorf("expected 12-char fingerprint, got %q", got.KeyFingerprint)
	}
	// Must never echo the raw key.
	if strings.Contains(got.String(), "viewer-key") {
		t.Errorf("actor must not contain the raw key: %q", got.String())
	}
}

func rbacRoleConfig(t *testing.T) AuthConfig {
	t.Helper()
	return AuthConfig{
		APIKeys: map[string]struct{}{"viewer-key": {}},
		Roles:   map[string]string{"viewer-key": RoleViewer},
		Logger:  slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError})),
	}
}

func TestParseAdminRoles(t *testing.T) {
	roles, err := ParseAdminRoles("k1:admin, k2:operator ,k3:Viewer,k4")
	if err != nil {
		t.Fatalf("ParseAdminRoles: %v", err)
	}
	want := map[string]string{
		"k1": RoleAdmin,
		"k2": RoleOperator,
		"k3": RoleViewer,
		"k4": RoleAdmin, // no colon → admin
	}
	if len(roles) != len(want) {
		t.Fatalf("expected %d roles, got %d", len(want), len(roles))
	}
	for k, v := range want {
		if roles[k] != v {
			t.Errorf("role %s: got %q, want %q", k, roles[k], v)
		}
	}

	if _, err := ParseAdminRoles("k1:superuser"); err == nil {
		t.Error("expected error for unknown role")
	}
	// Entries without a colon are legal keys defaulting to admin.
	if roles, err := ParseAdminRoles("plainkey"); err != nil || roles["plainkey"] != RoleAdmin {
		t.Errorf("plain key should default to admin: %v %v", roles, err)
	}
}

// ---------------------------------------------------------------------------
// Audit log (TODO v0.1.17)
// ---------------------------------------------------------------------------

func auditTestServer(t *testing.T) (*httptest.Server, *AuditLog) {
	t.Helper()
	audit := NewAuditLog(10, 64, slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError})))

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/admin/servers/{id}/drain", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /v1/admin/stats", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /v1/admin/audit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"entries": audit.Recent(50)})
	})

	// Chain mirrors main.go: audit inside, auth outside.
	chain := AdminAuth(AuthConfig{
		APIKeys: map[string]struct{}{"admin-key": {}},
		Roles:   map[string]string{"admin-key": RoleOperator},
		Logger:  slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError})),
	})(audit.Middleware(mux))

	return httptest.NewServer(chain), audit
}

func TestAudit_RecordsMutationsWithActorAndBody(t *testing.T) {
	ts, audit := auditTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/v1/admin/servers/game-1001/drain", bytes.NewBufferString(`{"reason":"patch"}`))
	req.Header.Set("Authorization", "Bearer admin-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	entries := audit.Recent(10)
	if len(entries) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(entries))
	}
	e := entries[0]
	if e.Method != "POST" || e.Path != "/v1/admin/servers/game-1001/drain" {
		t.Errorf("unexpected method/path: %s %s", e.Method, e.Path)
	}
	if e.Status != 200 {
		t.Errorf("expected status 200, got %d", e.Status)
	}
	if !strings.HasPrefix(e.Actor, "operator:") {
		t.Errorf("expected operator actor, got %q", e.Actor)
	}
	if e.Time.IsZero() {
		t.Error("expected timestamp")
	}
	if !bytes.Contains(e.Body, []byte(`"reason"`)) {
		t.Errorf("expected request body captured, got %s", e.Body)
	}
}

func TestAudit_RecordsReadsWithoutBody(t *testing.T) {
	ts, audit := auditTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/v1/admin/stats", nil)
	req.Header.Set("Authorization", "Bearer admin-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()

	entries := audit.Recent(10)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].Method != "GET" || len(entries[0].Body) != 0 {
		t.Errorf("read must be recorded without body: %+v", entries[0])
	}
}

func TestAudit_RingCapsAndServes(t *testing.T) {
	ts, audit := auditTestServer(t)
	defer ts.Close()

	// Push 15 entries into a ring capped at 10.
	for i := 0; i < 15; i++ {
		req, _ := http.NewRequest("GET", ts.URL+"/v1/admin/stats", nil)
		req.Header.Set("Authorization", "Bearer admin-key")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET %d: %v", i, err)
		}
		resp.Body.Close()
	}
	if n := len(audit.Recent(100)); n != 10 {
		t.Errorf("expected ring capped at 10, got %d", n)
	}

	// Endpoint serves the ring.
	resp, err := http.Get(ts.URL + "/v1/admin/audit") // no auth header → 401
	if err != nil {
		t.Fatalf("audit GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401 without key, got %d", resp.StatusCode)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/v1/admin/audit", nil)
	req.Header.Set("Authorization", "Bearer admin-key")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("audit GET: %v", err)
	}
	defer resp.Body.Close()
	var body struct {
		Entries []AuditEntry `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Entries) == 0 {
		t.Error("expected entries in response")
	}
	// The audit endpoint itself must not be recorded before it responds —
	// entries so far are the 15 pushes only (or fewer after cap).
	if len(body.Entries) > 10 {
		t.Errorf("expected at most 10 entries, got %d", len(body.Entries))
	}
}

func TestAudit_InvalidJSONBodyIsWrapped(t *testing.T) {
	audit := NewAuditLog(5, 64, slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError})))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /x", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(400) })
	h := audit.Middleware(mux)

	req := httptest.NewRequest("POST", "/x", bytes.NewBufferString("not-json{{"))
	h.ServeHTTP(httptest.NewRecorder(), req)

	entries := audit.Recent(1)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	// Entry must marshal despite invalid payload.
	if _, err := json.Marshal(entries[0]); err != nil {
		t.Fatalf("entry must marshal: %v", err)
	}
	if !json.Valid(entries[0].Body) || !bytes.Contains(entries[0].Body, []byte("not-json")) {
		t.Errorf("expected wrapped body, got %s", entries[0].Body)
	}
}

// ---------------------------------------------------------------------------
// Rate limiting (TODO v0.1.17)
// ---------------------------------------------------------------------------

func TestRateLimit_TokenBucketPerClient(t *testing.T) {
	rl := NewRateLimiter(map[string]RateRule{
		"/v1/registry/servers/register": {RPS: 0.001, Burst: 3}, // effectively static bucket
	}, RateRule{RPS: 1000, Burst: 1000})

	now := time.Now()
	rl.now = func() time.Time { return now }

	h := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := func(ip string) int {
		r := httptest.NewRequest("POST", "/v1/registry/servers/register", nil)
		r.RemoteAddr = ip + ":1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}

	// 3 tokens then 429 for this client.
	if code := req("10.0.0.1"); code != 200 {
		t.Fatalf("req1: %d", code)
	}
	if code := req("10.0.0.1"); code != 200 {
		t.Fatalf("req2: %d", code)
	}
	if code := req("10.0.0.1"); code != 200 {
		t.Fatalf("req3: %d", code)
	}
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/registry/servers/register", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after burst, got %d", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header")
	}

	// Different client IP has its own bucket: unaffected.
	if code := req("10.0.0.2"); code != 200 {
		t.Errorf("other client must be unaffected, got %d", code)
	}
}

func TestRateLimit_RefillsOverTime(t *testing.T) {
	rl := NewRateLimiter(nil, RateRule{RPS: 1, Burst: 1})
	now := time.Now()
	rl.now = func() time.Time { return now }

	h := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := func() int {
		r := httptest.NewRequest("GET", "/v1/discovery/servers", nil)
		r.RemoteAddr = "10.0.0.9:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}

	if code := req(); code != 200 {
		t.Fatalf("first: %d", code)
	}
	if code := req(); code != 429 {
		t.Fatalf("second (empty bucket): %d", code)
	}

	now = now.Add(1100 * time.Millisecond) // one token at 1 rps
	if code := req(); code != 200 {
		t.Errorf("after refill: %d", code)
	}
}

func TestRateLimit_LongestPrefixWins(t *testing.T) {
	rl := NewRateLimiter(map[string]RateRule{
		"/v1/registry/":                 {RPS: 1000, Burst: 1000},
		"/v1/registry/servers/register": {RPS: 0.001, Burst: 1}, // strict
	}, RateRule{RPS: 1000, Burst: 1000})

	h := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// register takes the strict rule (1 token), then 429.
	post := func() int {
		r := httptest.NewRequest("POST", "/v1/registry/servers/register", nil)
		r.RemoteAddr = "10.0.0.3:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	if code := post(); code != 200 {
		t.Fatalf("first register: %d", code)
	}
	if code := post(); code != 429 {
		t.Fatalf("second register should hit strict rule: %d", code)
	}

	// Heartbeat under the loose prefix still allowed (separate rule/bucket).
	hb := httptest.NewRequest("POST", "/v1/registry/servers/game-1/heartbeat", nil)
	hb.RemoteAddr = "10.0.0.3:1234"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, hb)
	if rec.Code != 200 {
		t.Errorf("heartbeat should use loose rule, got %d", rec.Code)
	}
}

func TestRateLimit_ParseRules(t *testing.T) {
	rules, err := ParseRateLimitRules("/a=10:20;/b=5")
	if err != nil {
		t.Fatalf("ParseRateLimitRules: %v", err)
	}
	if rules["/a"].RPS != 10 || rules["/a"].Burst != 20 {
		t.Errorf("unexpected /a rule: %+v", rules["/a"])
	}
	if rules["/b"].RPS != 5 || rules["/b"].Burst != 5 {
		t.Errorf("unexpected /b rule (default burst): %+v", rules["/b"])
	}

	if _, err := ParseRateLimitRules("missing-equals"); err == nil {
		t.Error("expected error for malformed rule")
	}
	if _, err := ParseRateLimitRules("/x=abc"); err == nil {
		t.Error("expected error for non-numeric rps")
	}

	def, err := ParseRateDefault("100:200")
	if err != nil {
		t.Fatalf("ParseRateDefault: %v", err)
	}
	if def.RPS != 100 || def.Burst != 200 {
		t.Errorf("unexpected default: %+v", def)
	}
}

func TestRateLimit_DisabledRuleAllowsAll(t *testing.T) {
	// rps 0 disables the rule (ParseRateLimitRules rejects such specs, so
	// construct the rule directly).
	rl := NewRateLimiter(map[string]RateRule{
		"/x": {RPS: 0, Burst: 0},
	}, RateRule{RPS: 1, Burst: 1})

	h := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for i := 0; i < 5; i++ {
		r := httptest.NewRequest("GET", "/x/anything", nil)
		r.RemoteAddr = "10.0.0.4:1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != 200 {
			t.Fatalf("disabled rule must allow all, got %d on request %d", rec.Code, i)
		}
	}
}
