// Package sysproxy reads and writes the operating system's HTTP proxy
// settings for applications that honor the operating system's proxy settings.
package sysproxy

import "strconv"

type Endpoint struct {
	Enabled bool
	Server  string
	Port    int
}

// State is what the OS currently has configured.
type State struct {
	Supported bool
	Enabled   bool
	Service   string
	Server    string
	Port      int
	// RawServer preserves Windows' per-protocol proxy configuration on restore.
	RawServer string `json:",omitempty"`
	// WindowsOptions marks a complete WinINet snapshot, including disabled
	// manual settings, PAC, auto-detection and bypass rules.
	WindowsOptions bool   `json:",omitempty"`
	Flags          uint32 `json:",omitempty"`
	Bypass         string `json:",omitempty"`
	PAC            string `json:",omitempty"`
	// macOS has independent HTTP, HTTPS and SOCKS settings. Keep State
	// comparable, including complete observations used by the watchdog.
	ProtocolsKnown bool        `json:",omitempty"`
	Protocols      [3]Endpoint `json:",omitempty"`
	PACEnabled     bool        `json:",omitempty"`
	AutoDiscovery  bool        `json:",omitempty"`
}

func (s State) OwnedBy(port int) bool {
	if s.WindowsOptions && s.Flags != 3 {
		return false
	}
	if s.ProtocolsKnown {
		if s.PACEnabled || s.AutoDiscovery {
			return false
		}
		for _, p := range s.Protocols {
			if !p.Enabled || p.Server != "127.0.0.1" || p.Port != port {
				return false
			}
		}
	}
	return s.Enabled && s.Server == "127.0.0.1" && s.Port == port &&
		(s.RawServer == "" || s.RawServer == "127.0.0.1:"+strconv.Itoa(port))
}

func Recover(c Controller) error {
	if r, ok := c.(interface{ Recover() error }); ok {
		return r.Recover()
	}
	return nil
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
