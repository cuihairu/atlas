package grpc

import (
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"google.golang.org/grpc"

	pb "github.com/cuihairu/atlas/api/pb"
	"github.com/cuihairu/atlas/internal/admin"
	"github.com/cuihairu/atlas/internal/directory"
	"github.com/cuihairu/atlas/internal/discovery"
	httpadapter "github.com/cuihairu/atlas/internal/event/http"
	"github.com/cuihairu/atlas/internal/registry"
	"github.com/cuihairu/atlas/internal/routing"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// TestServiceSurfacePinned freezes the gRPC contract surface from both
// sources that must agree: what RegisterServices actually mounts on a grpc
// server (GetServiceInfo — a dropped Register*Server call shows up here) and
// what the compiled-in proto descriptor declares (a proto edit shows up
// here). Both must equal the inventory below verbatim, so adding, renaming
// or removing an RPC, flipping one to streaming, or silently un-registering
// a service fails this test until the inventory is updated on purpose. This
// is the mechanical half of the v1.0 API-freeze preparation (TODO.md "API
// stabilization and v1.0"); the freeze decision itself stays with the user.
func TestServiceSurfacePinned(t *testing.T) {
	mem := memory.New()
	srv := New(
		registry.New(mem, mem, testLogger()),
		discovery.New(mem, mem),
		directory.New(mem),
		routing.New(mem, mem, mem, mem),
		admin.New(mem),
		httpadapter.New(),
	)
	g := grpc.NewServer()
	srv.RegisterServices(g)

	registered := make([]string, 0, 24)
	for name, info := range g.GetServiceInfo() {
		for _, m := range info.Methods {
			registered = append(registered, name+"/"+m.Name+streamMark(m.IsClientStream, m.IsServerStream))
		}
	}
	sort.Strings(registered)

	fd := pb.File_api_proto_atlas_proto
	if fd == nil {
		t.Fatal("atlas proto descriptor not registered")
	}
	declared := make([]string, 0, 24)
	services := fd.Services()
	for i := 0; i < services.Len(); i++ {
		svc := services.Get(i)
		methods := svc.Methods()
		for j := 0; j < methods.Len(); j++ {
			m := methods.Get(j)
			declared = append(declared, string(svc.FullName())+"/"+string(m.Name())+streamMark(m.IsStreamingClient(), m.IsStreamingServer()))
		}
	}
	sort.Strings(declared)

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
	if !reflect.DeepEqual(registered, want) {
		t.Fatalf("registered gRPC surface changed:\n  added:   %v\n  removed: %v\nupdate the pinned inventory in this test on purpose (v1.0 freeze guardrail)",
			missing(want, registered), missing(registered, want))
	}
	if !reflect.DeepEqual(declared, want) {
		t.Fatalf("proto descriptor surface changed:\n  added:   %v\n  removed: %v\nupdate the pinned inventory in this test on purpose (v1.0 freeze guardrail)",
			missing(want, declared), missing(declared, want))
	}
}

// streamMark annotates a surface entry when a method is streaming, so
// flipping an RPC to streaming also breaks the pin.
func streamMark(clientStream, serverStream bool) string {
	switch {
	case clientStream && serverStream:
		return " (bidi-stream)"
	case clientStream:
		return " (client-stream)"
	case serverStream:
		return " (server-stream)"
	}
	return ""
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
