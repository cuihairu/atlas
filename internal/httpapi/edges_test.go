// Edge coverage for the config parsers and the rate limiter's guards:
// empty list parts, malformed rule specs, the stats counters after a
// rejection, the bucket memory cap, header-based client IPs, and CIDR
// allowlist parsing.
package httpapi

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	// A negative RPS parses fine but must be rejected with a real error —
	// a silent (nil, nil) would make the caller treat the whole config as
	// valid while allow() lets rule.RPS <= 0 through unthrottled.
	for _, bad := range []string{"/a=-5", "/a=-1:10"} {
		if _, err := ParseRateLimitRules(bad); err == nil {
			t.Errorf("ParseRateLimitRules(%q) accepted a negative rps", bad)
		}
	}
	// strconv.ParseFloat accepts NaN and Inf with err=nil, and NaN compares
	// false for rps < 0 — so those values bypass the negative guard and
	// int(math.Max(1, rps)) overflows to the int64 minimum, producing a
	// pathological rule. Each must be rejected with a real error.
	for _, tc := range []struct {
		name string
		spec string
	}{
		{"NaN", "/a=NaN"},
		{"+Inf", "/a=+Inf"},
		{"-Inf", "/a=-Inf"},
		{"overflow", "/a=1e300"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseRateLimitRules(tc.spec)
			if err == nil {
				t.Errorf("ParseRateLimitRules(%q) accepted a non-finite or overflow rps", tc.spec)
			}
		})
	}
	// The overflow guard must be platform-correct: finite RPS values in
	// [2^31, 2^63) are representable in float64 and pass a MaxInt64-based
	// guard, but overflow int (int=32) on 32-bit platforms (GOARCH=386/arm),
	// yielding a negative burst. On 32-bit they must be rejected; on 64-bit
	// they are legitimate and must convert to a positive burst equal to the
	// integer RPS. This is exactly the regression the MaxInt guard fixes.
	mid32 := []struct {
		name string
		spec string
		want int64
	}{
		{"above_2_31", "/a=3e9", 3000000000},
		{"above_2_31_2", "/a=1e10", 10000000000},
	}
	for _, tc := range mid32 {
		t.Run("mid32_"+tc.name, func(t *testing.T) {
			rules, err := ParseRateLimitRules(tc.spec)
			if strconv.IntSize == 32 {
				if err == nil {
					t.Errorf("ParseRateLimitRules(%q) accepted an rps that overflows int on 32-bit", tc.spec)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRateLimitRules(%q): %v", tc.spec, err)
			}
			if got := int64(rules["/a"].Burst); got != tc.want {
				t.Errorf("burst = %d, want %d", got, tc.want)
			}
		})
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
	if got := extractIP(r, true); got != "203.0.113.9" {
		t.Errorf("extractIP = %q, want the X-Real-IP value", got)
	}
}

// TestExtractIPIgnoresForwardedWhenUntrusted pins the forgery guard: without
// the opt-in, forwarded headers are ignored and the socket address wins.
func TestExtractIPIgnoresForwardedWhenUntrusted(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	if got := extractIP(r, false); got != "10.0.0.1" {
		t.Errorf("extractIP = %q, want the socket address", got)
	}
}

// TestRateLimit_ForwardedHeaderBucketKeys pins the per-client bucket key:
// untrusted (default) keys on the socket address so a spoofed XFF cannot
// dodge the limit; trusted keys on the forwarded address.
func TestRateLimit_ForwardedHeaderBucketKeys(t *testing.T) {
	rl := NewRateLimiter(map[string]RateRule{"/v1/registry/": {RPS: 1, Burst: 1}}, RateRule{RPS: 1, Burst: 1})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := rl.Middleware(next)

	do := func(xff string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/registry/heartbeat", nil)
		req.RemoteAddr = "192.168.1.1:80"
		if xff != "" {
			req.Header.Set("X-Forwarded-For", xff)
		}
		rec := httptest.NewRecorder()
		mw.ServeHTTP(rec, req)
		return rec.Code
	}

	// Same socket, different forwarded headers. Untrusted: same bucket → the
	// second request trips the burst-1 limit (429).
	if do("10.0.2.10") != http.StatusOK || do("10.0.2.11") != http.StatusTooManyRequests {
		t.Errorf("untrusted: expected same-bucket 200 then 429, got %d then %d", 200, 200)
	}

	// Trusted: different forwarded headers land in different buckets.
	trusted := NewRateLimiter(map[string]RateRule{"/v1/registry/": {RPS: 1, Burst: 1}}, RateRule{RPS: 1, Burst: 1}).TrustForwardedFor(true)
	mwt := trusted.Middleware(next)
	doT := func(xff string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/registry/heartbeat", nil)
		req.RemoteAddr = "192.168.1.1:80"
		req.Header.Set("X-Forwarded-For", xff)
		rec := httptest.NewRecorder()
		mwt.ServeHTTP(rec, req)
		return rec.Code
	}
	first, second := doT("10.0.2.10"), doT("10.0.2.11")
	if first != http.StatusOK || second != http.StatusOK {
		t.Errorf("trusted: expected per-header buckets 200 then 200, got %d then %d", first, second)
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
