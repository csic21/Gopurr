package nativehost

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRelaunchShowMarkerIsOneShot(t *testing.T) {
	home := t.TempDir()
	if ConsumeRelaunchShow(home) {
		t.Fatal("empty home requested a visible relaunch")
	}
	if err := MarkRelaunchShow(home); err != nil {
		t.Fatal(err)
	}
	if err := MarkRelaunchShow(home); err != nil {
		t.Fatal("replacing a pending relaunch marker:", err)
	}
	if !ConsumeRelaunchShow(home) {
		t.Fatal("relaunch marker was not observed")
	}
	if ConsumeRelaunchShow(home) {
		t.Fatal("relaunch marker survived its first launch")
	}
}

func TestRelaunchShowMarkerExpires(t *testing.T) {
	home := t.TempDir()
	if err := MarkRelaunchShow(home); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, relaunchShowFile)
	old := time.Now().Add(-relaunchShowTTL - time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if ConsumeRelaunchShow(home) {
		t.Fatal("stale relaunch marker requested a visible window")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("stale relaunch marker was not removed")
	}
}
