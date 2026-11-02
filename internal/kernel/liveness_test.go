package kernel

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunningUsesFreshControllerEvidence(t *testing.T) {
	var probes atomic.Int32
	entered := make(chan struct{}, 1)
	manager, remote := uxController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			probes.Add(1)
			select {
			case entered <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			return
		}
		w.Write([]byte("{}"))
	}))
	if _, err := remote.Runtime(t.Context()); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for i := 0; i < 100; i++ {
		if !manager.Running() {
			t.Fatal("live controller shown as stopped")
		}
	}
	if probes.Load() != 0 || time.Since(start) > 100*time.Millisecond {
		t.Fatalf("render path probed despite live data: probes=%d elapsed=%v", probes.Load(), time.Since(start))
	}
	// The cache is bounded. Old evidence must not hide a real outage.
	remote.lastSeen.Store(time.Now().Add(-11 * time.Second).UnixNano())
	start = time.Now()
	if manager.Running() {
		t.Fatal("stale evidence hid outage")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("expired cache blocked rendering")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("background probe did not start")
	}
	if probes.Load() != 1 {
		t.Fatalf("probes=%d", probes.Load())
	}
	for i := 0; i < 100; i++ {
		manager.Running()
	}
	if probes.Load() != 1 {
		t.Fatalf("failed probe repeated per widget: %d", probes.Load())
	}
}

func TestRejectedControllerDoesNotRefreshLiveness(t *testing.T) {
	_, remote := uxController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	if remote.Probe(context.Background()) == nil {
		t.Fatal("unauthorized probe succeeded")
	}
	if _, err := remote.Runtime(t.Context()); err == nil {
		t.Fatal("unauthorized runtime succeeded")
	}
	if remote.recentlyAlive() {
		t.Fatal("authentication failure treated as running")
	}
}

func TestTransportResetDropsLiveness(t *testing.T) {
	remote := NewRemote("unused", "secret")
	remote.markAlive()
	remote.resetTransport()
	if remote.recentlyAlive() {
		t.Fatal("stopped daemon retained live state")
	}
}

func TestRunningForChangeChecksColdLiveController(t *testing.T) {
	manager, _ := uxController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{}"))
	}))
	k := New(manager)
	if !k.RunningForChange() {
		t.Fatal("configuration change skipped a live cold controller")
	}
	if !k.Running() {
		t.Fatal("successful check did not refresh display state")
	}
}
