package main

import (
	"strings"
	"testing"
)

func TestNativeBackendRejectsIncompleteWindowsSession(t *testing.T) {
	if err := runNativeBackend(nil); err == nil || !strings.Contains(err.Error(), "absolute home, socket, token-file, frontend and parent are required") {
		t.Fatalf("incomplete Windows session was accepted: %v", err)
	}
}
