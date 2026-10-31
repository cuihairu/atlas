// Error-path coverage for the Redis runtime store: the canceled-context
// sweeps drive every verb's query-failure wrap, and a ghost-HGETALL client
// pins the ListRuntimes degradation branches (a per-key error and an
// expired-between-SCAN-and-HGETALL key) without racing real TTL expiry.
package redisstore

import (
	"context"
	"errors"
	"testing"

	"github.com/redis/go-redis/v9"

	"github.com/cuihairu/atlas/internal/model"
)

var errFault = errors.New("fault injected")

// ghostHGetAll wraps a real client and fakes HGetAll for one key: either an
// error (a backend blip on that key) or an empty result (the key expired
// between SCAN and HGETALL). All other methods delegate untouched.
type ghostHGetAll struct {
	redis.UniversalClient
	ghost string
	vals  map[string]string
	err   error
}

func (g *ghostHGetAll) HGetAll(ctx context.Context, key string) *redis.MapStringStringCmd {
	if key == g.ghost {
		return redis.NewMapStringStringResult(g.vals, g.err)
	}
	return g.UniversalClient.HGetAll(ctx, key)
}

// TestPingAndDeleteRuntimeCanceled pins the two lifecycle verbs against a
// canceled context: a dead connection must surface as an error, not a hang.
func TestPingAndDeleteRuntimeCanceled(t *testing.T) {
	st := openTestStore(t)
	cctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := st.Ping(cctx); err == nil {
		t.Error("ping with canceled context: expected error")
	}
	if err := st.DeleteRuntime(cctx, "cancel-srv"); err == nil {
		t.Error("delete runtime with canceled context: expected error")
	}
}

// TestRecordHeartbeatCanceled pins the pipeline-exec wrap: a heartbeat whose
// HSet/Expire pipeline cannot run must be reported, never a silent success.
func TestRecordHeartbeatCanceled(t *testing.T) {
	st := openTestStore(t)
	cctx, cancel := context.WithCancel(context.Background())
	cancel()

	hb := model.Heartbeat{Players: 1, Load: 0.5, Status: model.StatusOnline}
	if err := st.RecordHeartbeat(cctx, "hb-cancel", hb); err == nil {
		t.Fatal("record heartbeat with canceled context: expected error")
	}
}

// TestGetRuntimeCanceled pins the HGetAll wrap of the single-key read.
func TestGetRuntimeCanceled(t *testing.T) {
	st := openTestStore(t)
	cctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := st.GetRuntime(cctx, "rt-cancel"); err == nil {
		t.Fatal("get runtime with canceled context: expected error")
	}
}

// TestGetRuntimesCanceled pins the pipeline-exec wrap of the batched read.
func TestGetRuntimesCanceled(t *testing.T) {
	st := openTestStore(t)
	cctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := st.GetRuntimes(cctx, []string{"a", "b"}); err == nil {
		t.Fatal("get runtimes with canceled context: expected error")
	}
}

// TestListRuntimesCanceled pins the scan-error wrap: a canceled context stops
// the SCAN iterator and surfaces its error instead of a partial map.
func TestListRuntimesCanceled(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	if err := st.RecordHeartbeat(ctx, "scan-srv", model.Heartbeat{Players: 1, Load: 0.5, Status: model.StatusOnline}); err != nil {
		t.Fatalf("record: %v", err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()

	if _, err := st.ListRuntimes(cctx); err == nil {
		t.Fatal("list runtimes with canceled context: expected error")
	}
}

// TestListRuntimesPerKeyError pins the fail-hard contract of the fleet-wide
// read: a key whose HGetAll errors is surfaced (not silently dropped) — an
// unknown liveness view must not be presented as a healthy zero.
func TestListRuntimesPerKeyError(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	hb := model.Heartbeat{Players: 1, Load: 0.5, Status: model.StatusOnline}
	if err := st.RecordHeartbeat(ctx, "ghost-srv", hb); err != nil {
		t.Fatalf("record ghost: %v", err)
	}
	if err := st.RecordHeartbeat(ctx, "good-srv", hb); err != nil {
		t.Fatalf("record good: %v", err)
	}

	ghost := &ghostHGetAll{UniversalClient: st.client, ghost: runtimeKey("ghost-srv"), err: errFault}
	st2 := New(ghost)
	if _, err := st2.ListRuntimes(ctx); err == nil {
		t.Fatal("list runtimes with a failing key: expected error, got nil")
	}
}

// TestListRuntimesExpiredBetweenScanAndHGetAll pins the expiry race: a key
// that SCAN saw but HGetAll reports empty is skipped, not surfaced as an
// error (it expired between the two calls).
func TestListRuntimesExpiredBetweenScanAndHGetAll(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	hb := model.Heartbeat{Players: 1, Load: 0.5, Status: model.StatusOnline}
	if err := st.RecordHeartbeat(ctx, "expired-srv", hb); err != nil {
		t.Fatalf("record expired: %v", err)
	}
	if err := st.RecordHeartbeat(ctx, "alive-srv", hb); err != nil {
		t.Fatalf("record alive: %v", err)
	}

	ghost := &ghostHGetAll{UniversalClient: st.client, ghost: runtimeKey("expired-srv"), vals: map[string]string{}}
	st2 := New(ghost)
	out, err := st2.ListRuntimes(ctx)
	if err != nil {
		t.Fatalf("list runtimes: %v", err)
	}
	if _, ok := out["alive-srv"]; !ok {
		t.Fatalf("live server missing from runtime view: %v", out)
	}
	if _, ok := out["expired-srv"]; ok {
		t.Fatalf("expired key must be skipped, not returned: %v", out)
	}
}
