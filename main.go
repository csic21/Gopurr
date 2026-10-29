package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"flag"

	"gopurr/internal/app"
	"gopurr/internal/brand"
	"gopurr/internal/buildinfo"
	"gopurr/internal/core"
	"gopurr/internal/service"
	"gopurr/internal/sysproxy"
	"gopurr/internal/update"
)

// runDaemon parses the --core flags and blocks until the process is asked to
// stop. It is deliberately dumb: everything it does comes from the config.
func runDaemon(args []string) error {
	fs := flag.NewFlagSet("core", flag.ContinueOnError)
	config := fs.String("config", "", "config file to run")
	socket := fs.String("socket", "", "unix socket for the controller")
	secret := fs.String("secret", "", "controller secret")
	secretFile := fs.String("secret-file", "", "read the controller secret from this file")
	parent := fs.Int("parent", 0, "exit when this pid disappears")
	home := fs.String("home", "", "kernel home (geoip, cache)")
	status := fs.String("status", "", "status file written once ready")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *config == "" || *socket == "" {
		return errors.New("--config and --socket are required")
	}
	return core.RunDaemon(core.DaemonOptions{
		ConfigPath: *config,
		Socket:     *socket,
		Secret:     *secret,
		SecretFile: *secretFile,
		ParentPID:  *parent,
		Home:       *home,
		StatusPath: *status,
	})
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("Gopurr %s\n", buildinfo.Version)
		return
	}
	if handled, err := sysproxy.HandleCommand(os.Args[1:]); handled {
		if err != nil {
			log.Printf("system proxy helper: %v", err)
			os.Exit(1)
		}
		return
	}
	if handled, err := service.HandleCommand(os.Args[1:], runDaemon); handled {
		if err != nil {
			log.Printf("service: %v", err)
			os.Exit(1)
		}
		return
	}
	// The privileged helper runs the same binary with a flag, so a single
	// artifact is both the UI and the thing root executes.
	// The kernel side of the app: one process, one config, the standard
	// mihomo controller on a unix socket. Installed as a LaunchDaemon when it
	// needs to be root (TUN), started by the app otherwise.
	if len(os.Args) > 1 && os.Args[1] == "--core" {
		if err := runDaemon(os.Args[2:]); err != nil {
			log.Printf("core: %v", err)
			os.Exit(1)
		}
		return
	}

	mygo.App.SetName("Gopurr")
	if os.Getenv("GOPURR_HOME") == "" && !mygo.App.RequestSingleInstanceLock() {
		return
	}
	a := app.New()
	mygo.App.OnSecondInstance(func(args []string, _ string) {
		for _, arg := range args {
			if arg == "--autostart" {
				return
			}
		}
		showWindow(a)
	})
	update.ConfigureHTTPClient(a.UpdateProxyURL)
	mygo.Theme.SetSource(mygo.ThemeSource(a.Prefs().Appearance))

	mygo.App.SetName("Gopurr")
	mygo.App.SetVersion(buildinfo.Version)

	installQuitLifecycle(a)

	mygo.App.WhenReady(func() {
		if err := mygo.App.Dock.SetIcon(brand.IconPNG); err != nil {
			log.Printf("app icon unavailable: %v", err)
		}

		installWindowLifecycle(a)
		// Windows keeps its initial tray snapshot, so populate it only after
		// the background kernel and routing groups are ready.
		a.StartBackground()
		trayReady := installTray(a)
		openInitialWindow(a, trayReady)
		installTestHooks(a)
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func installQuitLifecycle(a *app.App) {
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		cancelRelease()
		if a.ShutdownReady() {
			// Only the final quit may close windows. Until cleanup completes,
			// keep AppKit dispatching frames and events instead of waiting on
			// proxy file locks, networksetup or daemon termination here.
			quitting.Store(true)
			return
		}
		e.PreventDefault()
		a.ShutdownAsync(func(err error) {
			if err != nil {
				log.Printf("quit canceled: %v", err)
				showWindow(a)
				return
			}
			if a.RelaunchRequested() {
				mygo.App.Relaunch()
			} else {
				mygo.App.Quit()
			}
		})
	})
}

