//go:build darwin

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShellCommandQuotesArguments(t *testing.T) {
	cmd := shellCommand("install", "/Applications/My Clash/bin/Gopurr", "/Users/a b/Library/Application Support/gopurr")

	if !strings.HasPrefix(cmd, "set -- 'install'") {
		t.Fatalf("action not first: %q", cmd[:80])
	}
	if !strings.Contains(cmd, `'/Applications/My Clash/bin/Gopurr'`) {
		t.Fatalf("binary not quoted: %s", cmd)
	}
	if !strings.Contains(cmd, `'/Users/a b/Library/Application Support/gopurr'`) {
		t.Fatalf("data dir not quoted: %s", cmd)
	}
	if !strings.Contains(cmd, "launchctl bootstrap") {
		t.Fatal("script body missing")
	}
}

func TestShellCommandEscapesApostrophes(t *testing.T) {
	cmd := shellCommand("install", "/tmp/it's/bin", "/tmp/d")
	if !strings.Contains(cmd, `'/tmp/it'\''s/bin'`) {
		t.Fatalf("apostrophe not escaped: %s", cmd)
	}
}

func TestAppleScriptEscaping(t *testing.T) {
	script := appleScript(`echo "hi" \ ok`)

	if !strings.HasPrefix(script, `do shell script "`) {
		t.Fatalf("not an AppleScript: %s", script)
	}
	if !strings.HasSuffix(script, `" with administrator privileges`) {
		t.Fatalf("no privileges clause: %s", script)
	}
	if !strings.Contains(script, `\"hi\"`) {
		t.Fatalf("inner quotes not escaped: %s", script)
	}
	if strings.Contains(script, `"hi"`) {
		t.Fatalf("raw inner quotes survived: %s", script)
	}
	if !strings.Contains(script, `\\`) {
		t.Fatalf("backslash not escaped: %s", script)
	}
}

func TestInstalled(t *testing.T) {
	old := plistDir
	defer func() { plistDir = old }()

	plistDir = t.TempDir()
	if Installed() {
		t.Fatal("empty dir reported as installed")
	}

	path := PlistPath()
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Installed() {
		t.Fatal("plist not detected")
	}
	if filepath.Base(path) != Label+".plist" {
		t.Fatalf("plist path = %s", path)
	}
}

func TestInstallRejectsRelativePaths(t *testing.T) {
	err := Install("bin/Gopurr", "/tmp/x")
	if err == nil || !strings.Contains(err.Error(), "绝对路径") {
		t.Fatalf("relative binary accepted: %v", err)
	}
	err = Install("/bin/sh", "relative/dir")
	if err == nil || !strings.Contains(err.Error(), "绝对路径") {
		t.Fatalf("relative data dir accepted: %v", err)
	}
}

func TestUninstallRejectsRelativePaths(t *testing.T) {
	if err := Uninstall("relative"); err == nil {
		t.Fatal("relative data dir accepted")
	}
}

func TestShellCommandOmitsEmptyArgs(t *testing.T) {
	cmd := shellCommand("uninstall", "", "/tmp/d")
	if strings.Contains(cmd, "set -- 'uninstall' ''") {
		t.Fatalf("empty binary argument emitted: %q", cmd[:60])
	}
	if !strings.Contains(cmd, `set -- 'uninstall' '/tmp/d'`) {
		t.Fatalf("uninstall args wrong: %q", cmd[:60])
	}
}
