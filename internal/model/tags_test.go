package model

import "testing"

func TestFillTagDefaults(t *testing.T) {
	// Preset: label/tier/public inherit from the preset table.
	yes := true
	got := FillTag(ServerTag{Code: TagHot}, nil)
	if got.Label != "火热" || got.Tier != TierHot || !got.Public {
		t.Fatalf("preset fill = %+v", got)
	}

	// Explicit values win over preset defaults.
	got = FillTag(ServerTag{Code: TagHot, Label: "超火爆", Tier: TierNew}, &yes)
	if got.Label != "超火爆" || got.Tier != TierNew || !got.Public {
		t.Fatalf("explicit fill = %+v", got)
	}

	// Preset can be made internal with public=false.
	internal := false
	got = FillTag(ServerTag{Code: TagNew}, &internal)
	if got.Public {
		t.Fatalf("preset forced internal = %+v", got)
	}

	// Custom code: neutral tier, internal by default.
	got = FillTag(ServerTag{Code: "ops_note", Label: "内部"}, nil)
	if got.Tier != TierNeutral || got.Public {
		t.Fatalf("custom fill = %+v", got)
	}
}

func TestValidateServerTags(t *testing.T) {
	ok := []ServerTag{
		{Code: "hot", Label: "火热", Tier: TierHot, Public: true},
		{Code: "ops_note", Label: "内部", Tier: TierNeutral},
	}
	if err := ValidateServerTags(ok); err != nil {
		t.Fatalf("valid list rejected: %v", err)
	}
	if err := ValidateServerTags(nil); err != nil {
		t.Fatalf("nil list rejected: %v", err)
	}

	bad := []ServerTag{
		{Code: "hot", Label: "火热", Tier: TierHot},
		{Code: "HOT", Label: "火热", Tier: TierHot},      // uppercase code
		{Code: "-x", Label: "x", Tier: TierNeutral},    // bad shape
		{Code: "ok", Label: "", Tier: TierNeutral},     // missing label
		{Code: "ok2", Label: "x", Tier: "sparkly"},     // unknown tier
		{Code: "hot", Label: "dup", Tier: TierNeutral}, // duplicate code
	}
	for i, tag := range bad {
		if err := ValidateServerTags([]ServerTag{ok[0], tag}); err == nil {
			t.Errorf("tags[%d] = %+v accepted, want rejection", i, tag)
		}
	}
}

func TestValidTagCode(t *testing.T) {
	valid := []string{"hot", "new", "no_register", "ops-note", "a", "0", "zone_eu-1"}
	for _, code := range valid {
		if !ValidTagCode(code) {
			t.Errorf("ValidTagCode(%q) = false, want true", code)
		}
	}
	invalid := []string{"", "-x", "_x", "HOT", "höt", "a b", "x.y", withLen(33)}
	for _, code := range invalid {
		if ValidTagCode(code) {
			t.Errorf("ValidTagCode(%q) = true, want false", code)
		}
	}
}

func withLen(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}

func TestTagHelpers(t *testing.T) {
	tags := []ServerTag{
		{Code: TagHot, Label: "火热", Tier: TierHot, Public: true},
		{Code: "ops_note", Label: "内部", Tier: TierNeutral},
	}
	if !HasTag(tags, TagHot) || HasTag(tags, TagFull) {
		t.Fatalf("HasTag misbehaves on %v", tags)
	}
	pub := PublicTags(tags)
	if len(pub) != 1 || pub[0].Code != TagHot {
		t.Fatalf("PublicTags = %v", pub)
	}
	if IsPresetTag("hot") != true || IsPresetTag("ops_note") {
		t.Fatal("IsPresetTag wrong")
	}
}

func TestRegistrationBlocked(t *testing.T) {
	base := func(mutate func(*Server)) *Server {
		s := &Server{ID: "game-1", Status: StatusOnline}
		mutate(s)
		return s
	}

	if code, _ := base(func(s *Server) {}).RegistrationBlocked(false); code != "" {
		t.Fatal("online server should allow registration")
	}
	if code, _ := base(func(s *Server) {}).RegistrationBlocked(true); code != "" {
		t.Fatal("online server should allow registration in warn mode")
	}

	// 禁止注册 always wins, even in warn mode.
	s := base(func(s *Server) {
		s.Tags = []ServerTag{{Code: TagNoRegister, Label: "禁止注册", Tier: TierWarning, Public: true}}
	})
	if code, msg := s.RegistrationBlocked(false); code != "REGISTRATION_FORBIDDEN" || msg == "" {
		t.Fatalf("no_register block = %q %q", code, msg)
	}
	if code, _ := s.RegistrationBlocked(true); code != "REGISTRATION_FORBIDDEN" {
		t.Fatal("no_register must ignore warn mode")
	}

	// 维护中 via tag: blocks by default, warns when warnOnly.
	s = base(func(s *Server) {
		s.Tags = []ServerTag{{Code: TagMaintenance, Label: "维护中", Tier: TierWarning, Public: true}}
	})
	if code, _ := s.RegistrationBlocked(false); code != "SERVER_IN_MAINTENANCE" {
		t.Fatalf("maintenance tag block = %q", code)
	}
	if code, _ := s.RegistrationBlocked(true); code != "" {
		t.Fatal("maintenance tag warn mode should allow")
	}

	// 维护中 via lifecycle status behaves the same.
	s = base(func(s *Server) { s.Status = StatusMaintenance })
	if code, _ := s.RegistrationBlocked(false); code != "SERVER_IN_MAINTENANCE" {
		t.Fatalf("maintenance status block = %q", code)
	}
	if code, _ := s.RegistrationBlocked(true); code != "" {
		t.Fatal("maintenance status warn mode should allow")
	}
}
