package httpapi

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// AuthConfig controls access to Admin endpoints.
type AuthConfig struct {
	// APIKeys is the set of valid API keys. Clients must send one in the
	// Authorization header: "Bearer <key>".
	APIKeys map[string]struct{}
	// IPWhitelist, when non-empty, restricts Admin access to these CIDR
	// ranges. An empty list means "allow all".
	IPWhitelist []*net.IPNet
	// Logger for auth failures.
	Logger *slog.Logger
}

// AdminAuth returns middleware that enforces API key + IP whitelist on all
// /v1/admin/* routes.
//
// Checks run in order: IP whitelist first (cheap), then API key (constant-time
// comparison). Both must pass.
func AdminAuth(cfg AuthConfig) func(http.Handler) http.Handler {
	hasKeys := len(cfg.APIKeys) > 0
	hasIPs := len(cfg.IPWhitelist) > 0

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// ── IP whitelist ──────────────────────────────────
			if hasIPs {
				clientIP := extractIP(r)
				if !ipAllowed(clientIP, cfg.IPWhitelist) {
					cfg.Logger.Warn("admin: ip blocked",
						"ip", clientIP,
						"path", r.URL.Path,
					)
					writeError(w, http.StatusForbidden, "IP_NOT_ALLOWED",
						"your IP is not in the admin whitelist")
					return
				}
			}

			// ── API key ───────────────────────────────────────
			if hasKeys {
				key := extractBearerToken(r)
				if key == "" {
					writeError(w, http.StatusUnauthorized, "MISSING_API_KEY",
						"Authorization: Bearer <key> header is required")
					return
				}
				if _, ok := cfg.APIKeys[key]; !ok {
					cfg.Logger.Warn("admin: invalid api key",
						"path", r.URL.Path,
					)
					writeError(w, http.StatusUnauthorized, "INVALID_API_KEY",
						"the provided API key is not valid")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractIP parses the client IP from the request, respecting X-Forwarded-For
// and X-Real-IP when behind a reverse proxy.
func extractIP(r *http.Request) string {
	// X-Forwarded-For: client, proxy1, proxy2
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

// ipAllowed checks whether ip falls within any of the allowed CIDR ranges.
func ipAllowed(ipStr string, allowed []*net.IPNet) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, cidr := range allowed {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// extractBearerToken pulls the token from "Authorization: Bearer <token>".
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return ""
	}
	return strings.TrimPrefix(auth, "Bearer ")
}

// ParseIPWhitelist parses a comma-separated list of CIDR strings into
// []*net.IPNet. Individual IPs (e.g. "10.0.1.5") are treated as /32 (IPv4)
// or /128 (IPv6).
func ParseIPWhitelist(raw string) ([]*net.IPNet, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var nets []*net.IPNet
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// If no slash, treat as single IP.
		if !strings.Contains(part, "/") {
			ip := net.ParseIP(part)
			if ip == nil {
				continue
			}
			if ip.To4() != nil {
				part += "/32"
			} else {
				part += "/128"
			}
		}
		_, cidr, err := net.ParseCIDR(part)
		if err != nil {
			return nil, err
		}
		nets = append(nets, cidr)
	}
	return nets, nil
}

// ParseAPIKeys parses a comma-separated list of API keys into a set.
func ParseAPIKeys(raw string) map[string]struct{} {
	keys := make(map[string]struct{})
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			keys[part] = struct{}{}
		}
	}
	return keys
}