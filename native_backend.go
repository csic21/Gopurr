package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/egoist/mygo"
	"gopurr/internal/app"
	"gopurr/internal/autostart"
	"gopurr/internal/buildinfo"
	"gopurr/internal/nativehost"
	"gopurr/internal/patch"
	"gopurr/internal/securefs"
	"gopurr/internal/update"
)

// runNativeBackend keeps the proven desktop business layer, privileged helpers
// and core process, while the only window is the NSH frontend. No legacy window
// or editor is constructed in this mode.
func runNativeBackend(args []string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("NSH desktop backend currently requires macOS")
	}
	fs := flag.NewFlagSet("native-backend", flag.ContinueOnError)
	home := fs.String("home", "", "private application directory")
	socket := fs.String("socket", "", "private native socket")
	tokenFile := fs.String("token-file", "", "session authentication file")
	frontend := fs.String("frontend", "", "NSH application executable")
	parent := fs.Int("parent", 0, "frontend PID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !filepath.IsAbs(*home) || !filepath.IsAbs(*socket) || !filepath.IsAbs(*tokenFile) || !filepath.IsAbs(*frontend) || *parent <= 1 {
		return errors.New("absolute home, socket, token-file, frontend and parent are required")
	}
	token, err := nativehost.ReadToken(*tokenFile)
	if err != nil {
		return err
	}
	homeLock, err := nativehost.LockHome(*home)
	if err != nil {
		return err
	}
	defer homeLock.Close()
	if err = prepareNativeHome(*home); err != nil {
		return err
	}
	if err = os.Setenv("GOPURR_HOME", *home); err != nil {
		return err
	}
	mygo.App.SetName("Gopurr")
	mygo.App.SetVersion(buildinfo.Version)
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	a := app.New()
	a.NativeSetAutoLaunchHandler(func(enabled bool) error { return autostart.SetExecutable(enabled, *frontend) })
	app.ConfigureLanguages(a.Prefs())
	mygo.Theme.SetSource(mygo.ThemeSource(a.Prefs().Appearance))
	update.ConfigureHTTPClient(a.UpdateProxyURL)
	events := &nativehost.Events{}
	show := func() { a.SetWindowVisible(true); events.Add("show", map[string]any{"page": a.NativePage()}) }
	mygo.App.SetMenu(a.ApplicationMenu(show))
	mygo.App.OnLocaleChanged(func(string) { mygo.App.SetMenu(a.ApplicationMenu(show)) })
	importURL := func(raw string) error {
		payload, err := a.NativeImportURL(raw)
		if err != nil {
			return err
		}
		a.NativeSetPage("subs")
		events.Add("import", payload)
		show()
		return nil
	}
	mygo.App.OnOpenURL(func(raw string) {
		mygo.App.WhenReady(func() {
			if err := importURL(raw); err != nil {
				events.Add("error", err.Error())
				show()
			}
		})
	})
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnActivate(func(bool) { show() })
	a.SetPrefsHook(func(p app.Prefs) {
		mygo.RunOnMain(func() {
			mygo.Theme.SetSource(mygo.ThemeSource(p.Appearance))
			app.ConfigureLanguages(p)
			mygo.App.SetMenu(a.ApplicationMenu(show))
			events.Add("prefs", p)
		})
	})
	var quitOnce sync.Once
	scheduleQuit := func() {
		quitOnce.Do(func() {
			events.Add("quit", map[string]bool{"relaunch": a.RelaunchRequested()})
			time.AfterFunc(5*time.Second, func() { mygo.RunOnMain(mygo.App.Quit) })
		})
	}
	var shutdownWaiters []chan error
	shutdownPending := false
	beginShutdown := func(done chan error) {
		if a.ShutdownReady() {
			done <- nil
			return
		}
		shutdownWaiters = append(shutdownWaiters, done)
		if shutdownPending {
			return
		}
		shutdownPending = true
		a.ShutdownAsync(func(err error) {
			shutdownPending = false
			if err == nil {
				_ = homeLock.Close()
				// Cleanup belongs to the app, not to one socket request. A
				// disconnected quit caller must not strand a stopped helper.
				scheduleQuit()
			} else {
				events.Add("error", err.Error())
			}
			for _, waiter := range shutdownWaiters {
				waiter <- err
			}
			shutdownWaiters = nil
		})
	}
	shutdown := func(ctx context.Context) error {
		done := make(chan error, 1)
		mygo.RunOnMain(func() { beginShutdown(done) })
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if a.ShutdownReady() {
			scheduleQuit()
			return
		}
		e.PreventDefault()
		done := make(chan error, 1)
		beginShutdown(done)
		go func() {
			err := <-done
			mygo.RunOnMain(func() {
				if err != nil {
					log.Printf("native quit canceled: %v", err)
					events.Add("error", err.Error())
					show()
					return
				}
				scheduleQuit()
			})
		}()
	})
	var server *nativehost.Server
	defer func() {
		if server != nil {
			_ = server.Close()
			_ = nativehost.CleanupSession(*socket, *tokenFile, token)
		}
	}()
	mygo.App.WhenReady(func() {
		a.ConfigureShortcuts(mygo.GlobalShortcut, show)
		mygo.App.OnQuit(a.CloseShortcuts)
		a.StartBackground()
		installTrayWithWindow(a, show)
		var listenErr error
		server, listenErr = nativehost.Listen(*socket, token, func(ctx context.Context, method string, params json.RawMessage) (any, error) {
			if a.ShuttingDown() && method != "host.ping" && method != "host.events" && method != "host.shutdown" {
				return nil, errors.New("desktop application is shutting down")
			}
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			switch method {
			case "host.ping":
				return map[string]any{"ready": true, "version": buildinfo.Version}, nil
			case "host.importURL":
				var p struct {
					URL string `json:"url"`
				}
				if err := json.Unmarshal(params, &p); err != nil {
					return nil, err
				}
				done := make(chan error, 1)
				mygo.RunOnMain(func() { done <- importURL(p.URL) })
				select {
				case err := <-done:
					return nil, err
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			case "host.events":
				var p struct {
					After uint64 `json:"after"`
				}
				if err := json.Unmarshal(params, &p); err != nil {
					return nil, err
				}
				return events.Since(p.After), nil
			case "host.shutdown":
				if err := shutdown(ctx); err != nil {
					return nil, err
				}
				scheduleQuit()
				return map[string]bool{"ok": true}, nil
			case "host.visible":
				var p struct {
					Visible bool `json:"visible"`
				}
				if err := json.Unmarshal(params, &p); err != nil {
					return nil, err
				}
				mygo.RunOnMain(func() { a.SetWindowVisible(p.Visible) })
				return true, nil
			case "host.file.open", "host.file.save":
				return nativeFileDialog(method, params)
			case "host.clipboard.write":
				var p struct {
					Text string `json:"text"`
				}
				if err := json.Unmarshal(params, &p); err != nil {
					return nil, err
				}
				mygo.Clipboard.WriteText(p.Text)
				return map[string]bool{"ok": true}, nil
			case "host.openURL":
				var p struct {
					URL string `json:"url"`
				}
				if err := json.Unmarshal(params, &p); err != nil {
					return nil, err
				}
				u, err := url.Parse(p.URL)
				if err != nil || !nativeURLScheme(u.Scheme) {
					return nil, errors.New("unsupported URL")
				}
				return map[string]bool{"ok": true}, mygo.Shell.OpenExternal(p.URL)
			case "host.reveal":
				var p struct {
					Path string `json:"path"`
				}
				if err := json.Unmarshal(params, &p); err != nil {
					return nil, err
				}
				if !filepath.IsAbs(p.Path) {
					return nil, errors.New("absolute path required")
				}
				mygo.Shell.ShowItemInFolder(p.Path)
				return true, nil
			default:
				return a.NativeCall(ctx, method, params)
			}
		})
		if listenErr != nil {
			log.Printf("native socket: %v", listenErr)
			mygo.App.Quit()
			return
		}
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for range ticker.C {
				// The helper is a direct child. Reparenting also detects PID
				// reuse; EPERM alone is not evidence that a parent has exited.
				if !nativeParentAlive(*parent) {
					mygo.RunOnMain(mygo.App.Quit)
					// Cleanup may fail transiently. Keep retrying while the
					// frontend is gone instead of abandoning an orphan core.
				}
			}
		}()
	})
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	go func() { <-sig; mygo.RunOnMain(mygo.App.Quit) }()
	err = mygo.App.Run()
	return err
}

