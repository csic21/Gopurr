package kernel

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartupReportsExitImmediatelyWithCoreOutput(t *testing.T) {
	m := &Manager{logPath: filepath.Join(t.TempDir(), "core.log")}
	if err := os.WriteFile(m.logPath, []byte("bind: address already in use"), 0600); err != nil {
		t.Fatal(err)
	}
	exit := &processExit{done: make(chan struct{}), err: errors.New("exit status 1")}
	close(exit.done)
	start := time.Now()
	err := m.waitReady(exit, 10*time.Second)
	if err == nil || !strings.Contains(err.Error(), "address already in use") || time.Since(start) > time.Second {
		t.Fatalf("early exit not reported promptly: %v", err)
	}
}

func TestHiddenWindowReducesConnectionPolling(t *testing.T) {
	r := NewRemote("unused", "secret")
	r.SetUIActive(false)
	if r.connectionPollInterval() < 10*time.Second {
		t.Fatal("hidden window still polls frequently")
	}
	r.SetUIActive(true)
	if r.connectionPollInterval() != 2*time.Second {
		t.Fatal("reopened window did not resume live polling")
	}
}
