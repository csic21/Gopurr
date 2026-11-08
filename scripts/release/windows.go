package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Opt-in until the actual Windows UI, migration and signed update acceptance
// workflow has passed. A normal release cannot select an unvalidated host.
func buildNativeWindows(config map[string]any, version, output string, release bool, env []string) error {
	if runtime.GOOS != "windows" {
		return errors.New("NSH Windows packaging requires a Windows x64 builder")
	}
	if release && os.Getenv("MYGO_UPDATER_PRIVATE_KEY") == "" {
		return errors.New("native Windows release requires MYGO_UPDATER_PRIVATE_KEY")
	}
	if release {
		refresh := exec.Command("go", "run", "./scripts/update-geodata")
		refresh.Env, refresh.Stdout, refresh.Stderr = env, os.Stdout, os.Stderr
		if err := refresh.Run(); err != nil {
			return fmt.Errorf("refresh bundled geodata: %w", err)
		}
	}
	stage, err := filepath.Abs(filepath.Join(output, "windows-amd64"))
	if err != nil {
		return err
	}
	build := exec.Command("pwsh", "-NoProfile", "-File", "scripts/build-native-windows.ps1", "-Output", stage, "-Version", version)
	build.Env, build.Stdout, build.Stderr = env, os.Stdout, os.Stderr
	if err = build.Run(); err != nil {
		return err
	}
	destination := filepath.Join("dist", "windows-amd64")
	args := []string{"-NoProfile", "-File", "scripts/package-native-windows.ps1", "-App", stage, "-Output", destination}
	if release {
		args = append(args, "-Release")
	}
	pack := exec.Command("pwsh", args...)
	pack.Env, pack.Stdout, pack.Stderr = env, os.Stdout, os.Stderr
	if err = pack.Run(); err != nil {
		return err
	}
	if !release {
		return nil
	}
	updates, _ := config["updates"].(map[string]any)
	key, _ := updates["publicKey"].(string)
	if err = verifyUpdate(destination, version, "windows-amd64", key); err != nil {
		return err
	}
	legacy, err := os.ReadFile(filepath.Join(destination, "update-windows-amd64.json"))
	if err != nil {
		return err
	}
	native, err := os.ReadFile(filepath.Join(destination, "update-native-windows-amd64.json"))
	if err != nil {
		return err
	}
	if !bytes.Equal(legacy, native) {
		return errors.New("legacy and native Windows feeds disagree")
	}
	return nil
}
