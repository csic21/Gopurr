package core

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestGeoUpdatesRetryAndPersistAcrossRestarts(t *testing.T) {
	home := t.TempDir()
	u := newGeoUpdater(home)
	p := geoUpdatePlan{Enabled: true, Interval: 24 * time.Hour, Files: []geoDatabase{{Name: "geoip.metadb", URL: "https://example.invalid/geoip"}}}
	now := time.Now().UTC().Truncate(time.Second)
	calls := 0
	u.refresh = func(context.Context, []geoDatabase) error {
		calls++
		if calls == 1 {
			return errors.New("offline")
		}
		return nil
	}
	u.check(context.Background(), now, p)
	if _, err := os.Stat(u.statePath()); !os.IsNotExist(err) {
		t.Fatalf("failed update recorded as successful: %v", err)
	}
	u.check(context.Background(), now.Add(59*time.Minute), p)
	if calls != 1 {
		t.Fatal("retried too soon")
	}
	u.check(context.Background(), now.Add(time.Hour), p)
	if calls != 2 {
		t.Fatal("worker did not retry after initial failure")
	}
	restarted := newGeoUpdater(home)
	restarted.refresh = func(context.Context, []geoDatabase) error { calls++; return nil }
	restarted.check(context.Background(), now.Add(2*time.Hour), p)
	if calls != 2 {
		t.Fatal("restart discarded last successful check")
	}
	restarted.check(context.Background(), now.Add(25*time.Hour), p)
	if calls != 3 {
		t.Fatal("daily update did not run")
	}
	p.Enabled = false
	restarted.check(context.Background(), now.Add(100*time.Hour), p)
	if calls != 3 {
		t.Fatal("ignored explicit opt-out")
	}
	p.Enabled = true
	p.Files[0].URL = "https://example.invalid/new-profile"
	restarted.check(context.Background(), now.Add(26*time.Hour), p)
	if calls != 4 {
		t.Fatal("new source waited for the old source's daily interval")
	}
}

func TestGeoDownloadFailureKeepsWorkingDatabase(t *testing.T) {
	u := newGeoUpdater(t.TempDir())
	working := []byte("last usable database")
	path := u.home + "/GeoSite.dat"
	if err := os.WriteFile(path, working, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, code := range []int{http.StatusServiceUnavailable, http.StatusOK} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			_, _ = w.Write([]byte("invalid database"))
		}))
		err := u.updateFile(context.Background(), geoDatabase{Name: "GeoSite.dat", Path: path, URL: server.URL})
		server.Close()
		if err == nil {
			t.Fatalf("accepted failed/invalid download with status %d", code)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(working) {
			t.Fatalf("working cache lost: %s %v", got, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := downloadGeo(ctx, "http://127.0.0.1:1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
