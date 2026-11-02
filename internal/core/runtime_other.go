//go:build !darwin && !windows

package core

import "net/netip"

// TUN services are not offered on these platforms.
func checkTunRoutes(int, []netip.Prefix) error { return nil }