func openInitialWindow(a *app.App, trayReady bool) {
	a.StartBackground()
	if !trayReady || a.StartupNeedsAttention() || !a.Prefs().SilentStart {
		showWindow(a)
	} else {
		a.SetWindowVisible(false)
		applyDockVisibility(a.Prefs(), false)
	}
}

var (
	// Window state and the release timer are accessed only on the UI thread.
	mainWin           *mygo.Window
	release           *time.Timer
	releaseGeneration uint64
	quitting          atomic.Bool
)

func installWindowLifecycle(a *app.App) {
	// The kernel must outlive the last window, including a light-mode release.
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnActivate(func(bool) { showWindow(a) })
	a.SetPrefsHook(func(app.Prefs) {
		mygo.RunOnMain(func() {
			if quitting.Load() {
				return
			}
			// A newer save may have reached the UI thread ahead of this hook.
			p := a.Prefs()
			if source := mygo.ThemeSource(p.Appearance); mygo.Theme.Source() != source {
				mygo.Theme.SetSource(source)
			}
			applyDockVisibility(p, windowVisible())
			scheduleRelease(a)
		})
	})
}

func windowVisible() bool {
	return mainWin != nil && !mainWin.IsDestroyed() && mainWin.IsVisible()
}

func applyDockVisibility(p app.Prefs, visible bool) {
	policy := mygo.ActivationPolicyAccessory
	if p.DockVisible(visible) {
		policy = mygo.ActivationPolicyRegular
	}
	if mygo.App.ActivationPolicy() != policy {
		mygo.App.SetActivationPolicy(policy)
	}
}

// showWindow opens the interface, rebuilding it when light mode released it.
func showWindow(a *app.App) {
	mygo.RunOnMain(func() {
		if quitting.Load() {
			return
		}
		cancelRelease()
		applyDockVisibility(a.Prefs(), true)
		if mainWin == nil || mainWin.IsDestroyed() {
			mainWin = newMainWindow(a)
			return
		}
		if mainWin.IsMinimized() {
			mainWin.Restore()
		}
		mainWin.Show()
		mainWin.Focus()
	})
}

func newMainWindow(a *app.App) *mygo.Window {
	w := mygo.NewWindow(mygo.WindowOptions{
		Title:                "Gopurr",
		Width:                1060,
		Height:               720,
		MinWidth:             840,
		MinHeight:            560,
		StateKey:             "main",
		TitleBarStyle:        mygo.TitleBarHidden,
		TrafficLightPosition: &mygo.Point{X: 20, Y: 12},
		BackgroundColor:      app.WindowBackgroundColor(),
		Content:              ui.View(a.View),
	})
	if err := w.SetIcon(brand.IconPNG); err != nil {
		log.Printf("window icon unavailable: %v", err)
	}
	w.OnShow(func() { a.SetWindowVisible(true) })
	w.OnHide(func() { a.SetWindowVisible(false) })
	w.OnMinimize(func() { a.SetWindowVisible(false) })
	w.OnRestore(func() { a.SetWindowVisible(true) })
	a.SetWindowVisible(true)

	// Closing the window parks the app in the menu bar: the kernel and the
	// system proxy must keep running, that is the whole point of a proxy app.
	w.OnClose(func(e *mygo.CloseEvent) {
		if quitting.Load() {
			return // the app is going away; let the close through
		}
		// Closing the window parks the app in the menu bar instead of quitting.
		e.PreventDefault()
		if a.ShuttingDown() {
			return
		}
		w.Hide()
		applyDockVisibility(a.Prefs(), false)
		scheduleRelease(a)
	})
	return w
}

