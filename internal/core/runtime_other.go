//go:build !darwin && !windows

package core

// TUN services are not offered on these platforms.
func checkTunRoutes(int) error { return nil }
