package grpc

import (
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	pb "github.com/cuihairu/atlas/api/pb"
)

// TestServiceSurfacePinned freezes the gRPC contract surface: every service
// and method in the compiled-in proto descriptor — the exact contract the
// server serves — must be listed here verbatim, so adding, renaming or
// removing an RPC, or flipping one to streaming, fails this test until the
// inventory is updated on purpose. (Server embeds the five generated service
// implementations, so a registered surface that drifted from the descriptor
// would not compile.) This is the mechanical half of the v1.0 API-freeze
// preparation (TODO.md "API stabilization and v1.0"); the freeze decision
// itself stays with the user.
func TestServiceSurfacePinned(t *testing.T) {
	fd := pb.File_api_proto_atlas_proto
	if fd == nil {
		t.Fatal("atlas proto descriptor not registered")
	}

	got := make([]string, 0, 24)
	services := fd.Services()
	for i := 0; i < services.Len(); i++ {
		svc := services.Get(i)
		methods := svc.Methods()
		for j := 0; j < methods.Len(); j++ {
			m := methods.Get(j)
			entry := string(svc.FullName()) + "/" + string(m.Name())
			switch {
			case m.IsStreamingClient() && m.IsStreamingServer():
				entry += " (bidi-stream)"
			case m.IsStreamingClient():
				entry += " (client-stream)"
			case m.IsStreamingServer():
				entry += " (server-stream)"
			}
			got = append(got, entry)
		}
	}
	sort.Strings(got)

	want := []string{
		"atlas.v1.AdminService/CreateMigration",
		"atlas.v1.AdminService/Disable",
		"atlas.v1.AdminService/Enable",
		"atlas.v1.AdminService/GetLoadHeatmap",
		"atlas.v1.AdminService/GetMigration",
		"atlas.v1.AdminService/GetStats",
		"atlas.v1.AdminService/ListMigrations",
		"atlas.v1.AdminService/RollbackMigration",
		"atlas.v1.AdminService/SearchCharacters",
		"atlas.v1.AdminService/SetDrain",
		"atlas.v1.AdminService/SetMaintenance",
		"atlas.v1.DirectoryService/CreateCharacter",
		"atlas.v1.DirectoryService/DeleteCharacter",
		"atlas.v1.DirectoryService/GetCharacter",
		"atlas.v1.DirectoryService/ListCharactersByAccount",
		"atlas.v1.DirectoryService/ListCharactersByServer",
		"atlas.v1.DirectoryService/UpdateCharacter",
		"atlas.v1.DiscoveryService/GetServer",
		"atlas.v1.DiscoveryService/ListServers",
		"atlas.v1.RegistryService/Heartbeat",
		"atlas.v1.RegistryService/Register",
		"atlas.v1.RegistryService/Unregister",
		"atlas.v1.RoutingService/Recommend",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("gRPC surface changed:\n  added:   %v\n  removed: %v\nupdate the pinned inventory in this test on purpose (v1.0 freeze guardrail)",
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
