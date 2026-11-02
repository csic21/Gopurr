package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gopurr/internal/core"
)

func TestTunBootRestoresSystemProxyAfterCompletingTransition(t *testing.T) {
	a, calls := healthTestApp(t, nil)
	a.sysApplied, a.sysOn = false, false
	status, _ := json.Marshal(core.DaemonStatus{UID: 0, Socket: filepath.Join(a.dir, "core.sock")})
	if err := os.WriteFile(filepath.Join(a.dir, "core.json"), status, 0600); err != nil {
		t.Fatal(err)
	}
	completed := make(chan struct{})
	a.requestTun(true, func() { close(completed) })
	select {
	case <-completed:
	case <-time.After(3 * time.Second):
		t.Fatal("TUN boot did not complete")
	}
	waitProxyChange(t, a)
	if calls.enable != 1 || !a.sysApplied || !a.ov.TunEnable || busyNetwork(a) {
		t.Fatalf("TUN boot did not restore proxy: enable=%d applied=%v tun=%v busy=%v error=%s", calls.enable, a.sysApplied, a.ov.TunEnable, busyNetwork(a), a.sysErrText())
	}
}
