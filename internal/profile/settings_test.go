package profile

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditKeepsCachedConfigAndPersistsSourceSettings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("rules: [MATCH,DIRECT]\n")) }))
	defer srv.Close()
	dir := t.TempDir()
	s, _ := Open(dir)
	p, err := s.Add(srv.URL, "原订阅")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.Content(p.ID)
	hours := 3
	p.Name = "工作订阅"
	p.URL = srv.URL + "/new"
	p.UserAgent = "Gopurr-test"
	p.UpdateHours = &hours
	p.UseProxy = true
	if err := s.Edit(p); err != nil {
		t.Fatal(err)
	}
	again, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := again.List()[0]
	after, _ := again.Content(p.ID)
	if got.ID != p.ID || got.URL != p.URL || got.UserAgent != p.UserAgent || !got.UseProxy || got.UpdateHours == nil || *got.UpdateHours != 3 || again.ActiveID() != p.ID || string(before) != string(after) {
		t.Fatalf("edit lost state: %+v", got)
	}
	// A failed metadata save must not publish the candidate.
	s.dir = filepath.Join(dir, "missing")
	p.Name = "不能保存"
	if err := s.Edit(p); err == nil {
		t.Fatal("expected persistence failure")
	}
	if s.List()[0].Name != "工作订阅" {
		t.Fatal("failed edit replaced live metadata")
	}
}
func TestRefreshUsesSelectedProxyAndUA(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("rules: [MATCH,DIRECT]")) }))
	defer source.Close()
	s, _ := Open(t.TempDir())
	p, err := s.Add(source.URL, "测试")
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.UserAgent() != "custom-client/2" || r.URL.String() != "http://unreachable.invalid/sub" {
			t.Errorf("proxy request: %s %s", r.URL, r.UserAgent())
		}
		seen = true
		w.Write([]byte("rules: [MATCH,REJECT]"))
	}))
	defer proxy.Close()
	endpoint, _ := url.Parse(proxy.URL)
	s.SetProxyURL(func() *url.URL { return endpoint })
	p.URL = "http://unreachable.invalid/sub"
	p.UserAgent = "custom-client/2"
	p.UseProxy = true
	if err := s.Edit(p); err != nil {
		t.Fatal(err)
	}
	if err := s.Refresh(p.ID); err != nil {
		t.Fatal(err)
	}
	b, _ := s.Content(p.ID)
	if !seen || !strings.Contains(string(b), "REJECT") {
		t.Fatal("did not use selected proxy")
	}
	s.SetProxyURL(func() *url.URL { return nil })
	if err := s.Refresh(p.ID); err == nil {
		t.Fatal("silently fell back to direct")
	}
}
func TestInvalidProfileSettingsAndBackupLocalSource(t *testing.T) {
	hours := -1
	for _, p := range []Profile{{Name: "x", URL: "file:///tmp/x"}, {Name: "x", URL: "http://example.com", UserAgent: "bad\r\nUA"}, {Name: "x", URL: "http://example.com", UpdateHours: &hours}} {
		if ValidateSettings(p) == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "source.yaml")
	os.WriteFile(file, []byte("rules: [MATCH,DIRECT]"), 0600)
	s, _ := Open(dir)
	p, err := s.AddFile(file, "")
	if err != nil {
		t.Fatal(err)
	}
	s.Items[0].Path = ""
	os.Remove(file)
	if err := s.Refresh(p.ID); err != nil {
		t.Fatal("self-contained local config:", err)
	}
}

func TestRejectedRefreshLeavesCachedContentAndMetadata(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("rules: [MATCH,DIRECT]")) }))
	defer source.Close()
	s, _ := Open(t.TempDir())
	p, err := s.Add(source.URL, "原订阅")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.Content(p.ID)
	if err := s.RefreshChecked(p.ID, func([]byte) error { return fmt.Errorf("candidate rejected") }); err == nil {
		t.Fatal("expected rejection")
	}
	after, _ := s.Content(p.ID)
	if string(after) != string(before) || !s.List()[0].Updated.Equal(p.Updated) {
		t.Fatal("rejection changed cache or timestamp")
	}
}