// scheduleRelease destroys the window once the grace period passes, dropping
// its GPU buffers and UI caches while the daemon keeps working.
func scheduleRelease(a *app.App) {
	cancelRelease()
	p := a.Prefs()
	if quitting.Load() || a.ShuttingDown() || !p.LightMode || windowVisible() || mainWin == nil || mainWin.IsDestroyed() {
		return
	}

	delay := time.Duration(p.LightDelay) * time.Second
	if delay <= 0 {
		delay = time.Minute
	}

	generation := releaseGeneration
	release = time.AfterFunc(delay, func() {
		released := false
		mygo.RunOnMain(func() {
			// Stop cannot retract a callback already queued on the UI thread.
			// A reopen or preference change invalidates that older callback.
			if generation != releaseGeneration || quitting.Load() {
				return
			}
			release = nil
			if mainWin == nil || mainWin.IsDestroyed() || mainWin.IsVisible() {
				return
			}
			mainWin.Destroy()
			mainWin = nil
			applyDockVisibility(a.Prefs(), false)
			released = true
		})
		if !released {
			return
		}
		// Hand the freed heap back to the OS; Go would otherwise keep it.
		debug.FreeOSMemory()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		log.Printf("light mode: window released, heap=%dMiB", m.HeapAlloc>>20)
	})
}

func cancelRelease() {
	releaseGeneration++
	if release != nil {
		release.Stop()
		release = nil
	}
}

func installTray(a *app.App) bool {
	// The icon shows the window on a left click and its state menu on a
	// right click, as other menu bar proxies do. Only Linux panels, which
	// have no separate right click, open the menu on the click itself.
	menuOnClick := runtime.GOOS == "linux"
	toolTip := "Gopurr"
	if !menuOnClick {
		// The right click has no other affordance, so name it on hover.
		toolTip = "Gopurr · 左键显示窗口，右键打开菜单"
	}

	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "main",
		Icon:           brand.TrayPNG,
		IconIsTemplate: true,
		ToolTip:        toolTip,
	})
	if err != nil {
		log.Printf("tray unavailable: %v", err)
		return false
	}
	openWindow := func() { showWindow(a) }

	if runtime.GOOS != "darwin" {
		// Windows pops this menu on a right click by itself; Linux panels
		// open it on the click. Only Windows has a left click left to use.
		tray.SetMenu(a.TrayMenu(openWindow))
		if !menuOnClick {
			tray.OnClick(openWindow)
		}
		return true
	}

	// A fixed NSStatusItem menu consumes clicks before OnClick runs. Attach
	// a fresh menu only while tracking it, so every opening reads current
	// state, even after light mode has destroyed the main window.
	tracking := false
	openMenu := func() {
		if tracking {
			return
		}
		tracking = true
		defer func() {
			tray.SetMenu(nil)
			tracking = false
		}()
		tray.SetMenu(a.TrayMenu(openWindow))
		tray.PopUpMenu()
	}
	tray.OnClick(openWindow)
	tray.OnRightClick(openMenu)
	return true
}

// installTestHooks lets the memory measurement drive the app from a script:
// GOPURR_LIGHT shortens the grace period, GOPURR_AUTOCLOSE closes the window.
func installTestHooks(a *app.App) {
	if os.Getenv("GOPURR_LIGHT") == "1" {
		p := a.Prefs()
		p.LightMode = true
		p.LightDelay = 3
		a.UpdatePrefs(p)
	}

	if v := os.Getenv("GOPURR_QUIT_AFTER"); v != "" {
		secs, err := strconv.Atoi(v)
		if err == nil {
			time.AfterFunc(time.Duration(secs)*time.Second, func() { mygo.App.Quit() })
		}
	}

	if v := os.Getenv("GOPURR_REOPEN"); v != "" {
		secs, err := strconv.Atoi(v)
		if err == nil {
			time.AfterFunc(time.Duration(secs)*time.Second, func() { showWindow(a) })
		}
	}

	if v := os.Getenv("GOPURR_AUTOCLOSE"); v != "" {
		secs, err := strconv.Atoi(v)
		if err != nil {
			return
		}
		time.AfterFunc(time.Duration(secs)*time.Second, func() {
			mygo.RunOnMain(func() {
				if mainWin != nil {
					mainWin.Close()
				}
			})
		})
	}
}
