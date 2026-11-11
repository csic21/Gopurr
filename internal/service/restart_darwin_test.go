//go:build darwin

package service

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestKickstartCommandTargetsTheSystemService(t *testing.T) {
	want := "/bin/launchctl kickstart -k system/" + Label
	if got := kickstartCommand(); got != want {
		t.Fatalf("kickstart command = %q, want %q", got, want)
	}
}

// The fallback must reuse the one administrator authorization channel the
// installer already uses; it must never run a privileged command implicitly.
func TestKickstartUsesTheAdminChannel(t *testing.T) {
	previous := adminRun
	t.Cleanup(func() { adminRun = previous })
	var ran string
	adminRun = func(command string) error {
		ran = command
		return nil
	}
	if err := kickstart(); err != nil {
		t.Fatal(err)
	}
	if ran != kickstartCommand() {
		t.Fatalf("ran %q", ran)
	}
}

func TestControllerGoneDistinguishesMissingFromLive(t *testing.T) {
	dir, err := os.MkdirTemp("", "gp-restart-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if !controllerGone(filepath.Join(dir, "missing.sock")) {
		t.Fatal("missing socket not reported gone")
	}
	socket := filepath.Join(dir, "live.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if controllerGone(socket) {
		t.Fatal("live socket reported gone")
	}
}
