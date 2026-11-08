package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// The production macOS entry point ships the complete Catalyst host plus its
// desktop backend. Windows retains its existing packaging path.
func buildNativeMac(config map[string]any, version, output string, release, skipDMG bool, env []string) error {
	if release && os.Getenv("MYGO_UPDATER_PRIVATE_KEY") == "" {
		return errors.New("native macOS release requires MYGO_UPDATER_PRIVATE_KEY")
	}
	if release {
		refresh := exec.Command("go", "run", "./scripts/update-geodata")
		refresh.Env, refresh.Stdout, refresh.Stderr = env, os.Stdout, os.Stderr
		if err := refresh.Run(); err != nil {
			return fmt.Errorf("refresh bundled geodata: %w", err)
		}
	}
	updates, _ := config["updates"].(map[string]any)
	publicKey, _ := updates["publicKey"].(string)
	feed := nativeFeed(updates)
	stage, err := filepath.Abs(filepath.Join(output, "darwin-arm64"))
	if err != nil {
		return err
	}
	app := filepath.Join(stage, "Gopurr.app")
	build := exec.Command("bash", "scripts/build-native-apple.sh", "catalyst")
	build.Env = append(env, "GOPURR_NATIVE_DISTRIBUTION=1", "GOPURR_NATIVE_APP_PATH="+app, "GOPURR_NATIVE_UPDATE_FEED="+feed)
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err = build.Run(); err != nil {
		return err
	}
	if !skipDMG {
		if err = nativeDMG(app, filepath.Join(stage, "gopurr-"+version+"-arm64.dmg")); err != nil {
			return err
		}
	}
	if !release {
		return nil
	}
	destination := filepath.Join("dist", "darwin-arm64")
	pack := exec.Command("go", "run", "./scripts/package-native-update", "-production", "-app", app, "-out", destination, "-sign", "-key-env", "MYGO_UPDATER_PRIVATE_KEY", "-notes", "docs/release-notes.md")
	pack.Env, pack.Stdout, pack.Stderr = env, os.Stdout, os.Stderr
	if err = pack.Run(); err != nil {
		return err
	}
	if err = verifyUpdate(destination, version, "darwin-arm64", publicKey); err != nil {
		return err
	}
	legacy, err := os.ReadFile(filepath.Join(destination, "update-darwin-arm64.json"))
	if err != nil {
		return err
	}
	native, err := os.ReadFile(filepath.Join(destination, "update-native-darwin-arm64.json"))
	if err != nil {
		return err
	}
	if !bytes.Equal(legacy, native) {
		return errors.New("legacy and native update feeds disagree")
	}
	if !skipDMG {
		return copyFile(filepath.Join(stage, "gopurr-"+version+"-arm64.dmg"), filepath.Join(destination, "gopurr-"+version+"-arm64.dmg"))
	}
	return nil
}

func nativeFeed(updates map[string]any) string {
	if base, _ := updates["url"].(string); base != "" {
		return strings.TrimRight(base, "/") + "/update-native-darwin-arm64.json"
	}
	repo, _ := updates["github"].(string)
	prefix, _ := updates["tagPrefix"].(string)
	if prefix != "" && prefix != "v" {
		return "https://github.com/" + repo + "/releases/download/" + prefix + "{version}/update-native-darwin-arm64.json"
	}
	return "https://github.com/" + repo + "/releases/latest/download/update-native-darwin-arm64.json"
}

func nativeDMG(app, output string) error {
	work, err := os.MkdirTemp(filepath.Dir(app), ".native-dmg-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	if b, err := exec.Command("/bin/cp", "-R", app, filepath.Join(work, "Gopurr.app")).CombinedOutput(); err != nil {
		return fmt.Errorf("stage native DMG: %v: %s", err, b)
	}
	if err = os.Symlink("/Applications", filepath.Join(work, "Applications")); err != nil {
		return err
	}
	b, err := exec.Command("/usr/bin/hdiutil", "create", "-volname", "Gopurr", "-srcfolder", work, "-ov", "-format", "UDZO", output).CombinedOutput()
	if err != nil {
		return fmt.Errorf("create native DMG: %v: %s", err, b)
	}
	return nil
}
