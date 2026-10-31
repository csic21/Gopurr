package profile

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func aggregateSourceFile(t *testing.T, s *Store, name, raw string) Profile {
	t.Helper()
	p := filepath.Join(t.TempDir(), name+".yaml")
	if err := os.WriteFile(p, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	item, err := s.AddFile(p, name)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestAggregateFiltersPrefixesDedupeAndReferences(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := aggregateSourceFile(t, s, "工作", `proxies:
  - {name: 香港, type: ss, server: one.example, port: 443, cipher: aes-128-gcm, password: secret, plugin-opts: {mode: websocket, host: front.example}}
  - {name: 日本, type: ss, server: two.example, port: 443, cipher: aes-128-gcm, password: secret}
  - {name: 剩余流量, type: ss, server: quota.example, port: 443, cipher: aes-128-gcm, password: secret}
`)
	b := aggregateSourceFile(t, s, "备用", `proxies:
  - {name: 香港重复, type: ss, server: one.example, port: 443, cipher: aes-128-gcm, password: secret, plugin-opts: {mode: websocket, host: front.example}}
  - {name: 香港, type: ss, server: chain.example, port: 443, cipher: aes-128-gcm, password: secret, dialer-proxy: 香港重复}
`)
	spec := AggregateSpec{Sources: []AggregateSource{{ID: a.ID, Prefix: "A | "}, {ID: b.ID, Prefix: "B | "}}, Include: "香港|日本|流量", Exclude: "流量", Dedupe: true}
	raw, stats, err := s.BuildAggregate(spec)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Input != 5 || stats.Output != 3 || stats.Duplicates != 1 {
		t.Fatalf("wrong counts: %+v", stats)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	proxies := doc["proxies"].([]any)
	first := proxies[0].(map[string]any)
	if first["password"] != "secret" || first["plugin-opts"].(map[string]any)["host"] != "front.example" {
		t.Fatal("lost node fields")
	}
	chain := proxies[2].(map[string]any)
	if chain["dialer-proxy"] != "A | 香港" {
		t.Fatalf("wrong renamed dependency: %+v", chain)
	}
	if doc["rules"].([]any)[0] != "MATCH,"+AggregateSelect {
		t.Fatal("not a usable generated config")
	}
	if first["name"] != "A | 香港" || chain["name"] != "B | 香港" {
		t.Fatal("prefixes lost")
	}
	original, _ := s.Content(a.ID)
	if bytes.Contains(original, []byte("A | ")) {
		t.Fatal("source was mutated")
	}
}

func TestAggregateNameCollisionsAreStable(t *testing.T) {
	s, _ := Open(t.TempDir())
	a := aggregateSourceFile(t, s, "a", `proxies: [{name: same, type: ss, server: a.example, port: 443, password: a, cipher: aes-128-gcm}]`)
	b := aggregateSourceFile(t, s, "b", `proxies: [{name: same, type: ss, server: b.example, port: 443, password: b, cipher: aes-128-gcm}, {name: DIRECT, type: ss, server: c.example, port: 443, password: c, cipher: aes-128-gcm}]`)
	spec := AggregateSpec{Sources: []AggregateSource{{ID: a.ID}, {ID: b.ID}}}
	first, stats, err := s.BuildAggregate(spec)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := s.BuildAggregate(spec)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatal("nondeterministic result")
	}
	if strings.Join(stats.Names, ",") != "same,same (2),DIRECT (2)" {
		t.Fatalf("collision names: %v", stats.Names)
	}
}

func TestAggregateRejectsUnsupportedAndMissingDependencies(t *testing.T) {
	for name, raw := range map[string]string{
		"providers": "proxy-providers: {remote: {type: http, url: https://example.com}}",
		"mixed":     "proxy-providers: {remote: {type: http, url: https://example.com}}\nproxies: [{name: a, type: ss}]",
		"empty":     "rules: [MATCH,DIRECT]",
		"duplicate": "proxies: [{name: a, type: ss}, {name: a, type: ss}]",
		"missing":   "proxies: [{name: a, type: ss, dialer-proxy: missing}]",
		"cycle":     "proxies: [{name: a, type: ss, dialer-proxy: b}, {name: b, type: ss, dialer-proxy: a}]",
		"multi":     "proxies: [{name: a, type: ss}]\n---\nrules: [MATCH,DIRECT]",
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := Open(t.TempDir())
			p := aggregateSourceFile(t, s, name, raw)
			if _, _, err := s.BuildAggregate(AggregateSpec{Sources: []AggregateSource{{ID: p.ID}}, Dedupe: true}); err == nil {
				t.Fatal("accepted invalid source")
			}
		})
	}
	for _, spec := range []AggregateSpec{{}, {Sources: []AggregateSource{{ID: "x"}, {ID: "x"}}}, {Sources: []AggregateSource{{ID: "x"}}, Include: "["}} {
		if err := ValidateAggregateSpec(spec); err == nil {
			t.Fatal("accepted invalid spec")
		}
	}
}

