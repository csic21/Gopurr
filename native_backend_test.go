//go:build darwin

package main

import (
	"encoding/json"
	"gopurr/internal/patch"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeHomePreparationIsPrivateAndPreservesExistingSettings(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.Mkdir(home, 0755); err != nil {
		t.Fatal(err)
	}
	if err := prepareNativeHome(home); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(home)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("home is not private", err)
	}
	path := filepath.Join(home, "overrides.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var overrides patch.Overrides
	if err := json.Unmarshal(body, &overrides); err != nil {
		t.Fatal(err)
	}
	if overrides.MixedPort < 1 || overrides.MixedPort > 65535 || overrides.TunEnable {
		t.Fatal(overrides)
	}
	if err := prepareNativeHome(home); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(path)
	if string(again) != string(body) {
		t.Fatal("existing settings were overwritten")
	}
	info, _ = os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("overrides are not private")
	}
}
func TestNativeHomeRejectsLinkedOverrides(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "original")
	if err := os.WriteFile(target, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(home, "overrides.json")); err != nil {
		t.Fatal(err)
	}
	if err := prepareNativeHome(home); err == nil {
		t.Fatal("linked overrides were accepted")
	}
	info, _ := os.Stat(target)
	if info.Mode().Perm() != 0644 {
		t.Fatal("permissions of the linked target changed")
	}
}
func TestNativeParentChecksDirectParent(t *testing.T) {
	if !nativeParentAlive(os.Getppid()) {
		t.Fatal("live direct parent was treated as gone")
	}
	if nativeParentAlive(os.Getpid()) || nativeParentAlive(1) {
		t.Fatal("unrelated process treated as the direct parent")
	}
}
