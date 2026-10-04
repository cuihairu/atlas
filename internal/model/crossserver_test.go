package model

import (
	"encoding/json"
	"testing"
)

func testCrossSpec() CrossServerSpec {
	return CrossServerSpec{
		Topology: CrossServerTopology{Clusters: []CrossServerCluster{
			{ID: "c1", Servers: []string{"game-1", "game-2"}},
		}},
		Groups:       []CrossServerGroup{{ID: "g1", Servers: []string{"game-1"}}},
		Features:     map[string]bool{"cross_battle": true},
		MatchDomains: []CrossServerMatchDomain{{ID: "m1", Servers: []string{"game-1"}, Params: map[string]string{"mmr": "0-3000"}}},
		CrossPlayTypes: []CrossPlayType{
			{ID: "battlefield", Name: "跨服战场", Summary: "跨服 PVP 匹配对局", Lifecycle: CrossPlayLifecycleSeasonal, Matchmaking: true, Ranking: true, IDPrefix: "xb"},
		},
	}
}

func TestHashCrossServerSpecDeterministic(t *testing.T) {
	a, b := testCrossSpec(), testCrossSpec()
	if HashCrossServerSpec(a) != HashCrossServerSpec(b) {
		t.Fatal("identical specs hashed differently")
	}

	// nil and empty containers normalize to the same bytes — otherwise an
	// idempotent re-save would look like a change.
	withNil := CrossServerSpec{}
	withEmpty := NormalizeCrossServerSpec(CrossServerSpec{})
	if HashCrossServerSpec(withNil) != HashCrossServerSpec(withEmpty) {
		t.Error("nil and normalized-empty specs hash differently")
	}

	c := testCrossSpec()
	c.Features["cross_battle"] = false
	if HashCrossServerSpec(c) == HashCrossServerSpec(testCrossSpec()) {
		t.Error("different content produced the same hash")
	}

	// The type table is hashed content too: a changed table must move
	// the hash (idempotent save relies on it).
	tp := testCrossSpec()
	tp.CrossPlayTypes[0].Lifecycle = CrossPlayLifecyclePersistent
	if HashCrossServerSpec(tp) == HashCrossServerSpec(testCrossSpec()) {
		t.Error("type table change produced the same hash")
	}
}

func TestHashCrossServerSpecIgnoresMapOrdering(t *testing.T) {
	a := testCrossSpec()
	a.Features = map[string]bool{"aaa": true, "bbb": false, "ccc": true}
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	// Re-decode a copy: the map iteration order differs, the content does not.
	var b CrossServerSpec
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	if HashCrossServerSpec(a) != HashCrossServerSpec(b) {
		t.Error("hash depends on map iteration order")
	}
}

func TestValidateCrossServerSpec(t *testing.T) {
	if err := ValidateCrossServerSpec(NormalizeCrossServerSpec(CrossServerSpec{})); err != nil {
		t.Fatalf("empty spec is valid: %v", err)
	}
	if err := ValidateCrossServerSpec(testCrossSpec()); err != nil {
		t.Fatalf("sample spec: %v", err)
	}

	cases := []struct {
		name string
		mut  func(*CrossServerSpec)
	}{
		{"missing cluster id", func(s *CrossServerSpec) { s.Topology.Clusters[0].ID = "" }},
		{"duplicate cluster id", func(s *CrossServerSpec) {
			s.Topology.Clusters = append(s.Topology.Clusters, CrossServerCluster{ID: "c1"})
		}},
		{"bad cluster status", func(s *CrossServerSpec) { s.Topology.Clusters[0].Status = "paused" }},
		{"empty server id in cluster", func(s *CrossServerSpec) { s.Topology.Clusters[0].Servers = []string{"game-1", ""} }},
		{"duplicate server in cluster", func(s *CrossServerSpec) { s.Topology.Clusters[0].Servers = []string{"game-1", "game-1"} }},
		{"missing group id", func(s *CrossServerSpec) { s.Groups[0].ID = "" }},
		// 拓扑为准（三者关系裁决）: groups are ops sets constrained by the
		// topology — members must sit inside ONE cluster.
		{"group member outside every cluster", func(s *CrossServerSpec) {
			s.Groups[0].Servers = []string{"game-1", "game-9"}
		}},
		{"group spans two clusters", func(s *CrossServerSpec) {
			s.Topology.Clusters = append(s.Topology.Clusters, CrossServerCluster{ID: "c2", Servers: []string{"game-3"}})
			s.Groups[0].Servers = []string{"game-1", "game-3"}
		}},
		{"server in two clusters", func(s *CrossServerSpec) {
			s.Topology.Clusters = append(s.Topology.Clusters, CrossServerCluster{ID: "c2", Servers: []string{"game-1"}})
		}},
		{"bad feature key", func(s *CrossServerSpec) { s.Features["Cross Battle!"] = true }},
		{"feature key too long", func(s *CrossServerSpec) {
			s.Features["a"+string(make([]byte, 64))] = true
		}},
		{"missing domain id", func(s *CrossServerSpec) { s.MatchDomains[0].ID = "" }},
		{"duplicate domain id", func(s *CrossServerSpec) {
			s.MatchDomains = append(s.MatchDomains, CrossServerMatchDomain{ID: "m1"})
		}},
		{"missing type id", func(s *CrossServerSpec) { s.CrossPlayTypes[0].ID = "" }},
		{"bad type id charset", func(s *CrossServerSpec) { s.CrossPlayTypes[0].ID = "Cross Battle" }},
		{"duplicate type id", func(s *CrossServerSpec) {
			s.CrossPlayTypes = append(s.CrossPlayTypes, CrossPlayType{ID: "battlefield", IDPrefix: "zz"})
		}},
		{"bad type lifecycle", func(s *CrossServerSpec) { s.CrossPlayTypes[0].Lifecycle = "forever" }},
		{"type id prefix too short", func(s *CrossServerSpec) { s.CrossPlayTypes[0].IDPrefix = "x" }},
		{"type id prefix uppercase", func(s *CrossServerSpec) { s.CrossPlayTypes[0].IDPrefix = "XB" }},
		{"duplicate type id prefix", func(s *CrossServerSpec) {
			s.CrossPlayTypes = append(s.CrossPlayTypes, CrossPlayType{ID: "chat", IDPrefix: "xb"})
		}},
	}
	for _, tc := range cases {
		spec := testCrossSpec()
		tc.mut(&spec)
		if err := ValidateCrossServerSpec(spec); err == nil {
			t.Errorf("%s: accepted an invalid spec", tc.name)
		}
	}
}

