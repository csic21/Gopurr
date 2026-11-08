package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProfileActivationWorkflowRestoresAcceptedKernelOnIndexFailure(t *testing.T) {
	nativeDispatchFixture(t)
	a, _ := newSettingsTestApp(t, nil)
	oldPath, nextPath := filepath.Join(a.dir, "old-source.yaml"), filepath.Join(a.dir, "next-source.yaml")
	os.WriteFile(oldPath, []byte("rules: [\"DOMAIN,old.example,DIRECT\",\"MATCH,DIRECT\"]\n"), 0600)
	os.WriteFile(nextPath, []byte("rules: [\"DOMAIN,next.example,DIRECT\",\"MATCH,DIRECT\"]\n"), 0600)
	old, _ := a.store.AddFile(oldPath, "Old")
	next, _ := a.store.AddFile(nextPath, "Next")
	if err := a.activateManagedProfile(context.Background(), old.ID); err != nil {
		t.Fatal(err)
	}
	accepted, _ := os.ReadFile(a.mgr.ConfigPath())
	index := filepath.Join(a.dir, "profiles.json")
	os.Remove(index)
	os.Mkdir(index, 0700)
	if err := a.activateManagedProfile(context.Background(), next.ID); err == nil {
		t.Fatal("failed index commit was accepted")
	}
	restored, _ := os.ReadFile(a.mgr.ConfigPath())
	if a.store.ActiveID() != old.ID || string(restored) != string(accepted) || !a.k.Running() {
		t.Fatal("activation lost the previously accepted runtime")
	}
}
func TestNativeCanceledReimportReleasesReservationAndPreservesSource(t *testing.T) {
	nativeDispatchFixture(t)
	a, _ := newSettingsTestApp(t, nil)
	var block atomic.Bool
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if block.Load() {
			close(entered)
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte("rules: [\"MATCH,DIRECT\"]\n# original\n"))
	}))
	defer server.Close()
	nativeInvoke(t, a, "desktop.profiles.import", map[string]any{"url": server.URL, "name": "Source"})
	id := a.store.ActiveID()
	before, _ := a.store.Content(id)
	accepted, _ := os.ReadFile(a.mgr.ConfigPath())
	block.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := a.NativeCall(ctx, "desktop.profiles.import", []byte(`{"url":"`+server.URL+`","name":"Updated"}`))
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("reimport did not begin")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("native reimport did not cancel")
	}
	after, _ := a.store.Content(id)
	runtime, _ := os.ReadFile(a.mgr.ConfigPath())
	if busyNetwork(a) || a.store.ActiveID() != id || string(after) != string(before) || string(runtime) != string(accepted) || !strings.Contains(string(after), "original") {
		t.Fatal("canceled reimport changed accepted state or retained reservation")
	}
}
