package core

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeListener struct{ intact bool }

func (f *fakeListener) Accept() (net.Conn, error) { return nil, errors.New("closed") }
func (f *fakeListener) Close() error              { return nil }
func (f *fakeListener) Addr() net.Addr            { return nil }
func (f *fakeListener) Intact() bool              { return f.intact }

func withWatchInterval(t *testing.T, interval time.Duration) {
	t.Helper()
	previous := controllerWatchInterval
	controllerWatchInterval = interval
	t.Cleanup(func() { controllerWatchInterval = previous })
}

// A daemon whose published socket no longer resolves must exit so launchd (or
// the app's watchdog) can start a clean copy with fresh sockets.
func TestWatchControllerSocketsRestartsOnLostLink(t *testing.T) {
	withWatchInterval(t, 5*time.Millisecond)
	backend := filepath.Join(t.TempDir(), "backend.sock")
	if err := os.WriteFile(backend, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	defer close(done)
	stop := make(chan os.Signal, 1)
	go watchControllerSockets(done, stop, &controllerServer{listener: &fakeListener{}}, backend)
	select {
	case <-stop:
	case <-time.After(time.Second):
		t.Fatal("lost controller link did not request a restart")
	}
}

func TestWatchControllerSocketsRestartsOnLostBackend(t *testing.T) {
	withWatchInterval(t, 5*time.Millisecond)
	done := make(chan struct{})
	defer close(done)
	stop := make(chan os.Signal, 1)
	go watchControllerSockets(done, stop, &controllerServer{listener: &fakeListener{intact: true}}, filepath.Join(t.TempDir(), "missing.sock"))
	select {
	case <-stop:
	case <-time.After(time.Second):
		t.Fatal("missing backend socket did not request a restart")
	}
}

func TestWatchControllerSocketsKeepsHealthyDaemon(t *testing.T) {
	withWatchInterval(t, 5*time.Millisecond)
	backend := filepath.Join(t.TempDir(), "backend.sock")
	if err := os.WriteFile(backend, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	stop := make(chan os.Signal, 1)
	go watchControllerSockets(done, stop, &controllerServer{listener: &fakeListener{intact: true}}, backend)
	select {
	case <-stop:
		t.Fatal("healthy daemon requested a restart")
	case <-time.After(50 * time.Millisecond):
	}
	close(done)
}
