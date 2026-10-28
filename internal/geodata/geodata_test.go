package geodata

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBaselineIntegrityAndOfflineRepair(t *testing.T) {
	var manifest struct {
		SHA256 map[string]string `json:"sha256"`
	}
	raw, err := os.ReadFile("data/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	// A failed initial download may already have left a truncated cache.
	if err := os.WriteFile(filepath.Join(home, "geoip.metadb"), []byte("truncated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(home); err != nil {
		t.Fatal(err)
	}
	for name := range Files {
		data, err := os.ReadFile(filepath.Join(home, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := Validate(name, data); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := fmtHash(data); got != manifest.SHA256[name] {
			t.Fatalf("%s hash %s", name, got)
		}
		if err := Validate(name, data[:len(data)/2]); err == nil {
			t.Fatalf("truncated %s accepted", name)
		}
	}
	// An older valid database is still a working cache, not something the
	// bundled baseline should overwrite every time the application launches.
	old := time.Unix(1700000000, 0)
	for name := range Files {
		if err := os.Chtimes(filepath.Join(home, name), old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := Ensure(home); err != nil {
		t.Fatal(err)
	}
	for name := range Files {
		info, err := os.Stat(filepath.Join(home, name))
		if err != nil || !info.ModTime().Equal(old) {
			t.Fatalf("working %s replaced: %v %v", name, info, err)
		}
	}
}

func fmtHash(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func TestExistingMMDBAliasAndSymlink(t *testing.T) {
	home := t.TempDir()
	data, err := Bundled("geoip.metadb")
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(home, "Country.mmdb")
	if err := os.WriteFile(alias, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Ensure(home); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "geoip.metadb")); !os.IsNotExist(err) {
		t.Fatalf("alias ignored: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "database")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, alias); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := Ensure(home); err == nil {
		t.Fatal("followed database symlink")
	}
	got, _ := os.ReadFile(outside)
	if !bytes.Equal(got, []byte("outside")) {
		t.Fatal("changed symlink target")
	}
}

func TestDefaultsPreserveExplicitPolicyAndSources(t *testing.T) {
	doc := map[string]any{}
	Defaults(doc)
	if doc["geo-auto-update"] != true || doc["geo-update-interval"] != 24 {
		t.Fatalf("defaults: %v", doc)
	}
	urls := doc["geox-url"].(map[string]any)
	if len(urls) != 4 || len(Sources(urls["mmdb"].(string))) != 2 {
		t.Fatalf("sources: %v", urls)
	}
	custom := "https://example.invalid/custom.mmdb"
	doc = map[string]any{"geo-auto-update": false, "geo-update-interval": 12, "geox-url": map[string]any{"mmdb": custom}}
	Defaults(doc)
	if doc["geo-auto-update"] != false || doc["geo-update-interval"] != 12 || doc["geox-url"].(map[string]any)["mmdb"] != custom {
		t.Fatalf("overwrote custom settings: %v", doc)
	}
	if got := Sources(custom); len(got) != 1 || got[0] != custom {
		t.Fatalf("custom dataset replaced: %v", got)
	}
}
