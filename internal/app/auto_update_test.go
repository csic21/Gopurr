package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestAutoUpdateRefreshesWhenDue(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPURR_HOME", dir)

	var hits int32
	body := "mixed-port: 7890\nrules:\n  - MATCH,DIRECT\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	a := New()
	if _, err := a.store.Add(srv.URL, "测试订阅"); err != nil {
		t.Fatal(err)
	}
	atomic.StoreInt32(&hits, 0) // Add already fetched once

	// Not due yet: nothing is fetched.
	p := a.Prefs()
	p.AutoUpdateHours = 6
	p.LastAutoUpdate = time.Now()
	a.UpdatePrefs(p)
	a.maybeAutoUpdate()
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Fatalf("fetched %d times before the interval passed", got)
	}

	// Seven hours later it is due.
	p.LastAutoUpdate = time.Now().Add(-7 * time.Hour)
	a.UpdatePrefs(p)
	a.maybeAutoUpdate()
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("fetched %d times, want 1", got)
	}
	if last := a.Prefs().LastAutoUpdate; time.Since(last) > time.Minute {
		t.Fatalf("last update not recorded: %v", last)
	}

	// The clock restarts, so a second call right away does nothing.
	a.maybeAutoUpdate()
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("fetched again immediately: %d", got)
	}
}

func TestAutoUpdateSkipsLocalFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GOPURR_HOME", dir)

	src := filepath.Join(dir, "local.yaml")
	if err := os.WriteFile(src, []byte("mixed-port: 7890\nrules:\n  - MATCH,DIRECT\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := New()
	if _, err := a.store.AddFile(src, ""); err != nil {
		t.Fatal(err)
	}

	p := a.Prefs()
	p.AutoUpdateHours = 1
	p.LastAutoUpdate = time.Now().Add(-2 * time.Hour)
	a.UpdatePrefs(p)

	before := a.store.List()[0].Updated
	a.maybeAutoUpdate()

	after := a.store.List()[0].Updated
	if !after.Equal(before) {
		t.Fatalf("local file was re-imported by the scheduler: %v -> %v", before, after)
	}
}
