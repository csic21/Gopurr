package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopurr/internal/buildinfo"
	"gopurr/internal/controllerconfig"
	"gopurr/internal/process"
	"gopurr/internal/securefs"

	"go.yaml.in/yaml/v3"

	C "github.com/metacubex/mihomo/constant"
	"github.com/metacubex/mihomo/hub"
	"github.com/metacubex/mihomo/hub/executor"
)

// DaemonOptions describe a kernel that owns itself: a process that runs one
// config, exposes the standard controller on a unix socket, and exits on
// SIGTERM.
type DaemonOptions struct {
	ConfigPath string // the YAML the app prepared
	Socket     string // unix socket for the mihomo controller
	Secret     string // bearer token for that controller
	SecretFile string // ...or read it from here (keeps it out of a plist)
	ParentPID  int    // exit when this process goes away
	Home       string // geoip/cache/providers live here
	StatusPath string // written once ready, read by the app
}

// DaemonStatus is what the app needs to know about a running daemon.
type DaemonStatus struct {
	SecurityVersion int    `json:"securityVersion"`
	Version         string `json:"version,omitempty"`
	PID             int    `json:"pid"`
	UID             int    `json:"uid"`
	Socket          string `json:"socket"`
	Home            string `json:"home"`
}

// Increment when an installed privileged daemon must be reinstalled even if
// the application version has not changed (for example a development build).
const SecurityVersion = 1

// RunDaemon is the privileged side of the app. It is the same kernel code as
// before, just owned by a process that can legally create TUN devices.
func RunDaemon(opts DaemonOptions) error {
	// mihomo resolves a relative socket path against the home directory, which
	// is never what the caller meant.
	if abs, err := filepath.Abs(opts.Socket); err == nil {
		opts.Socket = abs
	}
	if opts.Home != "" {
		home, err := filepath.Abs(opts.Home)
		if err != nil {
			return err
		}
		opts.Home = home
		if err := os.MkdirAll(home, 0o755); err != nil {
			return err
		}
		C.SetHomeDir(home)
	}

	raw, err := os.ReadFile(opts.ConfigPath)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("配置不是合法 YAML: %w", err)
	}
	if doc == nil {
		doc = map[string]any{}
	}

	backendDir, err := privateControllerDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(backendDir)
	if err := securefs.PrivateDir(backendDir); err != nil {
		return err
	}
	backend := filepath.Join(backendDir, "c.sock")
	controllerconfig.Strip(doc)
	doc["external-controller-unix"] = backend
	if opts.Home != "" {
		doc["geodata-mode"] = false
	}

	secret := opts.Secret
	if secret == "" && opts.SecretFile != "" {
		b, err := os.ReadFile(opts.SecretFile)
		if err != nil {
			return fmt.Errorf("读取 secret: %w", err)
		}
		secret = strings.TrimSpace(string(b))
	}
	if secret == "" {
		return errors.New("没有 controller secret")
	}
	doc["secret"] = secret

	merged, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}

	if err := hub.Parse(merged); err != nil {
		return err
	}
	defer executor.Shutdown()
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("unix", backend, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("内核控制接口未就绪: %w", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	controller, err := serveController(opts.Socket, backend, secret)
	if err != nil {
		return err
	}
	defer controller.Close()

	if opts.StatusPath != "" {
		status := DaemonStatus{SecurityVersion: SecurityVersion, Version: buildinfo.Version, PID: os.Getpid(), UID: process.UID(), Socket: opts.Socket, Home: opts.Home}
		if b, err := json.Marshal(status); err == nil {
			if err := securefs.WriteFile(opts.StatusPath, b, 0o644); err != nil {
				return err
			}
		}
	}

	sig := make(chan os.Signal, 1)
	stopSignals, err := process.NotifyStop(sig)
	if err != nil {
		return err
	}
	defer stopSignals()
	done := make(chan struct{})
	defer close(done)

	if opts.ParentPID > 0 {
		// A daemon spawned by the app must not outlive it, even if the app
		// crashes and never gets to send SIGTERM.
		go func() {
			for {
				time.Sleep(2 * time.Second)
				select {
				case <-done:
					return
				default:
				}
				if !process.Alive(opts.ParentPID) {
					select {
					case sig <- os.Interrupt:
					default:
					}
					return
				}
			}
		}()
	}

	<-sig

	if opts.StatusPath != "" {
		_ = os.Remove(opts.StatusPath)
	}
	return nil
}
