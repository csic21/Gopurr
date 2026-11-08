//go:build cgo && !android

package main

/*
#include <stdint.h>
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"gopurr/internal/nativehost"
	"gopurr/internal/securefs"
)

type desktopProcess struct {
	dir, socket, token string
	cmd                *exec.Cmd
	exitErr            error // published by closing done
	done               chan struct{}
	ctx                context.Context
	cancel             context.CancelFunc
	stopOnce           sync.Once
}

var desktops = struct {
	sync.Mutex
	next  uint64
	items map[uint64]*desktopProcess
}{items: map[uint64]*desktopProcess{}}

//export GopurrDesktopOpen
func GopurrDesktopOpen(options *C.char) *C.char {
	return returnedString(func() []byte {
		var o struct {
			Home           string `json:"home"`
			Executable     string `json:"executable"`
			Frontend       string `json:"frontend"`
			ProductionHome bool   `json:"productionHome"`
		}
		if options == nil || json.Unmarshal([]byte(C.GoString(options)), &o) != nil || !filepath.IsAbs(o.Home) || !filepath.IsAbs(o.Executable) || !filepath.IsAbs(o.Frontend) {
			return failure("invalid_params", "Absolute desktop paths are required")
		}
		if err := desktopHomeReady(o.Home); err != nil {
			if errors.Is(err, nativehost.ErrHomeInUse) {
				return desktopAlreadyRunning(o.Home)
			}
			return failure("open_failed", err.Error())
		}
		temporaryRoot := ""
		if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
			temporaryRoot = "/tmp"
		}
		dir, err := os.MkdirTemp(temporaryRoot, "gp-nsh-")
		if err != nil {
			return failure("open_failed", err.Error())
		}
		if err = securefs.PrivateDir(dir); err != nil {
			os.RemoveAll(dir)
			return failure("open_failed", err.Error())
		}
		cleanup := true
		defer func() {
			if cleanup {
				os.RemoveAll(dir)
			}
		}()
		key := make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return failure("open_failed", err.Error())
		}
		token := hex.EncodeToString(key)
		tokenFile := filepath.Join(dir, "auth")
		socket := filepath.Join(dir, "rpc")
		if err = os.WriteFile(tokenFile, []byte(token), 0600); err != nil {
			return failure("open_failed", err.Error())
		}
		cmd := exec.Command(o.Executable, "--native-backend", "--home", o.Home, "--socket", socket, "--token-file", tokenFile, "--frontend", o.Frontend, "--parent", fmt.Sprint(os.Getpid()))
		cmd.Args = append(cmd.Args, "--wait-for-frontend-exit")
		if o.ProductionHome {
			cmd.Args = append(cmd.Args, "--production-home")
		}
		cmd.Stdout = os.Stdout
		startupLog := &desktopStartupLog{}
		cmd.Stderr = startupLog
		configureDesktopCommand(cmd)
		if err = cmd.Start(); err != nil {
			return failure("open_failed", err.Error())
		}
		processContext, processCancel := context.WithCancel(context.Background())
		p := &desktopProcess{dir: dir, socket: socket, token: token, cmd: cmd, done: make(chan struct{}), ctx: processContext, cancel: processCancel}
		// The waiter owns session cleanup from this point, including startup
		// failures and an unexpected backend exit. Never remove authentication
		// while a backend may still need to restore the OS proxy.
		cleanup = false
		go func() { p.exitErr = cmd.Wait(); p.cancel(); _ = os.RemoveAll(p.dir); close(p.done) }()
		ready := false
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			b, e := nativehost.Call(ctx, socket, token, "host.ping", nil)
			cancel()
			var reply struct {
				Result struct {
					Ready bool `json:"ready"`
				} `json:"result"`
				Error *nativehost.Error `json:"error"`
			}
			if e == nil && json.Unmarshal(b, &reply) == nil && reply.Error == nil && reply.Result.Ready {
				ready = true
				break
			}
			select {
			case <-p.done:
				var exited *exec.ExitError
				if errors.As(p.exitErr, &exited) && exited.ExitCode() == 23 {
					return desktopAlreadyRunning(o.Home)
				}
				if lock, err := nativehost.LockHome(o.Home); err == nil {
					lock.Close()
				} else if errors.Is(err, nativehost.ErrHomeInUse) {
					return desktopAlreadyRunning(o.Home)
				}
				message := "Desktop backend exited during startup"
				if detail := startupLog.String(); detail != "" {
					message += ": " + detail
				}
				return failure("open_failed", message)
			case <-time.After(50 * time.Millisecond):
			}
		}
		if !ready {
			p.stop()
			return failure("open_failed", "Desktop backend did not become ready")
		}
		desktops.Lock()
		desktops.next++
		id := desktops.next
		desktops.items[id] = p
		desktops.Unlock()
		cleanup = false
		b, _ := json.Marshal(map[string]any{"handle": id, "socket": socket, "tokenFile": tokenFile, "backendPID": cmd.Process.Pid, "frontendPID": os.Getpid()})
		return b
	})
}

// GUI processes may have no stderr handle on Windows. Keep a bounded startup
// diagnostic for the frontend and tolerate an unavailable inherited console.
type desktopStartupLog struct {
	sync.Mutex
	text []byte
}

func (l *desktopStartupLog) Write(data []byte) (int, error) {
	const limit = 8192
	l.Lock()
	if len(data) >= limit {
		l.text = append(l.text[:0], data[len(data)-limit:]...)
	} else {
		if excess := len(l.text) + len(data) - limit; excess > 0 {
			l.text = l.text[excess:]
		}
		l.text = append(l.text, data...)
	}
	l.Unlock()
	_, _ = os.Stderr.Write(data)
	if path := os.Getenv("GOPURR_NATIVE_SESSION_OUTPUT"); path != "" {
		path = strings.TrimSuffix(path, filepath.Ext(path)) + ".backend.log"
		if file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
			_, _ = file.Write(data)
			_ = file.Close()
		}
	}
	return len(data), nil
}
func (l *desktopStartupLog) String() string {
	l.Lock()
	defer l.Unlock()
	return strings.TrimSpace(string(l.text))
}

func desktopAlreadyRunning(home string) []byte {
	body, _ := json.Marshal(map[string]any{"error": map[string]any{
		"code": "already_running", "message": nativehost.ErrHomeInUse.Error(),
		"activate": nativehost.IsCanonicalHome(home),
	}})
	return body
}

//export GopurrDesktopCall
func GopurrDesktopCall(handle C.uint64_t, request *C.char) *C.char {
	return returnedString(func() []byte {
		desktops.Lock()
		p := desktops.items[uint64(handle)]
		desktops.Unlock()
		if p == nil {
			return failure("closed", "Desktop backend is closed")
		}
		var r nativehost.Request
		if request == nil || json.Unmarshal([]byte(C.GoString(request)), &r) != nil || r.Method == "" {
			return failure("invalid_request", "A desktop request is required")
		}
		timeout := 155 * time.Second
		if r.Method == "host.events" {
			// Polling never waits for UI/network work in the backend. Bound a
			// stalled transport so activation and graceful quit can retry.
			timeout = 2 * time.Second
		}
		ctx, cancel := context.WithTimeout(p.ctx, timeout)
		defer cancel()
		b, err := nativehost.Call(ctx, p.socket, p.token, r.Method, r.Params)
		if err != nil {
			return failure("transport", err.Error())
		}
		return b
	})
}

//export GopurrDesktopClose
func GopurrDesktopClose(handle C.uint64_t) {
	desktops.Lock()
	p := desktops.items[uint64(handle)]
	delete(desktops.items, uint64(handle))
	desktops.Unlock()
	if p == nil {
		return
	}
	p.stop()
	select {
	case <-p.done:
	case <-time.After(2 * time.Second):
		// The waiter retains the private session until graceful cleanup exits.
	}
}

func desktopHomeReady(home string) error {
	deadline := time.Now().Add(1500 * time.Millisecond)
	for {
		lock, err := nativehost.LockHome(home)
		if err == nil {
			return lock.Close()
		}
		if !errors.Is(err, nativehost.ErrHomeInUse) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// stop cancels client reads, then requests the same graceful cleanup as Quit.
// Startup timeout must not SIGKILL a helper that may already own an OS proxy.
// If restoration is temporarily unavailable, retry without deleting its token
// or killing its core. The backend's parent monitor also retries after the
// frontend itself exits.
func (p *desktopProcess) stop() {
	p.stopOnce.Do(func() {
		p.cancel()
		go func() {
			for {
				select {
				case <-p.done:
					return
				default:
				}
				ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
				reply, err := nativehost.Call(ctx, p.socket, p.token, "host.shutdown", nil)
				cancel()
				select {
				case <-p.done:
					return
				default:
				}
				var response nativehost.Response
				if err != nil || json.Unmarshal(reply, &response) != nil || response.Error != nil {
					// Handles startup before the socket is listening. The
					// backend subscribes to SIGINT before entering its loop.
					_ = p.cmd.Process.Signal(os.Interrupt)
				}
				select {
				case <-p.done:
					return
				case <-time.After(5 * time.Second):
				}
			}
		}()
	})
}
