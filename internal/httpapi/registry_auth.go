package httpapi

import (
	"log/slog"
	"net"
	"net/http"
)

// RegistryAuthConfig controls access to Registry endpoints (server
// registration, heartbeat, unregister).
type RegistryAuthConfig struct {
	// Tokens is the set of valid service tokens. Game servers must present one
	// in the Authorization header: "Bearer <token>" or X-Atlas-Token header.
	Tokens map[string]struct{}
	// IPWhitelist, when non-empty, restricts Registry access to these CIDR
	// ranges. Empty means "allow all".
	IPWhitelist []*net.IPNet
	Logger      *slog.Logger
}

// RegistryAuth returns middleware that enforces service token + optional IP
// whitelist on /v1/registry/* routes.
//
// This is a separate trust zone from Admin: game servers run on internal
// networks and authenticate with a shared service token, not an admin API key.
func RegistryAuth(cfg RegistryAuthConfig) func(http.Handler) http.Handler {
	hasTokens := len(cfg.Tokens) > 0
	hasIPs := len(cfg.IPWhitelist) > 0

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// ── IP whitelist ──────────────────────────────────
			if hasIPs {
				clientIP := extractIP(r)
				if !ipAllowed(clientIP, cfg.IPWhitelist) {
					cfg.Logger.Warn("registry: ip blocked",
						"ip", clientIP,
						"path", r.URL.Path,
					)
					writeError(w, http.StatusForbidden, "IP_NOT_ALLOWED",
						"your IP is not in the registry whitelist")
					return
				}
			}

			// ── Service token ─────────────────────────────────
			if hasTokens {
				token := extractBearerToken(r)
				if token == "" {
					// Also check X-Atlas-Token header for internal services
					// that don't use standard Authorization.
					token = r.Header.Get("X-Atlas-Token")
				}
				if token == "" {
					writeError(w, http.StatusUnauthorized, "MISSING_TOKEN",
						"Authorization: Bearer <token> or X-Atlas-Token header is required")
					return
				}
				if _, ok := cfg.Tokens[token]; !ok {
					cfg.Logger.Warn("registry: invalid token",
						"path", r.URL.Path,
					)
					writeError(w, http.StatusUnauthorized, "INVALID_TOKEN",
						"the provided service token is not valid")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
