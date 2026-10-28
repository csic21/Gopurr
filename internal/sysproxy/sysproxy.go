// Package sysproxy reads and writes the operating system's HTTP proxy
// settings, so the kernel can be used by apps that ignore proxies.
package sysproxy

// State is what the OS currently has configured.
type State struct {
	Supported bool
	Enabled   bool
	Service   string
	Server    string
	Port      int
	// RawServer preserves Windows' per-protocol proxy configuration on restore.
	RawServer string `json:",omitempty"`
}

// Available reports whether this build can change the system proxy.
func Available() bool { return supported }

// Controller owns the proxy changes for one application session. On macOS it
// retains a narrowly scoped privileged helper, so restoring on quit never asks
// for another password. Close must succeed before the kernel is stopped.
type Controller interface {
	Enable(port int, previous *State) error
	// Restore puts the proxy back. port is the local listener that a previous
	// application session may have left in the system settings.
	Restore(port int, previous *State) error
	Close() error
}
