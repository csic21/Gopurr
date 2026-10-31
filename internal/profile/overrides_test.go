package profile

import (
	"os"
	"path/filepath"
	"testing"

	"gopurr/internal/patch"
)

func TestOverridesSurviveSourceOperationsAndReturnCopies(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "source.yaml")
	if err := os.WriteFile(path, []byte("rules: [\"MATCH,DIRECT\"]"), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := s.AddFile(path, "Work")
	if err != nil {
		t.Fatal(err)
	}
	dns := patch.DefaultDNSSettings()
	next := patch.ProfileOverrides{ExtraRules: []string{"DOMAIN,work.example,DIRECT"}, MergeYAML: "profile: {store-selected: true}", DNS: &dns}
	if err := s.SetOverrides(p.ID, next); err != nil {
		t.Fatal(err)
	}
	next.ExtraRules[0] = "mutated"
	copy, _ := s.Get(p.ID)
	copy.Overrides.DNS.Nameserver[0] = "mutated"
	list := s.List()
	list[0].Overrides.ExtraRules[0] = "mutated"
	if err := s.Refresh(p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(path, "Renamed"); err != nil {
		t.Fatal(err)
	}
	p, _ = s.Get(p.ID)
	p.Name = "Edited"
	if err := s.Edit(p); err != nil {
		t.Fatal(err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	saved, _ := again.Get(p.ID)
	if saved.Overrides.ExtraRules[0] != "DOMAIN,work.example,DIRECT" || saved.Overrides.DNS.Nameserver[0] != "1.1.1.1" {
		t.Fatalf("overrides lost or aliased: %+v", saved.Overrides)
	}
	index := filepath.Join(dir, "profiles.json")
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(index, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOverrides(p.ID, patch.ProfileOverrides{}); err == nil {
		t.Fatal("expected failed index save")
	}
	still, _ := s.Get(p.ID)
	if len(still.Overrides.ExtraRules) != 1 {
		t.Fatal("failed save changed in-memory overrides")
	}
}
