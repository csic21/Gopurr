package profile

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sampleYAML = `mixed-port: 7890
proxies:
  - {name: a, type: ss, server: 1.2.3.4, port: 443, cipher: aes-128-gcm, password: x}
proxy-groups:
  - {name: 节点选择, type: select, proxies: [a]}
rules:
  - MATCH,节点选择
`

func TestParseUserinfo(t *testing.T) {
	info := parseUserinfo("upload=1024; download=2048; total=107374182400; expire=1800000000")
	if info.Upload != 1024 || info.Download != 2048 {
		t.Fatalf("traffic: %+v", info)
	}
	if info.Total != 107374182400 {
		t.Fatalf("total: %d", info.Total)
	}
	if info.Used() != 3072 {
		t.Fatalf("used: %d", info.Used())
	}
	if info.Expire != 1800000000 {
		t.Fatalf("expire: %d", info.Expire)
	}
}

func TestParseUserinfoGarbage(t *testing.T) {
	for _, raw := range []string{"", "nonsense", "upload=abc; total=", ";;;"} {
		if info := parseUserinfo(raw); info != (Info{}) {
			t.Fatalf("%q parsed to %+v", raw, info)
		}
	}
}

func TestHostOf(t *testing.T) {
	cases := map[string]string{
		"https://example.com/api/sub?token=1": "example.com",
		"http://1.2.3.4:8080/sub":             "1.2.3.4:8080",
		"weird":                               "weird",
		"":                                    "订阅",
	}
	for in, want := range cases {
		if got := hostOf(in); got != want {
			t.Fatalf("hostOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDecodeTitle(t *testing.T) {
	if got := decodeTitle("base64:5oiR55qE6K6i6ZiF"); got != "我的订阅" {
		t.Fatalf("base64 title: %q", got)
	}
	if got := decodeTitle("plain"); got != "plain" {
		t.Fatalf("plain title: %q", got)
	}
}

func TestStoreLifecycle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("subscription-userinfo", "upload=10; download=20; total=1000; expire=1900000000")
		w.Header().Set("profile-title", "base64:5rWL6K+V6K6i6ZiF")
		if r.Header.Get("User-Agent") == "" {
			t.Error("no user agent")
		}
		_, _ = w.Write([]byte(sampleYAML))
	}))
	defer srv.Close()

	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	p, err := store.Add(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "测试订阅" {
		t.Fatalf("title from header: %q", p.Name)
	}
	if store.ActiveID() != p.ID {
		t.Fatal("first profile should become active")
	}

	body, err := store.Content(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "mixed-port") {
		t.Fatalf("stored body: %q", body)
	}

	if err := store.Refresh(p.ID); err != nil {
		t.Fatal(err)
	}
	if got := store.List()[0].Info.Total; got != 1000 {
		t.Fatalf("refreshed info: %d", got)
	}

	if err := store.SetActive("nope"); err == nil {
		t.Fatal("expected error for unknown id")
	}
	if err := store.Remove(p.ID); err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 0 || store.ActiveID() != "" {
		t.Fatal("profile not removed")
	}
}

func TestStorePersists(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.Items = append(store.Items, Profile{
		ID: "abc", Name: "n", URL: "u", File: "abc.yaml", Updated: time.Now(),
	})
	store.Active = "abc"
	err = store.save()
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}

	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if again.ActiveID() != "abc" || len(again.List()) != 1 {
		t.Fatalf("reload: %+v", again.List())
	}
}

func TestAddRejectsBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add(srv.URL, "x"); err == nil {
		t.Fatal("expected HTTP error")
	}
}

func TestAddFile(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(dir, "local.yaml")
	if err := os.WriteFile(src, []byte(sampleYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := store.AddFile(src, "")
	if err != nil {
		t.Fatal(err)
	}
	if p.Kind != KindFile || p.Path != src {
		t.Fatalf("profile = %+v", p)
	}
	if p.Name != "local" {
		t.Fatalf("name from filename = %q", p.Name)
	}
	if !store.List()[0].IsFile() {
		t.Fatal("list lost the file kind")
	}

	// Editing the source and refreshing picks the new content up.
	if err := os.WriteFile(src, []byte(sampleYAML+"# edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.Refresh(p.ID); err != nil {
		t.Fatal(err)
	}
	body, err := store.Content(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# edited") {
		t.Fatalf("refresh did not re-read the file: %q", body)
	}
}

func TestAddFileRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)

	bad := filepath.Join(dir, "bad.yaml")
	_ = os.WriteFile(bad, []byte("a: [unclosed"), 0o644)
	if _, err := store.AddFile(bad, ""); err == nil {
		t.Fatal("invalid YAML accepted")
	}

	empty := filepath.Join(dir, "empty.yaml")
	_ = os.WriteFile(empty, nil, 0o644)
	if _, err := store.AddFile(empty, ""); err == nil {
		t.Fatal("empty file accepted")
	}
}

func TestAddRejectsNonConfigBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>login required</html>"))
	}))
	defer srv.Close()

	store, _ := Open(t.TempDir())
	if _, err := store.Add(srv.URL, "x"); err == nil {
		t.Fatal("an HTML login page was accepted as a config")
	}
}

func TestOldProfilesMigrateToURLKind(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	store.Items = append(store.Items, Profile{ID: "old", Name: "n", URL: "u", File: "old.yaml"})
	store.Active = "old"
	if err := store.save(); err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	store.mu.Unlock()

	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.List()[0].Kind; got != KindURL {
		t.Fatalf("migrated kind = %q", got)
	}
}