func nativeURLScheme(s string) bool {
	switch strings.ToLower(s) {
	case "http", "https", "file", "ssh", "vnc", "smb", "rdp", "ms-rd":
		return true
	}
	return false
}

func nativeFileDialog(method string, params json.RawMessage) (any, error) {
	var p struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	filters := []mygo.FileFilter{}
	switch p.Kind {
	case "backup":
		filters = []mygo.FileFilter{{Name: "Gopurr backup", Extensions: []string{"gopurr", "zip"}}}
	case "yaml", "profile":
		filters = []mygo.FileFilter{{Name: "YAML", Extensions: []string{"yaml", "yml"}}}
	case "logs":
		filters = []mygo.FileFilter{{Name: "Log", Extensions: []string{"txt", "log"}}}
	case "application":
		filters = []mygo.FileFilter{{Name: "Application", Extensions: []string{"app", "exe"}}}
	default:
		return nil, fmt.Errorf("unknown file kind %q", p.Kind)
	}
	var path string
	if method == "host.file.open" {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "Gopurr", Filters: filters})
		if err != nil {
			return nil, err
		}
		if len(paths) > 0 {
			path = paths[0]
		}
	} else {
		var err error
		path, err = mygo.Dialog.Save(mygo.SaveDialogOptions{Title: "Gopurr", DefaultPath: filepath.Base(p.Name), Filters: filters, CreateDirectories: true})
		if err != nil {
			return nil, err
		}
	}
	if path == "" {
		return map[string]any{"path": nil}, nil
	}
	return map[string]string{"path": path}, nil
}

func prepareNativeHome(home string) error {
	if err := securefs.PrivateDir(home); err != nil {
		return err
	}
	path := filepath.Join(home, "overrides.json")
	if _, err := os.Lstat(path); err == nil {
		return securefs.PrivateFile(path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Reserve both protocols while choosing a port. A TCP-only probe can pick
	// an occupied UDP port and fail the first core startup.
	for attempt := 0; attempt < 8; attempt++ {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			return err
		}
		port := listener.Addr().(*net.TCPAddr).Port
		packet, udpErr := net.ListenPacket("udp4", fmt.Sprintf("127.0.0.1:%d", port))
		listener.Close()
		if udpErr != nil {
			continue
		}
		packet.Close()
		ov := patch.Default()
		ov.MixedPort = port
		body, err := json.MarshalIndent(ov, "", "  ")
		if err != nil {
			return err
		}
		return securefs.WriteFile(path, body, 0600)
	}
	return errors.New("could not reserve a private desktop proxy port")
}

func nativeParentAlive(pid int) bool {
	if pid <= 1 || os.Getppid() != pid {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