func TestAggregateStoreSaveRefreshRollbackAndSourceDeletion(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	source := aggregateSourceFile(t, s, "a", `proxies: [{name: a, type: ss, server: a.example, port: 443, password: a, cipher: aes-128-gcm}]`)
	spec := AggregateSpec{Sources: []AggregateSource{{ID: source.ID}}, Dedupe: true}
	p, _, err := s.SaveAggregate("", "merged", spec, nil)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.Content(p.ID)
	if len(s.DependentAggregates(source.ID)) != 1 {
		t.Fatal("dependency missing")
	}
	if err := s.Remove(source.ID); err == nil {
		t.Fatal("deleted a referenced source")
	}
	copy := s.List()
	for _, item := range copy {
		if item.ID == p.ID {
			item.Aggregate.Sources[0].Prefix = "mutated"
		}
	}
	stored, _ := s.Get(p.ID)
	if stored.Aggregate.Sources[0].Prefix != "" {
		t.Fatal("source settings alias escaped store")
	}
	newSpec := spec
	newSpec.Include = "never-match"
	if _, _, err := s.SaveAggregate(p.ID, "changed", newSpec, nil); err == nil {
		t.Fatal("accepted empty aggregation")
	}
	after, _ := s.Content(p.ID)
	if !bytes.Equal(before, after) {
		t.Fatal("failed aggregation replaced snapshot")
	}
	if err := os.WriteFile(source.Path, []byte(`proxies: [{name: b, type: ss, server: b.example, port: 443, password: b, cipher: aes-128-gcm}]`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(source.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RefreshChecked(p.ID, func([]byte) error { return errors.New("kernel rejected") }); err == nil {
		t.Fatal("ignored rejection")
	}
	after, _ = s.Content(p.ID)
	if !bytes.Equal(before, after) {
		t.Fatal("rejected refresh replaced snapshot")
	}
	if err := s.Refresh(p.ID); err != nil {
		t.Fatal(err)
	}
	after, _ = s.Content(p.ID)
	if !bytes.Contains(after, []byte("b.example")) {
		t.Fatal("refresh did not read new source cache")
	}
	// A failed index write restores both cached YAML and in-memory settings.
	index := filepath.Join(dir, "profiles.json")
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(index, 0700); err != nil {
		t.Fatal(err)
	}
	newSpec = spec
	newSpec.Sources = append([]AggregateSource(nil), spec.Sources...)
	newSpec.Sources[0].Prefix = "new | "
	if _, _, err := s.SaveAggregate(p.ID, "changed", newSpec, nil); err == nil {
		t.Fatal("expected index write failure")
	}
	failed, _ := s.Content(p.ID)
	if !bytes.Equal(after, failed) {
		t.Fatal("index failure changed cached snapshot")
	}
	stored, _ = s.Get(p.ID)
	if stored.Name != "merged" || stored.Aggregate.Sources[0].Prefix != "" {
		t.Fatal("index failure changed metadata")
	}
}
