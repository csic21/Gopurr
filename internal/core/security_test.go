package core_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gopurr/internal/core"
	"gopurr/internal/process"
)

// Exercise the real mihomo listener in a separate process, not the fake
// controller used by UI tests. No real profiles, TUN or OS proxy are involved.
func TestDaemonSecurity(t *testing.T) {
	dir, err := os.MkdirTemp("", "gp-sec-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	portListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	unwantedTCP := portListener.Addr().String()
	portListener.Close()
	unwantedSocket := filepath.Join(dir, "unwanted.sock")
	unwantedPipe := fmt.Sprintf(`\\.\pipe\gopurr-security-%d-%d`, os.Getpid(), time.Now().UnixNano())
	raw := fmt.Sprintf(`mixed-port: 0
mode: rule
log-level: debug
external-controller: %q
external-controller-tls: %q
external-controller-unix: %q
external-controller-pipe: %q
secret: subscription-secret
proxies: []
proxy-groups: []
rules: ["MATCH,DIRECT"]
`, unwantedTCP, unwantedTCP, unwantedSocket, unwantedPipe)
	opts := core.DaemonOptions{ConfigPath: filepath.Join(dir, "runtime.yaml"), Socket: filepath.Join(dir, "core.sock"), Secret: "real-kernel-security-secret", Home: filepath.Join(dir, "mihomo"), StatusPath: filepath.Join(dir, "core.json")}
	if err := os.WriteFile(opts.ConfigPath, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "marker")
	if err := os.WriteFile(marker, []byte("must remain unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	linked := os.Symlink(marker, opts.StatusPath) == nil // Windows may disallow symlinks for ordinary users.
	encoded, _ := json.Marshal(opts)
	logFile, err := os.Create(filepath.Join(dir, "test.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { logFile.Close() })
	cmd := exec.Command(os.Args[0], "-test.run=^TestDaemonHelperProcess$")
	cmd.Env = append(os.Environ(), "GOPURR_DAEMON_TEST=1", "GOPURR_DAEMON_OPTIONS="+string(encoded))
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = process.Stop(cmd.Process)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
		}
	})
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", opts.Socket)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
	request := func(method, path, secret, body string) (int, []byte) {
		t.Helper()
		req, err := http.NewRequest(method, "http://localhost"+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if secret != "" {
			req.Header.Set("Authorization", "Bearer "+secret)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, data
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		if _, err := os.Stat(opts.StatusPath); err == nil {
			b, _ := os.ReadFile(opts.StatusPath)
			var status core.DaemonStatus
			if json.Unmarshal(b, &status) == nil && status.PID == cmd.Process.Pid {
				break
			}
		}
		if time.Now().After(deadline) {
			logs, _ := os.ReadFile(logFile.Name())
			t.Fatalf("daemon not ready: %s", logs)
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Run("reject_missing_wrong_and_subscription_secrets", func(t *testing.T) {
		for _, secret := range []string{"", "wrong", "subscription-secret"} {
			for _, operation := range []struct{ method, path, body string }{
				{"GET", "/version", ""}, {"GET", "/configs", ""}, {"GET", "/logs", ""}, {"GET", "/debug/pprof/", ""},
				{"PATCH", "/configs", `{"mode":"direct"}`}, {"PUT", "/configs", `{"payload":"mode: direct"}`},
				{"DELETE", "/connections", ""}, {"POST", "/upgrade", ""},
			} {
				if code, _ := request(operation.method, operation.path, secret, operation.body); code != 401 {
					t.Errorf("%s %s secret %q: status %d", operation.method, operation.path, secret, code)
				}
			}
		}
		code, data := request("GET", "/configs", opts.Secret, "")
		var config struct {
			Mode string `json:"mode"`
		}
		if json.Unmarshal(data, &config) != nil || code != 200 || config.Mode != "rule" {
			t.Fatalf("unauthorized mutation: %d %s", code, data)
		}
	})
	t.Run("authorized_control_and_reload", func(t *testing.T) {
		if code, data := request("PATCH", "/configs", opts.Secret, `{"mode":"direct"}`); code != 204 {
			t.Fatalf("patch: %d %s", code, data)
		}
		body, _ := json.Marshal(map[string]string{"payload": raw})
		if code, data := request("PUT", "/configs?force=true", opts.Secret, string(body)); code != 204 {
			t.Fatalf("reload: %d %s", code, data)
		}
		if code, _ := request("GET", "/version", "", ""); code != 401 {
			t.Fatal("reload removed authentication")
		}
		if code, _ := request("GET", "/version", opts.Secret, ""); code != 200 {
			t.Fatal("reload changed application secret")
		}
		if code, _ := request("GET", "/debug/pprof/", opts.Secret, ""); code != 403 {
			t.Fatalf("debug route exposed: %d", code)
		}
		if code, _ := request("PUT", "/configs", opts.Secret, `{"path":"/etc/passwd"}`); code != 400 {
			t.Fatalf("path reload was not rejected: %d", code)
		}
		for _, path := range []string{"/upgrade", "/upgrade/ui", "/restart"} {
			if code, _ := request("POST", path, opts.Secret, ""); code != 403 {
				t.Errorf("exposed privileged route %s: %d", path, code)
			}
		}
	})
	t.Run("streaming_works", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "http://localhost/traffic", nil)
		req.Header.Set("Authorization", "Bearer "+opts.Secret)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var frame map[string]any
		if resp.StatusCode != 200 {
			t.Fatalf("traffic: %d", resp.StatusCode)
		}
		if err := json.NewDecoder(resp.Body).Decode(&frame); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("no_subscription_management_listeners", func(t *testing.T) {
		if conn, err := net.DialTimeout("tcp", unwantedTCP, 200*time.Millisecond); err == nil {
			conn.Close()
			t.Fatal("subscription opened a TCP controller")
		}
		if _, err := os.Lstat(unwantedSocket); !os.IsNotExist(err) {
			t.Fatalf("subscription created Unix controller: %v", err)
		}
		assertNoNamedPipe(t, unwantedPipe)
		if runtime.GOOS != "windows" {
			info, err := os.Stat(opts.Socket)
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatalf("socket is not private: %v %v", info, err)
			}
		}
	})
	t.Run("status_does_not_follow_symlink", func(t *testing.T) {
		if !linked {
			t.Skip("symlink creation unavailable")
		}
		data, _ := os.ReadFile(marker)
		if string(data) != "must remain unchanged" {
			t.Fatal("status write followed a symlink")
		}
		info, err := os.Lstat(opts.StatusPath)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("status not atomically replaced: %v", err)
		}
	})
}
