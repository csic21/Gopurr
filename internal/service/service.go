//go:build darwin

// Package service installs the optional LaunchDaemon that lets the kernel own
// a TUN device.
//
// Installation and removal ask for administrator rights only after an explicit
// user action. The separate system-proxy helper is scoped to the UI session.
package service

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

//go:embed install.sh
var installScript string

func Available() bool { return true }

// plistDir is a variable so tests can point it at a temp directory.
var plistDir = "/Library/LaunchDaemons"

// PlistPath is where launchd looks for the daemon.
func PlistPath() string { return filepath.Join(plistDir, Label+".plist") }

// Installed reports whether the daemon is registered. It says nothing about
// whether it is currently answering.
func Installed() bool {
	if !Available() {
		return false
	}
	_, err := os.Stat(PlistPath())
	return err == nil
}

// Install registers the daemon and starts it. The caller should have asked the
// user first: installing a persistent service needs administrator authorization.
func Install(binary, dataDir string) error {
	if !Available() {
		return errors.New("当前平台不支持安装 macOS TUN 服务")
	}
	if !filepath.IsAbs(binary) || !filepath.IsAbs(dataDir) {
		return errors.New("安装服务需要绝对路径")
	}
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("可执行文件不可用: %w", err)
	}
	return runAsAdmin(shellCommand("install", binary, dataDir))
}

// Uninstall removes the daemon and its plist.
func Uninstall(dataDir string) error {
	if !Available() {
		return errors.New("当前平台不支持卸载 macOS TUN 服务")
	}
	if dataDir != "" && !filepath.IsAbs(dataDir) {
		return errors.New("数据目录必须是绝对路径")
	}
	return runAsAdmin(shellCommand("uninstall", "", dataDir))
}

// shellCommand builds the exact shell text root will run. Kept separate from
// execution so the quoting can be tested.
func shellCommand(action, binary, dataDir string) string {
	var b strings.Builder
	b.WriteString("set -- ")
	b.WriteString(shellQuote(action))
	if binary != "" {
		b.WriteString(" ")
		b.WriteString(shellQuote(binary))
	}
	if dataDir != "" {
		b.WriteString(" ")
		b.WriteString(shellQuote(dataDir))
	}
	b.WriteString("\n")
	b.WriteString(installScript)
	return b.String()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// appleScript wraps the command in the one authorization prompt macOS shows.
func appleScript(command string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(command)
	return fmt.Sprintf("do shell script \"%s\" with administrator privileges", escaped)
}

func runAsAdmin(command string) error {
	if strings.ContainsAny(command, "\x00") {
		return errors.New("命令包含非法字符")
	}

	cmd := exec.Command("osascript", "-e", appleScript(command))
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err == nil {
		return nil
	}
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(lower, "cancel"), strings.Contains(text, "-128"):
		return errors.New("已取消授权")
	case strings.Contains(lower, "not permitted"):
		return fmt.Errorf("系统拒绝了该操作: %s", text)
	}
	return fmt.Errorf("%s: %w", text, err)
}
