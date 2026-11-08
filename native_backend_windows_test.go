package main

import (
	"strings"
	"testing"
)

func TestNativeBackendRejectsUnsupportedWindowsHost(t *testing.T) {
	if err := runNativeBackend(nil); err == nil || !strings.Contains(err.Error(), "requires macOS") {
		t.Fatalf("unsupported native host was accepted: %v", err)
	}
}
