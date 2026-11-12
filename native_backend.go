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

	"gopurr/internal/app"
	"gopurr/internal/autostart"
	"gopurr/internal/buildinfo"
	"gopurr/internal/instance"
	"gopurr/internal/nativehost"
	"gopurr/internal/patch"
	"gopurr/internal/securefs"
	"gopurr/internal/shell"
	"gopurr/internal/shortcuts"
	"gopurr/internal/update"
)

// runNativeBackend keeps the proven desktop business layer, privileged helpers
// and core process, while the only window is the NSH frontend. No legacy window
// or editor is constructed in this mode.
func runNativeBackend(args []string) error {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return errors.New("NSH desktop backend requires macOS or Windows")
	}
	fs := flag.NewFlagSet("native-backend", flag.ContinueOnError)
	home := fs.String("home", "", "private application directory")
	socket := fs.String("socket", "", "private native socket")
	tokenFile := fs.String("token-file", "", "session authentication file")
	frontend := fs.String("frontend", "", "NSH application executable")
	parent := fs.Int("parent", 0, "frontend PID")
	waitForFrontend := fs.Bool("wait-for-frontend-exit", false, "keep the bundle alive until the NSH host exits")
	frontendTray := fs.Bool("frontend-tray", false, "native frontend owns the macOS status item")
	productionHome := fs.Bool("production-home", false, "reuse and snapshot the established desktop data")
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
	// The staged updater opens the frontend with these arguments. Backend
	// socket and authentication values describe this session only.
	os.Args = []string{*frontend, "--gopurr-home", *home}
	homeLock, err := nativehost.LockHome(*home)
	if err != nil {
		return err
	}
	defer homeLock.Close()
	if *productionHome {
		if err = nativehost.PrepareDesktopMigration(*home); err != nil {
			return fmt.Errorf("prepare desktop data migration: %w", err)
		}
	}
	if *productionHome {
		// The original desktop's default port is part of its behavior. Only
		// isolated previews reserve a random port; a release keeps defaults.
		err = securefs.PrivateDir(*home)
		if path := filepath.Join(*home, "overrides.json"); err == nil {
			if _, e := os.Lstat(path); e == nil {
				err = securefs.PrivateFile(path)
			} else if !errors.Is(e, os.ErrNotExist) {
				err = e
			}
		}
	} else {
		err = prepareNativeHome(*home)
	}
	if err != nil {
		return err
	}
	showWindowOnLaunch := nativehost.ConsumeRelaunchShow(*home)
	if err = os.Setenv("GOPURR_HOME", *home); err != nil {
		return err
	}
	a := app.New()
	a.SetNativeShowWindowOnLaunch(showWindowOnLaunch)
	a.NativeSetAutoLaunchHandler(func(enabled bool) error { return autostart.SetExecutable(enabled, *frontend) })
	app.ConfigureLanguages(a.Prefs())
	update.ConfigureHTTPClient(a.UpdateProxyURL)
	events := &nativehost.Events{}
	show := func() { a.SetWindowVisible(true); events.Add("show", map[string]any{"page": a.NativePage()}) }
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
	var requestStop func()
	activate := func(args []string) {
		app.RunOnMain(func() {
			for _, arg := range args {
				if arg == "--native-quit" {
					a.SetQuitHandler(nil)
					requestStop()
					return
				}
			}
			for _, arg := range args {
				if strings.HasPrefix(arg, "gopurr://") || strings.HasPrefix(arg, "clash://") {
					if err := importURL(arg); err != nil {
						events.Add("error", err.Error())
					}
				}
			}
			for _, arg := range args {
				if arg == "--autostart" {
					return
				}
			}
			show()
		})
	}
	stopInstance, err := instance.Listen(*home, activate)
	if err != nil {
		return err
	}
	defer stopInstance()
	a.SetPrefsHook(func(p app.Prefs) {
		app.RunOnMain(func() {
			app.ConfigureLanguages(p)
			events.Add("prefs", p)
		})
	})
	var quitOnce sync.Once
	stopped := make(chan struct{})
	stop := func() {
		select {
		case <-stopped:
		default:
			close(stopped)
		}
	}
	scheduleQuit := func() {
		quitOnce.Do(func() {
			relaunch := a.RelaunchRequested()
			log.Printf("native shutdown ready; frontend=%d relaunch=%v", *parent, relaunch)
			if relaunch {
				if err := nativehost.MarkRelaunchShow(*home); err != nil {
					log.Printf("native relaunch: remember visible window: %v", err)
				}
			}
			events.Add("quit", map[string]bool{"relaunch": false})
			go func() {
				if *waitForFrontend {
					for nativeParentAlive(*parent) {
						time.Sleep(50 * time.Millisecond)
					}
				} else {
					time.Sleep(500 * time.Millisecond)
				}
				app.RunOnMain(func() {
					log.Printf("native frontend exited; finishing shutdown (relaunch=%v)", relaunch)
					if relaunch {
						nativeRelaunch(*frontend, *home)
					}
					stop()
				})
			}()
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
	requestStop = func() {
		done := make(chan error, 1)
		app.RunOnMain(func() { beginShutdown(done) })
		go func() {
			if err := <-done; err != nil {
				log.Printf("native quit canceled: %v", err)
				events.Add("error", err.Error())
				app.RunOnMain(show)
			}
		}()
	}
	a.SetQuitHandler(requestStop)
	shutdown := func(ctx context.Context) error {
		done := make(chan error, 1)
		app.RunOnMain(func() { beginShutdown(done) })
		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	_ = *frontendTray
	trayMenu := app.NewNativeTrayMenu(func() *app.Menu { return a.TrayMenu(show) })
	var server *nativehost.Server
	defer func() {
		if server != nil {
			_ = server.Close()
			_ = nativehost.CleanupSession(*socket, *tokenFile, token)
		}
	}()
	a.StartBackground()
	var listenErr error
	server, listenErr = nativehost.Listen(*socket, token, func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		if a.ShuttingDown() && method != "host.ping" && method != "host.events" && method != "host.shutdown" {
			return nil, errors.New("desktop application is shutting down")
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		switch method {
		case "host.tray.menu":
			return trayMenu.Snapshot(ctx)
		case "host.tray.activate":
			var p struct {
				Command string `json:"command"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
			return trayMenu.Activate(ctx, p.Command)
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
			app.RunOnMain(func() { done <- importURL(p.URL) })
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
			app.RunOnMain(func() { a.SetWindowVisible(p.Visible) })
			return true, nil
		case "host.activate":
			app.RunOnMain(show)
			return true, nil
		case "host.shortcut":
			var p struct {
				Action int `json:"action"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
			app.RunOnMain(func() { a.RunShortcut(shortcuts.Action(p.Action), show) })
			return map[string]bool{"ok": true}, nil
		case "host.file.open", "host.file.save":
			return nativeFileDialog(method, params)
		case "host.clipboard.write":
			var p struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(params, &p); err != nil {
				return nil, err
			}
			return map[string]bool{"ok": true}, shell.WriteClipboard(p.Text)
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
			return map[string]bool{"ok": true}, shell.OpenExternal(p.URL)
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
			return true, shell.ShowItem(p.Path)
		default:
			return a.NativeCall(ctx, method, params)
		}
	})
	if listenErr != nil {
		return listenErr
	}
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if !nativeParentAlive(*parent) {
				requestStop()
				return
			}
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	select {
	case <-sig:
		requestStop()
		<-stopped
	case <-stopped:
	}
	return nil
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
	var extensions []string
	switch p.Kind {
	case "backup":
		extensions = []string{"gopurr", "zip"}
	case "yaml", "profile":
		extensions = []string{"yaml", "yml"}
	case "logs":
		extensions = []string{"txt", "log"}
	case "application":
		extensions = []string{"app", "exe"}
	default:
		return nil, fmt.Errorf("unknown file kind %q", p.Kind)
	}
	path, err := shell.ChooseFile(method != "host.file.open", filepath.Base(p.Name), extensions)
	if err != nil {
		return nil, err
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
