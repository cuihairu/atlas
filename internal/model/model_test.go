package model

import (
	"errors"
	"testing"
	"time"
)

func TestServerStatusPredicates(t *testing.T) {
	cases := []struct {
		status      ServerStatus
		valid       bool
		visible     bool
		accepts     bool
		autoManaged bool
	}{
		{StatusStarting, true, false, false, true},
		{StatusOnline, true, true, true, true},
		{StatusDraining, true, false, false, false},
		{StatusMaintenance, true, true, false, false},
		{StatusSuspect, true, true, true, true},
		{StatusOffline, true, false, false, false},
		{StatusDisabled, true, false, false, false},
		{"bogus", false, false, false, false},
		{"", false, false, false, false},
	}
	for _, tc := range cases {
		if got := tc.status.Valid(); got != tc.valid {
			t.Errorf("%q Valid = %v, want %v", tc.status, got, tc.valid)
		}
		if got := tc.status.Visible(); got != tc.visible {
			t.Errorf("%q Visible = %v, want %v", tc.status, got, tc.visible)
		}
		if got := tc.status.AcceptsTraffic(); got != tc.accepts {
			t.Errorf("%q AcceptsTraffic = %v, want %v", tc.status, got, tc.accepts)
		}
		if got := tc.status.AutoManaged(); got != tc.autoManaged {
			t.Errorf("%q AutoManaged = %v, want %v", tc.status, got, tc.autoManaged)
		}
	}
}

func TestServerValidate(t *testing.T) {
	valid := func(mutate func(*Server)) *Server {
		s := &Server{
			ID:       "game-1",
			Region:   "cn-east",
			Endpoint: Endpoint{Host: "10.0.0.1", Port: 30001},
		}
		mutate(s)
		return s
	}
	cases := []struct {
		name    string
		server  *Server
		wantErr bool
	}{
		{"valid minimal", valid(func(*Server) {}), false},
		{"missing id", valid(func(s *Server) { s.ID = " " }), true},
		{"missing region", valid(func(s *Server) { s.Region = "" }), true},
		{"missing host", valid(func(s *Server) { s.Endpoint.Host = "" }), true},
		{"port zero", valid(func(s *Server) { s.Endpoint.Port = 0 }), true},
		{"port too large", valid(func(s *Server) { s.Endpoint.Port = 65536 }), true},
		{"negative capacity", valid(func(s *Server) { s.Capacity = -1 }), true},
		{"unknown status", valid(func(s *Server) { s.Status = "bogus" }), true},
		{"empty status ok", valid(func(s *Server) { s.Status = "" }), false},
	}
	for _, tc := range cases {
		err := tc.server.Validate()
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
		if err != nil && !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid wrap", tc.name, err)
		}
	}
}

func TestOverCapacity(t *testing.T) {
	// Capacity 0 = unlimited; never over.
	unlimited := &Server{Capacity: 0, Players: 100000}
	if unlimited.OverCapacity() {
		t.Error("unlimited server reported over capacity")
	}
	at := &Server{Capacity: 2000, Players: 1999}
	if at.OverCapacity() {
		t.Error("at-capacity-minus-one reported over")
	}
	over := &Server{Capacity: 2000, Players: 2000}
	if !over.OverCapacity() {
		t.Error("saturated server not reported over")
	}
}

