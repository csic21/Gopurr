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
	"time"

	"gopurr/internal/localize"
)

func EnsureRunning() error { return nil } // launchd owns recovery
func StopRunning() error   { return fmt.Errorf("守护进程由系统管理，应用无法直接停止") }

// A healthy authorized daemon may request its own restart. It cannot select
// another executable or alter its installation; launchd starts the same
// protected binary. No fresh administrator authorization is needed.
func Restart(dir string) error {
	statusPath := filepath.Join(dir, "core.json")
	readPID := func() int {
		raw, _ := os.ReadFile(statusPath)
		var status struct {
			PID int `json:"pid"`
		}
		_ = json.Unmarshal(raw, &status)
		return status.PID
	}
	previousPID := readPID()
	if previousPID <= 1 {
		return errors.New(localize.Text("服务状态不可用，请重新检测后重试"))
	}
	secret, err := os.ReadFile(filepath.Join(dir, "core.secret"))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dir, "core.sock"))
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
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if pid := readPID(); pid > 1 && pid != previousPID {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New(localize.Text("服务重启超时，请重新检测服务状态"))
}
