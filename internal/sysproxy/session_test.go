package sysproxy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newSessionFixture(t *testing.T) (*sessionController, *State) {
	t.Helper()
	state := &State{Supported: true, WindowsOptions: true, Flags: 13, PAC: "https://pac.example/config", Bypass: "intranet;*.corp", RawServer: "http=old:80;https=secure:443"}
	c := &sessionController{path: filepath.Join(t.TempDir(), "recovery.json"), owner: 100, started: 1000,
		current: func() (State, error) { return *state, nil }, write: func(s State) error { *state = s; return nil },
		lock: func() (func(), error) { return func() {}, nil }, alive: func(int, uint64) bool { return false }, watch: func() error { return nil }}
	return c, state
}

func TestProxySessionRestoresPACAutoDetectBypassAndDisabledManualSettings(t *testing.T) {
	c, state := newSessionFixture(t)
	original := *state
	if err := c.Enable(7890, nil); err != nil {
		t.Fatal(err)
	}
	if state.PAC != "" || state.Flags != 3 || state.Port != 7890 {
		t.Fatalf("manual settings not exclusive: %+v", state)
	}
	if err := c.Enable(7891, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if !sameWindowsOptions(*state, original) {
		t.Fatalf("snapshot not fully restored: %+v", state)
	}
	if _, err := os.Stat(c.path); !os.IsNotExist(err) {
		t.Fatalf("journal retained: %v", err)
	}
}

func TestProxyCrashRecoveryDoesNotOverwriteNewerOwner(t *testing.T) {
	for _, foreign := range []bool{false, true} {
		c, state := newSessionFixture(t)
		original := *state
		if err := c.Enable(7890, nil); err != nil {
			t.Fatal(err)
		}
		if foreign {
			*state = windowsManualProxy("another-client", 9000)
		}
		beforeRecovery := *state
		if err := c.Recover(); err != nil {
			t.Fatal(err)
		}
		want := original
		if foreign {
			want = beforeRecovery
		}
		if !sameWindowsOptions(*state, want) {
			t.Fatalf("foreign=%v restored wrong owner: %+v", foreign, state)
		}
	}
}

func TestProxySessionTakeoverKeepsOriginalSnapshot(t *testing.T) {
	c, state := newSessionFixture(t)
	original := *state
	if err := c.Enable(7890, nil); err != nil {
		t.Fatal(err)
	}
	c.owner, c.started, c.watched = 101, 1001, false
	if err := c.Enable(7891, nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Restore(7891, nil); err != nil {
		t.Fatal(err)
	}
	if !sameWindowsOptions(*state, original) {
		t.Fatal("takeover saved the stale loopback proxy as original")
	}
}

func TestProxySessionRetainsRecoveryAfterPartialWriteAndRollbackFailure(t *testing.T) {
	c, state := newSessionFixture(t)
	c.write = func(s State) error { *state = s; return errors.New("write/notify failed") }
	if c.Enable(7890, nil) == nil {
		t.Fatal("partial write succeeded")
	}
	if _, err := os.Stat(c.path); err != nil {
		t.Fatal("lost recovery journal")
	}
	c.write = func(s State) error { *state = s; return nil }
	if err := c.Recover(); err != nil {
		t.Fatal(err)
	}
}

func TestProxySessionWatcherFailureDoesNotChangeOSSettings(t *testing.T) {
	c, state := newSessionFixture(t)
	original := *state
	c.watch = func() error { return errors.New("spawn failed") }
	if c.Enable(7890, nil) == nil || !sameWindowsOptions(*state, original) {
		t.Fatal("changed settings without a recovery watcher")
	}
}