func TestHeartbeatValidate(t *testing.T) {
	cases := []struct {
		name    string
		hb      Heartbeat
		wantErr bool
	}{
		{"valid", Heartbeat{Players: 10, Load: 0.5}, false},
		{"empty ok", Heartbeat{}, false},
		{"negative players", Heartbeat{Players: -1}, true},
		{"load below range", Heartbeat{Load: -0.1}, true},
		{"load above range", Heartbeat{Load: 1.1}, true},
		{"load bounds ok", Heartbeat{Load: 1}, false},
		{"unknown status", Heartbeat{Status: "bogus"}, true},
		{"valid status", Heartbeat{Status: StatusOnline}, false},
	}
	for _, tc := range cases {
		err := tc.hb.Validate()
		if (err != nil) != tc.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}

func TestMaintenanceWindowValidateAndActive(t *testing.T) {
	base := time.Now()
	win := &MaintenanceWindow{ID: "mwin-1", ServerID: "srv-1", StartAt: base, EndAt: base.Add(time.Hour)}

	if err := win.Validate(); err != nil {
		t.Fatalf("valid window rejected: %v", err)
	}

	cases := []struct {
		name string
		mut  func(*MaintenanceWindow)
	}{
		{"missing id", func(w *MaintenanceWindow) { w.ID = " " }},
		{"missing server", func(w *MaintenanceWindow) { w.ServerID = "" }},
		{"end before start", func(w *MaintenanceWindow) { w.EndAt = base.Add(-time.Second) }},
		{"end equals start", func(w *MaintenanceWindow) { w.EndAt = base }},
	}
	for _, tc := range cases {
		w := *win
		tc.mut(&w)
		if err := (&w).Validate(); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}

	// Active is half-open [start, end).
	if !win.Active(base) {
		t.Error("window inactive at start_at")
	}
	if !win.Active(base.Add(time.Hour - time.Second)) {
		t.Error("window inactive just before end_at")
	}
	if win.Active(base.Add(time.Hour)) {
		t.Error("window active at end_at")
	}
	if win.Active(base.Add(-time.Second)) {
		t.Error("window active before start_at")
	}
}

func TestAnnouncementValidateActiveAndLevels(t *testing.T) {
	base := time.Now()
	ann := &Announcement{ID: "ann-1", Title: "hi", Level: "", StartsAt: base, EndsAt: base.Add(time.Hour)}
	if err := ann.Validate(); err != nil {
		t.Fatalf("empty level must default valid: %v", err)
	}

	for _, lvl := range []string{"info", "warning", "critical"} {
		if !ValidAnnouncementLevel(lvl) {
			t.Errorf("level %q should be valid", lvl)
		}
	}
	for _, lvl := range []string{"loud", "INFO", "error"} {
		if ValidAnnouncementLevel(lvl) {
			t.Errorf("level %q should be invalid", lvl)
		}
	}

	cases := []struct {
		name string
		mut  func(*Announcement)
	}{
		{"missing id", func(a *Announcement) { a.ID = "" }},
		{"missing title", func(a *Announcement) { a.Title = "  " }},
		{"unknown level", func(a *Announcement) { a.Level = "loud" }},
		{"end before start", func(a *Announcement) { a.EndsAt = base.Add(-time.Second) }},
		{"end equals start", func(a *Announcement) { a.EndsAt = base }},
	}
	for _, tc := range cases {
		a := *ann
		a.Level = "info"
		tc.mut(&a)
		if err := (&a).Validate(); err == nil {
			t.Errorf("%s: expected error", tc.name)
		}
	}

	// Active is half-open [starts_at, ends_at).
	if !ann.Active(base) || !ann.Active(base.Add(time.Hour-time.Second)) {
		t.Error("announcement should be active inside interval")
	}
	if ann.Active(base.Add(time.Hour)) || ann.Active(base.Add(-time.Second)) {
		t.Error("announcement should be inactive outside interval")
	}
}

func TestStatsFinalizeDerivesOnlineServers(t *testing.T) {
	// BUGS ①: OnlineServers must always agree with ServersByStatus — the
	// derivation lives in Finalize so no producer can drift.
	s := &Stats{ServersByStatus: map[string]int{"online": 2, "suspect": 1}}
	s.Finalize()
	if s.OnlineServers != 2 {
		t.Errorf("online = %d, want 2", s.OnlineServers)
	}
	// Nil maps are healed, not crashed (the null.some lesson).
	empty := &Stats{}
	empty.Finalize()
	if empty.ServersByStatus == nil || empty.ServersByRegion == nil || empty.ServersByVersion == nil {
		t.Error("Finalize must default the classic facet maps")
	}
	if empty.OnlineServers != 0 {
		t.Errorf("empty online = %d, want 0", empty.OnlineServers)
	}
}

func TestEndpointString(t *testing.T) {
	if got := (Endpoint{Host: "10.0.0.1", Port: 30001}).String(); got != "10.0.0.1:30001" {
		t.Errorf("Endpoint.String() = %q, want 10.0.0.1:30001", got)
	}
	// A bare hostname still renders — JoinHostPort handles hosts that
	// would otherwise need brackets.
	if got := (Endpoint{Host: "game.internal", Port: 80}).String(); got != "game.internal:80" {
		t.Errorf("Endpoint.String() = %q, want game.internal:80", got)
	}
}

func TestMigrationStatusValid(t *testing.T) {
	valid := []MigrationStatus{
		MigrationPending, MigrationMigrating, MigrationVerifying,
		MigrationCompleted, MigrationFailed, MigrationRolledBack,
	}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("Valid(%q) = false, want true", s)
		}
	}
	for _, s := range []MigrationStatus{"", "running", "MIGRATING"} {
		if s.Valid() {
			t.Errorf("Valid(%q) = true, want false", s)
		}
	}
}
