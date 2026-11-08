//go:build !android

package engine

import (
	"context"
	"errors"
)

func mobileVPNSupported() bool         { return false }
func setVPNProtector(_ func(int) bool) {}
func duplicateVPNDescriptor(_ int) (int, error) {
	return -1, errors.New("VPN requires an Android native host")
}
func closeVPNDescriptor(_ int) {}
func probeMixedVPN(_ context.Context, _ int) error {
	return errors.New("VPN requires an Android native host")
}
