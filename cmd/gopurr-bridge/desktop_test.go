//go:build cgo && !android

package main

import (
	"context"
	"encoding/json"
	"errors"
	"gopurr/internal/nativehost"
	"gopurr/internal/securefs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDesktopStopCancelsActiveReadsAndRequestsGracefulShutdownOnce(t *testing.T) {
	temporaryRoot := ""
	if runtime.GOOS == "darwin" {
		temporaryRoot = "/tmp"
	}
	dir, err := os.MkdirTemp(temporaryRoot, "gp-stop-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if err := securefs.PrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	process := &desktopProcess{dir: dir, socket: filepath.Join(dir, "rpc"), token: strings.Repeat("s", 64), ctx: ctx, cancel: cancel, done: make(chan struct{})}
	entered := make(chan struct{})
	var stops atomic.Int32
	server, err := nativehost.Listen(process.socket, process.token, func(ctx context.Context, method string, _ json.RawMessage) (any, error) {
		if method == "host.shutdown" {
			stops.Add(1)
			return map[string]bool{"ok": true}, nil
		}
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	active := make(chan error, 1)
	go func() {
		_, err := nativehost.Call(process.ctx, process.socket, process.token, "wait", nil)
		active <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("active read not started")
	}
	var callers sync.WaitGroup
	for i := 0; i < 4; i++ {
		callers.Add(1)
		go func() { defer callers.Done(); process.stop() }()
	}
	callers.Wait()
	select {
	case err := <-active:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("close did not cancel active native read")
	}
	deadline := time.Now().Add(time.Second)
	for stops.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if stops.Load() != 1 {
		t.Fatal("graceful shutdown was not coalesced", stops.Load())
	}
	close(process.done)
}
func TestDesktopHomePreflightWaitsForRelaunchAndRejectsOtherLiveModel(t *testing.T) {
	home := t.TempDir()
	lock, err := nativehost.LockHome(home)
	if err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(60 * time.Millisecond); lock.Close() }()
	if err := desktopHomeReady(home); err != nil {
		t.Fatal("relaunch did not wait for cleanup to release home", err)
	}
	held, err := nativehost.LockHome(home)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if err := desktopHomeReady(home); !errors.Is(err, nativehost.ErrHomeInUse) {
		t.Fatal("live model was not rejected clearly", err)
	}
}