func TestEmptyCrossServerConfigETag(t *testing.T) {
	cfg := EmptyCrossServerConfig()
	if cfg.Version != 0 {
		t.Errorf("version = %d, want 0", cfg.Version)
	}
	if cfg.ETag() != `"`+cfg.Hash+`"` {
		t.Errorf("ETag = %s", cfg.ETag())
	}
	if cfg.Hash == "" {
		t.Error("empty snapshot has no hash")
	}
	// The empty snapshot's spec must itself be valid and hash-stable.
	if err := ValidateCrossServerSpec(cfg.Spec); err != nil {
		t.Errorf("empty spec invalid: %v", err)
	}
}

func TestNormalizeCrossServerSpec(t *testing.T) {
	spec := NormalizeCrossServerSpec(CrossServerSpec{})
	if spec.Topology.Clusters == nil || spec.Groups == nil ||
		spec.Features == nil || spec.MatchDomains == nil ||
		spec.CrossPlayTypes == nil {
		t.Fatal("normalization left nil containers")
	}
	// Empty params collapse to nil so a domain without params and one with
	// an empty map hash identically.
	d := CrossServerMatchDomain{ID: "m1", Params: map[string]string{}}
	if got := NormalizeCrossServerSpec(CrossServerSpec{MatchDomains: []CrossServerMatchDomain{d}}); got.MatchDomains[0].Params != nil {
		t.Error("empty params not collapsed to nil")
	}
}

func TestCloneCrossServerSpecIndependent(t *testing.T) {
	spec := testCrossSpec()
	clone := CloneCrossServerSpec(spec)

	// In-place mutation of the original must not reach the clone —
	// stores hand out clones so caller mutations cannot rewrite
	// stored history.
	spec.Topology.Clusters[0].Servers[0] = "game-mutated"
	spec.Groups[0].Servers[0] = "game-mutated"
	spec.Features["cross_battle"] = false
	spec.MatchDomains[0].Params["mmr"] = "9999"
	spec.CrossPlayTypes = append(spec.CrossPlayTypes, CrossPlayType{ID: "chat"})

	if clone.Topology.Clusters[0].Servers[0] != "game-1" {
		t.Error("clone shares the cluster servers array")
	}
	if clone.Groups[0].Servers[0] != "game-1" {
		t.Error("clone shares the group servers array")
	}
	if !clone.Features["cross_battle"] {
		t.Error("clone shares the features map")
	}
	if clone.MatchDomains[0].Params["mmr"] != "0-3000" {
		t.Error("clone shares the params map")
	}
	if len(clone.CrossPlayTypes) != 1 {
		t.Error("clone shares the type table slice")
	}
	if HashCrossServerSpec(spec) == HashCrossServerSpec(clone) {
		t.Error("mutated original still hashes like the clone")
	}
}
