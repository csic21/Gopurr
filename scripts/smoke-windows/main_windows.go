// Command smoke-windows verifies the actual SCM service and embedded Wintun on
// an ephemeral CI machine. It never enables system proxy or automatic routing.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"gopurr/internal/kernel"
	"gopurr/internal/service"

	"golang.org/x/sys/windows"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() (err error) {
	binary := flag.String("binary", "", "application executable")
	flag.Parse()
	if !windows.GetCurrentProcessToken().IsElevated() {
		return errors.New("service smoke test requires an administrator CI runner")
	}
	exe, err := filepath.Abs(*binary)
	if err != nil {
		return err
	}
	if service.Installed() {
		return errors.New("refusing to replace an existing Gopurr service")
	}
	dir, err := os.MkdirTemp("", "gp-smoke-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	manager, err := kernel.NewManager(dir)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	config := fmt.Sprintf(`mixed-port: %d
allow-lan: false
mode: rule
log-level: info
ipv6: false
dns:
  enable: false
tun:
  enable: true
  device: Gopurr-CI
  stack: mixed
  auto-route: false
  auto-detect-interface: true
rules:
  - MATCH,DIRECT
`, port)
	if err := os.WriteFile(manager.ConfigPath(), []byte(config), 0o600); err != nil {
		return err
	}
	admin := func(action string) error {
		result := filepath.Join(dir, "result.json")
		_ = os.Remove(result)
		cmd := exec.Command(exe, "--service-admin", action, dir, result)
		out, cmdErr := cmd.CombinedOutput()
		data, _ := os.ReadFile(result)
		var status struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(data, &status); err != nil {
			return fmt.Errorf("%s: %v (%s), result %s", action, cmdErr, out, data)
		}
		if status.Error != "" {
			return errors.New(status.Error)
		}
		return cmdErr
	}
	defer func() {
		if cleanupErr := admin("uninstall"); cleanupErr != nil && err == nil {
			err = cleanupErr
		}
		if err != nil {
			if logs, readErr := os.ReadFile(filepath.Join(dir, "core.log")); readErr == nil {
				fmt.Fprintln(os.Stderr, string(logs))
			}
		}
	}()
	wait := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for {
			if manager.Remote().Probe(ctx) == nil && manager.IsRoot() {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(100 * time.Millisecond):
			}
		}
	}
	if err := admin("install"); err != nil {
		return err
	}
	if err := wait(); err != nil {
		return fmt.Errorf("service readiness: %w", err)
	}
	before, err := manager.Status()
	if err != nil {
		return err
	}
	adapter := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference='Stop'; Get-NetAdapter -Name Gopurr-CI | Format-List Name,Status,InterfaceDescription")
	if out, err := adapter.CombinedOutput(); err != nil {
		return fmt.Errorf("Wintun adapter missing: %v %s", err, out)
	} else {
		fmt.Print(string(out))
	}
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer origin.Close()
	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	resp, err := client.Get(origin.URL)
	if err != nil {
		return fmt.Errorf("HTTP proxy through service: %w", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}
	if err := manager.Remote().Reload(manager.ConfigPath()); err != nil {
		return fmt.Errorf("reload: %w", err)
	}
	if err := admin("install"); err != nil {
		return fmt.Errorf("service upgrade: %w", err)
	}
	if err := wait(); err != nil {
		return fmt.Errorf("upgraded service readiness: %w", err)
	}
	after, err := manager.Status()
	if err != nil {
		return err
	}
	if before.PID == after.PID {
		return errors.New("upgrade did not replace service process")
	}
	fmt.Printf("SCM install, Wintun creation, HTTP proxy, reload and service upgrade passed (PID %d -> %d).\n", before.PID, after.PID)
	return nil
}
