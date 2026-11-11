//go:build !windows

package core

import (
	"os"
	"path/filepath"
	"testing"
)

// The controller socket is published as a symlink into a private temporary
// directory. When system cleanup removes that directory the link dangles and
// every client fails; the listener must be able to notice.
func TestControllerListenerReportsLostPublishedLink(t *testing.T) {
	dir := t.TempDir()
	socket := filepath.Join(dir, "core.sock")
	listener, err := listenController(socket, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	guard, ok := listener.(interface{ Intact() bool })
	if !ok {
		t.Fatal("controller listener cannot report its published link")
	}
	if !guard.Intact() {
		t.Fatal("fresh controller link reported lost")
	}
	target, err := os.Readlink(socket)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Dir(target)); err != nil {
		t.Fatal(err)
	}
	if guard.Intact() {
		t.Fatal("deleted temporary directory still reported intact")
	}
}
