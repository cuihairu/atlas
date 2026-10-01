// Token-bucket rate limiting middleware for Atlas listeners (TODO v0.1.17).
// Rules match by longest path prefix and bucket per client IP, so one noisy
// game server cannot starve the others. Disabled unless configured via
// ATLAS_RATE_LIMITS / ATLAS_RATE_LIMIT_DEFAULT (see ParseRateLimitRules).
package httpapi

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// RateRule is one token-bucket rule: refill at rps tokens per second, hold
// at most burst tokens.
type RateRule struct {
	RPS   float64
	Burst int
}

type rateBucket struct {
	tokens float64
	last   time.Time
}

// RateLimiter enforces per-endpoint, per-client-IP token buckets.
type RateLimiter struct {
	mu      sync.Mutex
	rules   []rateNamedRule // longest prefix first
	def     RateRule
	buckets map[string]*rateBucket
	// maxBuckets bounds memory against spoofed client IPs (XFF): new
	// clients are denied once the cap is reached (fail closed).
	maxBuckets int
	now        func() time.Time
}

type rateNamedRule struct {
	prefix string
	rule   RateRule
}

// NewRateLimiter builds a limiter from prefix rules plus a fallback default.
func NewRateLimiter(rules map[string]RateRule, def RateRule) *RateLimiter {
	named := make([]rateNamedRule, 0, len(rules))
	for prefix, rule := range rules {
		named = append(named, rateNamedRule{prefix: prefix, rule: rule})
	}
	// Longest prefix wins; sort once at construction.
	for i := 1; i < len(named); i++ {
		for j := i; j > 0 && len(named[j].prefix) > len(named[j-1].prefix); j-- {
			named[j], named[j-1] = named[j-1], named[j]
		}
	}
	if def.Burst <= 0 {
		def.Burst = int(math.Max(1, def.RPS))
	}
	return &RateLimiter{
		rules:      named,
		def:        def,
		buckets:    make(map[string]*rateBucket),
		maxBuckets: 65536,
		now:        time.Now,
	}
}

// Middleware returns the rate-limiting handler wrapper.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(r.URL.Path, extractIP(r)) {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED",
				"too many requests for this endpoint; retry later")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// allow consumes one token for (path, clientIP), refilling the bucket by
// elapsed time. Buckets are lazy: idle clients cost nothing but a map entry.
func (rl *RateLimiter) allow(path, clientIP string) bool {
	rule := rl.def
	for _, nr := range rl.rules {
		if strings.HasPrefix(path, nr.prefix) {
			rule = nr.rule
			break
		}
	}
	if rule.RPS <= 0 {
		return true // rule explicitly disables limiting
	}

	key := clientIP + "\x00" + pathPrefixKey(path, rl.rules, rl.def)
	now := rl.now()

	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[key]
	if !ok {
		if len(rl.buckets) >= rl.maxBuckets {
			// Memory guard: too many distinct clients (likely spoofed
			// XFF). Deny the new ones rather than grow without bound.
			return false
		}
		b = &rateBucket{tokens: float64(rule.Burst), last: now}
		rl.buckets[key] = b
	}

	// Refill by elapsed time, capped at burst.
	elapsed := now.Sub(b.last).Seconds()
	b.last = now
	b.tokens = math.Min(float64(rule.Burst), b.tokens+elapsed*rule.RPS)

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// pathPrefixKey resolves the matched rule for the bucket key so that
// different endpoints under the same client keep separate buckets even when
// the full path is unique per resource.
func pathPrefixKey(path string, rules []rateNamedRule, def RateRule) string {
	for _, nr := range rules {
		if strings.HasPrefix(path, nr.prefix) {
			return nr.prefix
		}
	}
	return "\x01default"
}

// ParseRateLimitRules parses "prefix=rps:burst" pairs (semicolon-separated)
// into a rule map, e.g.
//
//	"/v1/registry/servers/register=10:20;/v1/registry/=200:400"
//
// A missing ":burst" defaults the burst to the integer RPS.
func ParseRateLimitRules(raw string) (map[string]RateRule, error) {
	rules := make(map[string]RateRule)
	for _, part := range strings.Split(raw, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		prefix, spec, found := strings.Cut(part, "=")
		prefix = strings.TrimSpace(prefix)
		spec = strings.TrimSpace(spec)
		if !found || prefix == "" || spec == "" {
			return nil, fmt.Errorf("rate limit rule %q: expected prefix=rps[:burst]", part)
		}
		rpsStr, burstStr, hasBurst := strings.Cut(spec, ":")
		rps, err := strconv.ParseFloat(strings.TrimSpace(rpsStr), 64)
		if err != nil || rps < 0 {
			return nil, err
		}
		burst := int(math.Max(1, rps))
		if hasBurst {
			burst, err = strconv.Atoi(strings.TrimSpace(burstStr))
			if err != nil || burst <= 0 {
				return nil, err
			}
		}
		rules[prefix] = RateRule{RPS: rps, Burst: burst}
	}
	return rules, nil
}

// ParseRateDefault parses the default "rps:burst" rule (burst optional).
func ParseRateDefault(raw string) (RateRule, error) {
	rules, err := ParseRateLimitRules("default=" + raw)
	if err != nil {
		return RateRule{}, err
	}
	return rules["default"], nil
}
