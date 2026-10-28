package httpapi

import (
	"os"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// routePattern matches the single-line route registrations in handlers.go.
var routePattern = regexp.MustCompile(`mux\.HandleFunc\("([^"]+)"`)

// TestRouteSurfacePinned freezes the REST contract surface: every
// "METHOD /path" pattern registered in handlers.go must be listed here
// verbatim, so adding, renaming or removing a route fails this test until
// the inventory is updated on purpose. http.ServeMux cannot enumerate its
// patterns at runtime, so the pin reads the registration site itself — and
// also checks that every HandleFunc call site was matched, so a registration
// reformatted across lines cannot slip past silently. This is the mechanical
// half of the v1.0 API-freeze preparation (todo.md "API 稳定化与 v1.0");
// the freeze decision itself stays with the user.
func TestRouteSurfacePinned(t *testing.T) {
	src, err := os.ReadFile("handlers.go")
	if err != nil {
		t.Fatalf("read handlers.go: %v", err)
	}

	callSites := strings.Count(string(src), "mux.HandleFunc(")
	matches := routePattern.FindAllStringSubmatch(string(src), -1)
	if callSites == 0 || callSites != len(matches) {
		t.Fatalf("route registration moved or reformatted: %d HandleFunc call sites, %d matched; adjust routePattern before trusting this pin", callSites, len(matches))
	}

	got := make([]string, 0, len(matches))
	for _, m := range matches {
		got = append(got, m[1])
	}
	sort.Strings(got)

	want := []string{
		"DELETE /v1/admin/announcements/{id}",
		"DELETE /v1/admin/maintenance-windows/{id}",
		"DELETE /v1/admin/servers/{id}",
		"DELETE /v1/admin/servers/{id}/tags/{code}",
		"DELETE /v1/directory/characters/{character_id}",
		"GET /healthz",
		"GET /readyz",
		"GET /v1/admin/announcements",
		"GET /v1/admin/audit",
		"GET /v1/admin/bus-series",
		"GET /v1/admin/characters/search",
		"GET /v1/admin/crossserver/config",
		"GET /v1/admin/diagnose/routing",
		"GET /v1/admin/indexqueue/status",
		"GET /v1/admin/load-heatmap",
		"GET /v1/admin/load-series",
		"GET /v1/admin/maintenance-windows",
		"GET /v1/admin/migrations",
		"GET /v1/admin/migrations/{id}",
		"GET /v1/admin/rate-limits",
		"GET /v1/admin/realms",
		"GET /v1/admin/servers",
		"GET /v1/admin/servers/{id}/tags",
		"GET /v1/admin/shards",
		"GET /v1/admin/stats",
		"GET /v1/crossserver/config",
		"GET /v1/directory/accounts/{id}/characters",
		"GET /v1/directory/characters/{character_id}",
		"GET /v1/directory/servers/{id}/characters",
		"GET /v1/discovery/announcements",
		"GET /v1/discovery/servers",
		"GET /v1/discovery/servers/{id}",
		"GET /v1/routing/recommended",
		"PATCH /v1/directory/characters/{character_id}",
		"POST /v1/admin/announcements",
		"POST /v1/admin/maintenance-windows/batch",
		"POST /v1/admin/migrations",
		"POST /v1/admin/migrations/{id}/rollback",
		"POST /v1/admin/realms",
		"POST /v1/admin/servers/{id}/disable",
		"POST /v1/admin/servers/{id}/drain",
		"POST /v1/admin/servers/{id}/enable",
		"POST /v1/admin/servers/{id}/maintenance",
		"POST /v1/admin/servers/{id}/maintenance-window",
		"POST /v1/admin/servers/{id}/tags",
		"POST /v1/admin/servers/{id}/weight",
		"POST /v1/admin/shards",
		"POST /v1/directory/characters",
		"POST /v1/registry/servers/register",
		"POST /v1/registry/servers/{id}/heartbeat",
		"POST /v1/registry/servers/{id}/unregister",
		"PUT /v1/admin/crossserver/config",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("REST surface changed:\n  added:   %v\n  removed: %v\nupdate the pinned inventory in this test on purpose (v1.0 freeze guardrail)",
			missing(want, got), missing(got, want))
	}
}

// missing returns the entries of a that do not appear in b.
func missing(a, b []string) []string {
	var out []string
	for _, s := range a {
		if !slices.Contains(b, s) {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{"(none)"}
	}
	return []string{strings.Join(out, ", ")}
}
