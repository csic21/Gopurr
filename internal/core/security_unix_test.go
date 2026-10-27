//go:build !windows

package core_test

import "testing"

func assertNoNamedPipe(*testing.T, string) {}
