package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cuihairu/atlas/internal/model"
)

// deleteJSON issues a DELETE and returns the raw response (test servers run
// without the admin auth chain; main.go composes it).
func deleteJSON(t *testing.T, ts *httptest.Server, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	return resp
}

// TestDiscoveryAnnouncements covers the client-facing endpoint: active
// announcements only, global + requested server.
func TestDiscoveryAnnouncements(t *testing.T) {
	ts, mem := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "srv-1")

	ctx := context.Background()
	now := time.Now()
	srvID := "srv-1"
	anns := []*model.Announcement{
		{ID: "ann-global", Title: "Event", Level: "info", StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour)},
		{ID: "ann-srv1", Title: "Restart", Level: "warning", ServerID: &srvID, StartsAt: now.Add(-time.Hour), EndsAt: now.Add(time.Hour)},
		{ID: "ann-expired", Title: "Old", Level: "info", StartsAt: now.Add(-2 * time.Hour), EndsAt: now.Add(-time.Hour)},
	}
	for _, a := range anns {
		if err := mem.CreateAnnouncement(ctx, a); err != nil {
			t.Fatalf("seed %s: %v", a.ID, err)
		}
	}

	resp := getJSON(t, ts, "/v1/discovery/announcements?server_id=srv-1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		Announcements []*model.Announcement `json:"announcements"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Announcements) != 2 {
		t.Fatalf("got %d announcements, want 2 (active, global+srv-1)", len(body.Announcements))
	}
	if body.Announcements[0].ID != "ann-srv1" || body.Announcements[1].ID != "ann-global" {
		t.Errorf("order = [%s %s], want [ann-srv1 ann-global] (newest first)",
			body.Announcements[0].ID, body.Announcements[1].ID)
	}
}

// TestAdminMaintenanceWindowFlow: create (auto-announce) → list → delete.
func TestAdminMaintenanceWindowFlow(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "srv-1")

	now := time.Now()
	createBody := map[string]any{
		"start_at": now.Format(time.RFC3339),
		"end_at":   now.Add(time.Hour).Format(time.RFC3339),
	}
	resp := postJSON(t, ts, "/v1/admin/servers/srv-1/maintenance-window", createBody)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	var mw model.MaintenanceWindow
	if err := json.NewDecoder(resp.Body).Decode(&mw); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if mw.ID == "" || mw.AnnouncementID == nil {
		t.Fatalf("window = %+v, want id + linked announcement", mw)
	}

	// Linked announcement visible via discovery.
	dresp := getJSON(t, ts, "/v1/discovery/announcements?server_id=srv-1")
	var dbody struct {
		Announcements []*model.Announcement `json:"announcements"`
	}
	_ = json.NewDecoder(dresp.Body).Decode(&dbody)
	if len(dbody.Announcements) != 1 || dbody.Announcements[0].ID != *mw.AnnouncementID {
		t.Fatalf("discovery = %+v, want the auto announcement", dbody.Announcements)
	}

	// Admin list.
	lresp := getJSON(t, ts, "/v1/admin/maintenance-windows?server_id=srv-1")
	var lbody struct {
		Windows []*model.MaintenanceWindow `json:"maintenance_windows"`
	}
	_ = json.NewDecoder(lresp.Body).Decode(&lbody)
	if len(lbody.Windows) != 1 || lbody.Windows[0].ID != mw.ID {
		t.Fatalf("admin list = %+v", lbody.Windows)
	}

	// Delete.
	if resp := deleteJSON(t, ts, "/v1/admin/maintenance-windows/"+mw.ID); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}

	// Validation: inverted range.
	bad := map[string]any{
		"start_at": now.Add(time.Hour).Format(time.RFC3339),
		"end_at":   now.Format(time.RFC3339),
	}
	if resp := postJSON(t, ts, "/v1/admin/servers/srv-1/maintenance-window", bad); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("inverted window status = %d, want 400", resp.StatusCode)
	}

	// Unknown server.
	if resp := postJSON(t, ts, "/v1/admin/servers/ghost/maintenance-window", createBody); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown server status = %d, want 404", resp.StatusCode)
	}
}

// TestAdminAnnouncementCRUDEndpoints covers the admin REST surface.
func TestAdminAnnouncementCRUDEndpoints(t *testing.T) {
	ts, _ := setupTestServer(t)
	defer ts.Close()
	registerTestServer(t, ts, "srv-1")

	now := time.Now()
	body := map[string]any{
		"title":     "Double drops",
		"body":      "This weekend",
		"level":     "info",
		"starts_at": now.Add(-time.Minute).Format(time.RFC3339),
		"ends_at":   now.Add(time.Hour).Format(time.RFC3339),
	}
	resp := postJSON(t, ts, "/v1/admin/announcements", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", resp.StatusCode)
	}
	var a model.Announcement
	_ = json.NewDecoder(resp.Body).Decode(&a)
	if a.ID == "" {
		t.Fatal("no id returned")
	}

	lresp := getJSON(t, ts, "/v1/admin/announcements?active=true")
	var lbody struct {
		Announcements []*model.Announcement `json:"announcements"`
	}
	_ = json.NewDecoder(lresp.Body).Decode(&lbody)
	if len(lbody.Announcements) != 1 {
		t.Fatalf("list = %d, want 1", len(lbody.Announcements))
	}

	if resp := deleteJSON(t, ts, "/v1/admin/announcements/"+a.ID); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}

	// Invalid level rejected.
	bad := map[string]any{
		"title":     "x",
		"level":     "loud",
		"starts_at": now.Format(time.RFC3339),
		"ends_at":   now.Add(time.Hour).Format(time.RFC3339),
	}
	if resp := postJSON(t, ts, "/v1/admin/announcements", bad); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid level status = %d, want 400", resp.StatusCode)
	}
}
