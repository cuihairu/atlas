// Auth enforcement for the gRPC transport, mirroring the three REST
// security domains (docs/api.md §认证). The Go SDK already attaches the
// right credential per call — "authorization: Bearer …" for Registry and
// Admin methods, nothing for Public — this interceptor is the server side
// that was missing: without it the same Disable / RollbackMigration
// operations that cost an API key + RBAC + IP whitelist over REST :8082
// were free over gRPC :9090.
//
// Config-empty keeps the documented dev-mode semantics: when a domain has
// no tokens/keys/whitelist configured the interceptor lets its calls
// through, exactly like the REST middleware, which main.go only mounts
// when configuration exists. Wire it the same way.
package grpc

import (
	"context"
	"log/slog"
	"net"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	pb "github.com/cuihairu/atlas/api/pb"
)

// Domain prefixes derived from the generated service descriptors, so a
// proto package/service rename updates them automatically instead of
// silently unguarding the transport.
var (
	registryPrefix = "/" + pb.RegistryService_ServiceDesc.ServiceName + "/"
	adminPrefix    = "/" + pb.AdminService_ServiceDesc.ServiceName + "/"
)

// Role names, pinned to httpapi by TestAuthRolesMatchREST: the gRPC side
// must not invent its own vocabulary.
const (
	roleAdmin    = "admin"
	roleViewer   = "viewer"
	roleOperator = "operator"
)

// AuthConfig is the parsed auth material for the gRPC interceptor. main.go
// builds it from the same ATLAS_REGISTRY_TOKENS / ATLAS_ADMIN_API_KEYS /
// ATLAS_ADMIN_ROLES / whitelist values the REST middleware consumes.
type AuthConfig struct {
	// RegistryTokens are valid service tokens for RegistryService RPCs.
	RegistryTokens map[string]struct{}
	// RegistryIPs restricts RegistryService to these CIDRs (empty = all).
	RegistryIPs []*net.IPNet
	// AdminKeys are valid API keys for AdminService RPCs.
	AdminKeys map[string]struct{}
	// AdminRoles maps API key → role (admin/operator/viewer). Keys absent
	// from the map act as admin — same backwards-compat rule as REST.
	AdminRoles map[string]string
	// AdminIPs restricts AdminService to these CIDRs (empty = all).
	AdminIPs []*net.IPNet
	// Logger receives auth failures (default slog.Default()).
	Logger *slog.Logger
}

// authDomain classifies a full method into the REST security domain that
// owns the equivalent operations. Everything else (Discovery / Directory /
// Routing) stays Public: gateway-side auth by design.
func authDomain(fullMethod string) string {
	switch {
	case strings.HasPrefix(fullMethod, registryPrefix):
		return "registry"
	case strings.HasPrefix(fullMethod, adminPrefix):
		return "admin"
	default:
		return "public"
	}
}

// adminReadMethods are the AdminService RPC names the REST side exposes as
// GET-equivalents (viewer may call). Every other Admin RPC mutates and
// requires operator at least — the REST rule "viewer may read, never
// mutate" expressed per method.
var adminReadMethods = map[string]bool{
	"GetStats":         true,
	"GetMigration":     true,
	"ListMigrations":   true,
	"SearchCharacters": true,
}

// UnaryAuth returns the interceptor enforcing cfg on every call. Attach it
// only when configuration exists (mirrors how main.go mounts the REST
// middleware), or call it with a zero config — every domain then passes
// through, which is the documented unconfigured behavior.
func UnaryAuth(cfg AuthConfig) grpc.UnaryServerInterceptor {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		switch authDomain(info.FullMethod) {
		case "registry":
			if err := checkIP(ctx, cfg.RegistryIPs); err != nil {
				cfg.Logger.Warn("grpc: registry ip blocked",
					"ip", peerIP(ctx), "method", info.FullMethod)
				return nil, err
			}
			if len(cfg.RegistryTokens) > 0 {
				tok, ok := credentialFrom(ctx, true)
				if !ok {
					return nil, status.Error(codes.Unauthenticated,
						"MISSING_TOKEN: service token required (authorization bearer or x-atlas-token)")
				}
				if _, ok := cfg.RegistryTokens[tok]; !ok {
					cfg.Logger.Warn("grpc: invalid registry token", "method", info.FullMethod)
					return nil, status.Error(codes.Unauthenticated,
						"INVALID_TOKEN: the provided service token is not valid")
				}
			}
		case "admin":
			if err := checkIP(ctx, cfg.AdminIPs); err != nil {
				cfg.Logger.Warn("grpc: admin ip blocked",
					"ip", peerIP(ctx), "method", info.FullMethod)
				return nil, err
			}
			role := roleAdmin
			if len(cfg.AdminKeys) > 0 {
				key, ok := credentialFrom(ctx, false)
				if !ok {
					return nil, status.Error(codes.Unauthenticated,
						"MISSING_API_KEY: authorization bearer header is required")
				}
				if _, ok := cfg.AdminKeys[key]; !ok {
					cfg.Logger.Warn("grpc: invalid api key", "method", info.FullMethod)
					return nil, status.Error(codes.Unauthenticated,
						"INVALID_API_KEY: the provided API key is not valid")
				}
				if r, ok := cfg.AdminRoles[key]; ok {
					role = r
				}
			}
			if role == roleViewer && !adminReadMethods[strings.TrimPrefix(info.FullMethod, adminPrefix)] {
				cfg.Logger.Warn("grpc: role not allowed",
					"role", role, "method", info.FullMethod)
				return nil, status.Error(codes.PermissionDenied,
					"ROLE_NOT_ALLOWED: role "+role+" may only read Admin endpoints")
			}
		}
		return handler(ctx, req)
	}
}

// checkIP enforces a CIDR whitelist against the peer address, mirroring
// the REST middleware: configured whitelist means a missing/unparsable
// peer IP is denied (fail closed).
func checkIP(ctx context.Context, allowed []*net.IPNet) error {
	if len(allowed) == 0 {
		return nil
	}
	if !ipAllowed(peerIP(ctx), allowed) {
		return status.Error(codes.PermissionDenied,
			"IP_NOT_ALLOWED: your IP is not in the whitelist")
	}
	return nil
}

// peerIP is the caller's host part without the transport port.
func peerIP(ctx context.Context) string {
	p, ok := peer.FromContext(ctx)
	if !ok || p.Addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(p.Addr.String())
	if err != nil {
		return p.Addr.String()
	}
	return host
}

// ipAllowed reports whether ipStr falls in any allowed CIDR (net.ParseIP
// failure denies, same as the REST helper).
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

// credentialFrom pulls the bearer credential from incoming metadata.
// Registry also accepts the X-Atlas-Token header counterpart
// ("x-atlas-token") because game servers' internal tooling uses it over
// REST; admin keys are bearer-only, matching the REST middleware.
func credentialFrom(ctx context.Context, allowAtlasToken bool) (string, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", false
	}
	if auth := firstMD(md, "authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer "), true
	}
	if allowAtlasToken {
		if tok := firstMD(md, "x-atlas-token"); tok != "" {
			return tok, true
		}
	}
	return "", false
}

func firstMD(md metadata.MD, key string) string {
	if vs := md.Get(key); len(vs) > 0 {
		return vs[0]
	}
	return ""
}
