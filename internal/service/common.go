package service

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const Label = "io.github.gopurr.core"

// LogTail explains a daemon that is installed but not answering. The installer
// copies the protected log into the user's data directory so the interface can
// show why the kernel stopped instead of guessing "no administrator rights".
func LogTail(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(dataDir, "service.log"))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var last string
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if last == "" {
			last = line
		}
		if strings.Contains(line, "level=error") || strings.Contains(line, "core:") {
			last = line
			break
		}
	}
	return shorten(last, 160)
}

func shorten(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}

func WaitInstalled(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if Installed() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func InstallDescription() string {
	if runtime.GOOS == "windows" {
		return "会将内核复制到 Program Files\\Gopurr\\Service，并注册 Gopurr Core Windows 系统服务，以 SYSTEM 身份运行。\n\n只有 TUN 需要它；不安装也能使用系统代理。可随时在这里卸载。\n\nWindows 会显示 UAC 管理员授权。"
	}
	return "会写入 /Library/LaunchDaemons/io.github.gopurr.core.plist，并让内核以 root 常驻运行。\n\n只有开启 TUN 才需要它；不安装也能正常使用系统代理。随时可以在这里卸载。\n\nmacOS 会要求一次管理员密码。"
}

func UninstallDescription() string {
	if runtime.GOOS == "windows" {
		return "会停止并删除 Gopurr Core Windows 系统服务及其受保护的内核副本，需要 UAC 管理员授权。卸载后 TUN 会关闭，订阅与设置保留。"
	}
	return "会停止并删除 /Library/LaunchDaemons/io.github.gopurr.core.plist，需要一次管理员授权。卸载后 TUN 会关闭，系统代理不受影响。"
}

func AuthorizationHint() string {
	if runtime.GOOS == "windows" {
		return "正在安装服务，请在 Windows UAC 中确认授权…"
	}
	return "正在安装服务，macOS 会要求一次管理员密码…"
}
