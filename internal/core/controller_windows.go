package core

import (
	"net"
	"os"
)

func privateControllerDir() (string, error) { return os.MkdirTemp("", "gp-core-") }

// The socket inherits the private data directory's owner/SYSTEM/admin DACL.
// The bearer check remains mandatory on Windows as on Unix.
func listenController(path, _ string) (net.Listener, error) {
	// Windows leaves AF_UNIX socket files behind after a process exits.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return net.Listen("unix", path)
}
