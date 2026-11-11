//go:build darwin

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gopurr/internal/localize"
)

func EnsureRunning() error { return nil } // launchd owns recovery
func StopRunning() error   { return fmt.Errorf("守护进程由系统管理，应用无法直接停止") }

// A healthy authorized daemon may request its own restart. It cannot select
// another executable or alter its installation; launchd starts the same
// protected binary. No fresh administrator authorization is needed.
//
// That channel needs a working controller socket. When the socket is gone (the
// daemon's temporary directory was cleaned, or the process is wedged) launchd
// is asked to replace the process instead; this is the only way back and it
// needs the same one-time administrator authorization as installing the
// service.
func Restart(dir string) error {
	statusPath := filepath.Join(dir, "core.json")
	socket := filepath.Join(dir, "core.sock")
	previousPID := readDaemonPID(statusPath)
	err := restartThroughController(socket, previousPID)
	if err == nil {
		return nil
	}
	if !controllerGone(socket) {
		return err
	}
	if !Installed() {
		return err
	}
	if kickErr := kickstart(); kickErr != nil {
		return errors.Join(err, kickErr)
	}
	if waitErr := waitForDaemonChange(statusPath, previousPID, 30*time.Second); waitErr != nil {
		return errors.Join(err, waitErr)
	}
	return nil
}

func restartThroughController(socket string, previousPID int) error {
	if previousPID <= 1 {
		return errors.New(localize.Text("服务状态不可用，请重新检测后重试"))
	}
	secret, err := os.ReadFile(filepath.Join(filepath.Dir(socket), "core.secret"))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://localhost/gopurr/restart", nil)
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(secret)))
	response, err := (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		return fmt.Errorf(localize.Text("服务未响应；请稍后重试，仍失败时使用修复服务: %w"), err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf(localize.Text("服务不支持直接重启，请更新服务（HTTP %d）"), response.StatusCode)
	}
	if err := waitForDaemonChange(filepath.Join(filepath.Dir(socket), "core.json"), previousPID, 30*time.Second); err != nil {
		return errors.New(localize.Text("服务重启超时，请重新检测服务状态"))
	}
	return nil
}

func readDaemonPID(statusPath string) int {
	raw, _ := os.ReadFile(statusPath)
	var status struct {
		PID int `json:"pid"`
	}
	_ = json.Unmarshal(raw, &status)
	return status.PID
}

// waitForDaemonChange reports readiness once launchd (or the daemon itself) has
// published a new process. It treats the old PID as stale evidence.
func waitForDaemonChange(statusPath string, previousPID int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if pid := readDaemonPID(statusPath); pid > 1 && pid != previousPID {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New(localize.Text("服务重启超时，请重新检测服务状态"))
}

// controllerGone distinguishes a dead socket from a busy daemon. A busy daemon
// still accepts the connection; only a missing target or a closed listener
// proves there is nothing to answer.
func controllerGone(socket string) bool {
	conn, err := net.DialTimeout("unix", socket, 400*time.Millisecond)
	if err == nil {
		conn.Close()
		return false
	}
	return errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}

// adminRun is a seam so tests never open an authorization prompt.
var adminRun = runAsAdmin

// kickstartCommand is the exact root command that replaces the daemon without
// touching its installation.
func kickstartCommand() string {
	return "/bin/launchctl kickstart -k system/" + Label
}

func kickstart() error {
	if err := adminRun(kickstartCommand()); err != nil {
		return fmt.Errorf(localize.Text("重启系统服务失败: %w"), err)
	}
	return nil
}
