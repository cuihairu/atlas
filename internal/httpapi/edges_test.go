// Edge coverage for the config parsers and the rate limiter's guards:
// empty list parts, malformed rule specs, the stats counters after a
// rejection, the bucket memory cap, header-based client IPs, and CIDR
// allowlist parsing.
package httpapi

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimit_ParseRulesSkipsEmptyParts(t *testing.T) {
	rules, err := ParseRateLimitRules("/a=1;; ;")
	if err != nil {
		t.Fatalf("ParseRateLimitRules: %v", err)
	}
	if len(rules) != 1 || rules["/a"].RPS != 1 {
		t.Errorf("rules = %+v, want the single /a rule", rules)
	}
	for _, bad := range []string{"/a=5:0", "/a=5:x"} {
		if _, err := ParseRateLimitRules(bad); err == nil {
			t.Errorf("ParseRateLimitRules(%q) accepted a bad burst", bad)
		}
	}
}

func TestRateLimit_StatsCountRejections(t *testing.T) {
	rl := NewRateLimiter(map[string]RateRule{
		"/v1/discovery/": {RPS: 1, Burst: 1},
	}, RateRule{RPS: 1000, Burst: 1000})
	now := time.Now()
	rl.now = func() time.Time { return now }

	h := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	hit := func() int {
		r := httptest.NewRequest("GET", "/v1/discovery/servers", nil)
		r.RemoteAddr = "10.7.7.7:9"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec.Code
	}
	if code := hit(); code != 200 {
		t.Fatalf("first request: %d", code)
	}
	if code := hit(); code != http.StatusTooManyRequests {
		t.Fatalf("second request: %d, want 429", code)
	}

	st := rl.Stats()
	if st.RejectedByEndpoint["/v1/discovery/"] != 1 {
		t.Errorf("rejected_by_endpoint = %v, want 1 under /v1/discovery/", st.RejectedByEndpoint)
	}
	if st.RejectedByClient["10.7.7.7"] != 1 {
		t.Errorf("rejected_by_client = %v, want 1 for 10.7.7.7", st.RejectedByClient)
	}
}

func TestRateLimit_BucketCapRejectsNewClients(t *testing.T) {
	rl := NewRateLimiter(nil, RateRule{RPS: 100, Burst: 1})
	rl.maxBuckets = 1 // shrink the spoofed-XFF memory guard
	if !rl.allow("/x", "10.1.1.1") {
		t.Fatal("first client must be allowed")
	}
	if rl.allow("/x", "10.1.1.2") {
		t.Fatal("second client must be rejected at the bucket cap")
	}
}

func TestParseAdminRolesSkipsEmptyParts(t *testing.T) {
	roles, err := ParseAdminRoles("k1:admin,, k2:viewer")
	if err != nil {
		t.Fatalf("ParseAdminRoles: %v", err)
	}
	if len(roles) != 2 || roles["k1"] != RoleAdmin || roles["k2"] != RoleViewer {
		t.Errorf("roles = %v, want k1/k2 only", roles)
	}
}

func TestExtractIPPrefersRealIPHeader(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Real-IP", " 203.0.113.9 ")
	if got := extractIP(r); got != "203.0.113.9" {
		t.Errorf("extractIP = %q, want the X-Real-IP value", got)
	}
}

func TestIPAllowedRejectsGarbageIP(t *testing.T) {
	_, cidr, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	if ipAllowed("not-an-ip", []*net.IPNet{cidr}) {
		t.Error("an unparseable IP must never match the allowlist")
	}
}

func TestParseIPWhitelistRejectsBadCIDR(t *testing.T) {
	if _, err := ParseIPWhitelist("10.0.0.0/8,300.300.300.300/8"); err == nil {
		t.Error("expected error for a malformed CIDR entry")
	}
}
