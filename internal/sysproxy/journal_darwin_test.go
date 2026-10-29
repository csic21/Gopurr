//go:build darwin

package sysproxy

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// A long-lived child models the watcher launched inside Enable's critical
// section. It must not keep the parent's journal lock alive across exec.
func TestJournalLockNotInheritedByWatcher(t *testing.T) {
	if os.Getenv("GOPURR_TEST_LOCK_CHILD") == "1" {
		_, _ = os.Stdout.Write([]byte("ready"))
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	path := filepath.Join(t.TempDir(), "recovery.json")
	unlock, err := lockJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	child := exec.Command(os.Args[0], "-test.run=^TestJournalLockNotInheritedByWatcher$")
	child.Env = append(os.Environ(), "GOPURR_TEST_LOCK_CHILD=1")
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stdin.Close(); _ = child.Process.Kill(); _ = child.Wait() }()
	ready := make([]byte, 5)
	if _, err := io.ReadFull(stdout, ready); err != nil {
		t.Fatal(err)
	}
	unlock()
	unlock = nil
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	release, err := lockJournalContext(ctx, path)
	if err != nil {
		t.Fatalf("watcher inherited the journal lock: %v", err)
	}
	release()
}

func TestJournalLockWaitCanTimeOutAndRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery.json")
	unlock, err := lockJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	release, err := lockJournalContext(ctx, path)
	unlock()
	if release != nil {
		release()
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contended lock did not time out: %v", err)
	}
	release, err = lockJournal(path)
	if err != nil {
		t.Fatalf("released lock cannot be retried: %v", err)
	}
	release()
}

func TestDirectProxyCrashRecoveryPreservesEveryProtocolAndBypass(t *testing.T) {
	network := newNetworkFixture()
	journal := filepath.Join(t.TempDir(), "recovery.json")
	leases, err := loadLeases(journal, network.run)
	if err != nil {
		t.Fatal(err)
	}
	if err := leases.enable(7890, nil); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(journal); err != nil || st.Mode().Perm() != 0600 {
		t.Fatalf("durable snapshot missing or public: %v", err)
	}
	// Discard the entire first controller, as SIGKILL would do. The surviving
	// watcher uses only the journal and operating system's current settings.
	if err := watchLease(context.Background(), journal, os.Getpid(), func() bool { return false }, network.run, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	network.assertOriginal(t)
	if _, err := os.Stat(journal); !os.IsNotExist(err) {
		t.Fatal("successful recovery did not remove the journal")
	}
}

func TestRecoveryJournalFailurePreventsOSWrites(t *testing.T) {
	network := newNetworkFixture()
	leases := &proxyLeases{command: network.run, services: map[string]*proxyLease{}, journal: t.TempDir()}
	if err := leases.enable(7890, nil); err == nil {
		t.Fatal("proxy applied without a durable snapshot")
	}
	network.assertOriginal(t)
}

func TestOldWatcherDoesNotRestoreNewSession(t *testing.T) {
	network := newNetworkFixture()
	journal := filepath.Join(t.TempDir(), "recovery.json")
	leases, _ := loadLeases(journal, network.run)
	if err := leases.enable(7890, nil); err != nil {
		t.Fatal(err)
	}
	if err := watchLease(context.Background(), journal, os.Getpid()+1, func() bool { return false }, network.run, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if network.proxies[0].port != 7890 {
		t.Fatal("an old watcher restored a newer session")
	}
}

func TestUnusedControllerCanCloseAfterDataDirectoryDisappears(t *testing.T) {
	dir := t.TempDir()
	c := NewControllerAt(filepath.Join(dir, "recovery.json"))
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("unused controller blocked quit: %v", err)
	}
}

func TestMalformedRecoveryJournalIsRetained(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery.json")
	for _, raw := range []string{"null", "{}", "not json"} {
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadLeases(path, newNetworkFixture().run); err == nil {
			t.Fatalf("accepted malformed recovery journal %q", raw)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal("discarded the original recovery record")
		}
	}
}

func TestRecoveredLeaseRespectsExternalChangesAndCanRetry(t *testing.T) {
	network := newNetworkFixture()
	journal := filepath.Join(t.TempDir(), "recovery.json")
	leases, _ := loadLeases(journal, network.run)
	if err := leases.enable(7890, nil); err != nil {
		t.Fatal(err)
	}
	foreign := proxySetting{true, "other.example", 9999}
	network.proxies[1] = foreign
	network.bypass = []string{"*.other"}
	network.fail = "-setwebproxy Wi-Fi http.example 8080"
	recovered, err := loadLeases(journal, network.run)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.restore(); err == nil {
		t.Fatal("fixture restore failure not reported")
	}
	recovered, err = loadLeases(journal, network.run)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.restore(); err != nil {
		t.Fatal(err)
	}
	if network.proxies[1] != foreign || !reflect.DeepEqual(network.bypass, []string{"*.other"}) ||
		network.proxies[0] != newNetworkFixture().proxies[0] {
		t.Fatal("recovery overwrote another client's settings or lost the original HTTP proxy")
	}
}
