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
