package service

import (
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopurr/internal/localize"
	"gopurr/internal/rotatelog"
)

const Label = "io.github.gopurr.core"

// LogTail explains a daemon that is installed but not answering. The installer
// copies the protected log into the user's data directory so the interface can
// show why the kernel stopped instead of guessing "no administrator rights".
func LogTail(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	path := filepath.Join(dataDir, "service.log")
	if runtime.GOOS == "windows" {
		path = windowsServiceLogPath()
	}
	raw, err := rotatelog.Tail(path, 64<<10)
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
		return localize.Text("会将内核复制到 Program Files\\Gopurr\\Service，并注册 Gopurr Core Windows 系统服务，以 SYSTEM 身份运行，同时配置内核专用防火墙规则和崩溃恢复。\n\n只有 TUN 需要它；不安装也能使用系统代理。可在网络详情中卸载并清理防火墙规则。\n\nWindows 会显示 UAC 管理员授权。")
	}
	return localize.Text("会安装或替换受保护的内核副本，并注册系统服务。\n\nTUN 需要此服务；安装后系统代理也会复用授权，日常开关与应用重启无需再次输入密码。不安装也可独立使用系统代理。\n\n普通应用更新不要求重装服务。安装、更新或卸载服务时，macOS 会请求一次管理员授权。")
}

func UninstallDescription() string {
	if runtime.GOOS == "windows" {
		return localize.Text("会停止并删除 Gopurr Core Windows 系统服务及其受保护的内核副本，需要 UAC 管理员授权。卸载后 TUN 会关闭，订阅与设置保留。")
	}
	return localize.Text("会停止并删除系统服务及受保护的内核副本，需要一次管理员授权。\n\n卸载后 TUN 会关闭，系统代理改用普通内核，订阅与设置保留。系统策略要求时，系统代理会单独请求授权。")
}

func AuthorizationHint() string {
	if runtime.GOOS == "windows" {
		return localize.Text("正在安装服务，请在 Windows UAC 中确认授权…")
	}
	return localize.Text("正在安装服务，macOS 会要求一次管理员密码…")
}
