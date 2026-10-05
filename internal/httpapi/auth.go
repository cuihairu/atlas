package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// Admin roles (TODO v0.1.17), from most to least privileged.
const (
	// RoleAdmin has full access to every Admin endpoint.
	RoleAdmin = "admin"
	// RoleOperator may read everything and perform lifecycle mutations
	// (server state, migrations, realms/shards).
	RoleOperator = "operator"
	// RoleViewer may read Admin endpoints but never mutate.
	RoleViewer = "viewer"
)

type ctxKey int

const actorKey ctxKey = 1

// Actor identifies the authenticated caller for audit purposes.
type Actor struct {
	// Role is one of admin/operator/viewer ("admin" when RBAC is disabled).
	Role string
	// KeyFingerprint is the first 12 hex chars of the API key's SHA-256 —
	// enough to correlate audit entries, never reversible to the key.
	KeyFingerprint string
	// CertFingerprint is the first 12 hex chars of the client certificate's
	// SHA-256 when mTLS is used. Empty if no client cert presented.
	CertFingerprint string
}

// String renders the actor for logs: "role:fingerprint[:certfp]".
func (a Actor) String() string {
	if a.KeyFingerprint == "" && a.CertFingerprint == "" {
		return a.Role + ":anonymous"
	}
	if a.CertFingerprint == "" {
		return a.Role + ":" + a.KeyFingerprint
	}
	return a.Role + ":" + a.KeyFingerprint + ":" + a.CertFingerprint
}

// CertFingerprintFrom returns the client certificate fingerprint from the
// request context, or empty string if no client cert was presented.
func CertFingerprintFrom(ctx context.Context) string {
	a, _ := ctx.Value(actorKey).(Actor)
	return a.CertFingerprint
}

// ActorFrom returns the authenticated actor, or the zero actor when the
// request never passed through AdminAuth (e.g. RBAC disabled).
func ActorFrom(ctx context.Context) Actor {
	a, _ := ctx.Value(actorKey).(Actor)
	return a
}

// WithActor stores a resolved actor in the context. The gRPC auth
// interceptor uses it to hand the actor to the audit layer, mirroring how
// AdminAuth injects it for the REST middleware chain.
func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, actorKey, a)
}

// AuthConfig controls access to Admin endpoints.
type AuthConfig struct {
	// APIKeys is the set of valid API keys. Clients must send one in the
	// Authorization header: "Bearer <key>".
	APIKeys map[string]struct{}
	// Roles maps API key → role (admin/operator/viewer). Keys absent from
	// the map — and every key when Roles is nil — act as admin (backwards
	// compatible with deployments that only configure APIKeys).
	Roles map[string]string
	// IPWhitelist, when non-empty, restricts Admin access to these CIDR
	// ranges. An empty list means "allow all".
	IPWhitelist []*net.IPNet
	// Logger for auth failures.
	Logger *slog.Logger
}

// AdminAuth returns middleware that enforces API key + IP whitelist + RBAC on
// all /v1/admin/* routes.
//
// Checks run in order: IP whitelist first (cheap), then API key
// (constant-time comparison), then role. The resolved Actor is stored in the
// request context for downstream audit logging.
func AdminAuth(cfg AuthConfig) func(http.Handler) http.Handler {
	hasKeys := len(cfg.APIKeys) > 0
	hasIPs := len(cfg.IPWhitelist) > 0
	hasRoles := len(cfg.Roles) > 0

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

			// ── API key + RBAC ────────────────────────────────
			actor := Actor{Role: RoleAdmin}
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
				sum := sha256.Sum256([]byte(key))
				actor.KeyFingerprint = hex.EncodeToString(sum[:])[:12]
				if hasRoles {
					if role, ok := cfg.Roles[key]; ok {
						actor.Role = role
					}
				}
			}

			// ── mTLS client cert fingerprint (for audit correlation) ───
			actor.CertFingerprint = extractCertFingerprint(r)

			// ── Role check ────────────────────────────────────
			if actor.Role == RoleViewer && r.Method != http.MethodGet && r.Method != http.MethodHead {
				cfg.Logger.Warn("admin: role not allowed",
					"actor", actor.String(),
					"method", r.Method,
					"path", r.URL.Path,
				)
				writeError(w, http.StatusForbidden, "ROLE_NOT_ALLOWED",
					"role "+actor.Role+" may only read Admin endpoints")
				return
			}

			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey, actor)))
		})
	}
}

// ParseAdminRoles parses "key:role" pairs (comma-separated) into a key →
// role map. Unknown roles are rejected; entries without a colon default to
// admin (equivalent to listing the key in ATLAS_ADMIN_API_KEYS).
func ParseAdminRoles(raw string) (map[string]string, error) {
	roles := make(map[string]string)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, role, found := strings.Cut(part, ":")
		key = strings.TrimSpace(key)
		role = strings.TrimSpace(strings.ToLower(role))
		if !found || role == "" {
			role = RoleAdmin
		}
		switch role {
		case RoleAdmin, RoleOperator, RoleViewer:
		default:
			return nil, fmt.Errorf("unknown role %q (expected admin, operator or viewer)", role)
		}
		if key != "" {
			roles[key] = role
		}
	}
	return roles, nil
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

// extractCertFingerprint returns the first 12 hex chars of the peer
// certificate's SHA-256, or empty string if no client cert presented.
func extractCertFingerprint(r *http.Request) string {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return ""
	}
	sum := sha256.Sum256(r.TLS.PeerCertificates[0].Raw)
	return hex.EncodeToString(sum[:])[:12]
}
