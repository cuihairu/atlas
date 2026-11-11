package routing

import (
	"context"
	"fmt"
	"testing"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
	"github.com/cuihairu/atlas/internal/store/memory"
)

// TestListMatchingCoversFilteredSetPastDefaultPage pins the Recommend
// candidate contract: the filtered set is cursor-paginated at the store
// cap. A bare list call takes the store's 50-row default page, which
// made every server beyond the first 50 IDs invisible to recommendation
// no matter how healthy it was.
func TestListMatchingCoversFilteredSetPastDefaultPage(t *testing.T) {
	mem := memory.New()
	// 60 eligible servers — past the default page, under the hard cap.
	const n = 60
	for i := 0; i < n; i++ {
		seed(t, mem, fmt.Sprintf("srv-%03d", i), "eu", model.StatusOnline, 10, 0.1, 100)
	}

	svc := New(mem, mem, mem, mem)
	got, err := svc.listMatching(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("listMatching: %v", err)
	}
	if len(got) != n {
		t.Fatalf("listMatching = %d servers, want %d (default-page truncation)", len(got), n)
	}
}

// TestDiagnoseCoversFleetsPastTheStoreCap pins the diagnosis contract:
// verdicts cover the whole fleet. A single list call truncates at the
// store cap, and servers past it would silently vanish from the report —
// the disappearance Diagnose exists to rule out.
func TestDiagnoseCoversFleetsPastTheStoreCap(t *testing.T) {
	mem := memory.New()
	const fleet = store.ListServersMaxLimit + 50
	for i := 0; i < fleet; i++ {
		seed(t, mem, fmt.Sprintf("srv-%03d", i), "eu", model.StatusOnline, 10, 0.1, 100)
	}

	svc := New(mem, mem, mem, mem)
	diag, err := svc.Diagnose(context.Background(), Request{Region: "eu"})
	if err != nil {
		t.Fatalf("Diagnose: %v", err)
	}
	if len(diag.Servers) != fleet {
		t.Fatalf("Diagnose verdicts = %d, want %d (past-the-cap truncation)", len(diag.Servers), fleet)
	}
	if diag.Stage != "strict" || diag.WinnerID == "" {
		t.Fatalf("stage %q winner %q, want strict with a winner", diag.Stage, diag.WinnerID)
	}
}
