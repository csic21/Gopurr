package app

import (
	"fmt"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Fields belong to the UI thread. Only shutdownPending is read by workers.
type shutdownState struct {
	ready, relaunch bool
	err             string
	quit            func() // optional quit dispatcher for isolated UI tests
}

type shutdownResult struct {
	restored bool
	err      error
}

func (a *App) ShutdownReady() bool     { return a.shutdown.ready }
func (a *App) ShuttingDown() bool      { return a.shutdownPending.Load() }
func (a *App) RelaunchRequested() bool { return a.shutdown.relaunch }

func (a *App) requestQuit(relaunch bool) {
	if a.ShuttingDown() {
		return
	}
	a.shutdown.relaunch = relaunch
	quit := a.shutdown.quit
	if quit == nil {
		quit = mygo.App.Quit
	}
	// The first quit is canceled by OnBeforeQuit. Relaunch is requested only
	// after asynchronous cleanup succeeds, so mygo cannot lose that intent
	// when canceling its initial quit event.
	quit()
}

// ShutdownAsync starts on the UI thread and returns immediately. Completion
// and all UI state changes return to the UI thread, including failure/retry.
func (a *App) ShutdownAsync(done func(error)) {
	a.beginShutdown(mygo.RunOnMain, done)
}

func (a *App) beginShutdown(dispatch func(func()), done func(error)) {
	if !a.shutdownPending.CompareAndSwap(false, true) {
		return
	}
	a.cancelMeshProbe()
	a.shutdown.err = ""
	work := a.shutdownWork()
	a.invalidate()
	go func() {
		result := work()
		dispatch(func() {
			a.applyShutdownResult(result)
			if result.err != nil {
				a.shutdownPending.Store(false)
			}
			a.invalidate()
			done(result.err)
		})
	}()
}

// Snapshot UI-owned inputs before starting the worker. Blocking proxy locks,
// networksetup/helper requests and daemon termination never touch UI fields.
func (a *App) shutdownWork() func() shutdownResult {
	applied, port, previous := a.sysApplied, a.mixedPort(), a.prev
	proxy := a.proxy
	if applied {
		proxy = a.proxyController()
	}
	if previous != nil {
		copy := *previous
		previous = &copy
	}
	kernel, updates, stopUpdates := a.k, a.softwareUpdates, a.stopSoftwareUpdates
	meshClient := a.meshClient
	return func() (result shutdownResult) {
		if applied {
			if err := proxy.Restore(port, previous); err != nil {
				result.err = fmt.Errorf("无法恢复系统代理，应用保持运行: %w", err)
				return
			}
			result.restored = true
		}
		if proxy != nil {
			if result.err = proxy.Close(); result.err != nil {
				return
			}
		}
		if meshClient != nil {
			if err := meshClient.Shutdown(); err != nil {
				result.err = fmt.Errorf("无法关闭组网入站访问，应用保持运行: %w", err)
				return
			}
		}
		// Failure above must keep the kernel and recovery records available.
		if stopUpdates != nil {
			stopUpdates()
		}
		if updates != nil {
			updates.Close()
		}
		if kernel != nil {
			kernel.Shutdown()
		}
		return
	}
}

func (a *App) applyShutdownResult(result shutdownResult) {
	if result.restored {
		a.prev = nil
		a.savePrevProxy()
		a.sysApplied, a.sysOn = false, false
	}
	if result.err != nil {
		a.shutdown.err = result.err.Error()
		a.setSysErr(a.shutdown.err)
		a.page = "overview"
		return
	}
	a.shutdown.err = ""
	a.setSysErr("")
	a.cancelDelayTest()
	a.shutdown.ready = true
}

func (a *App) dismissShutdownError() {
	a.shutdown.err, a.shutdown.relaunch = "", false
	if a.softwareUpdates != nil {
		a.softwareUpdates.Hide()
	}
}

func (a *App) shutdownModal(c *ui.Context) {
	pal := paletteFor(c)
	width, _ := c.Size()
	action := "退出"
	if a.shutdown.relaunch {
		action = "重启"
	}
	title, message := "正在"+action, "正在恢复系统代理并结束当前会话，请稍候。"
	failed := a.shutdown.err != ""
	if failed {
		title, message = action+"未完成", "应用仍在运行，订阅与设置已保留。请稍后重试。"
	}
	ui.Overlay(c, func() {
		back := ui.Box(c.Key("shutdown")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().Modal()
		modalBackdrop(back)
		if failed && back.OverlayShortcut(0, ui.KeyEscape) {
			a.dismissShutdownError()
		}
		back.Children(func() {
			panel := ui.Column(c).Width(min(float32(560), width-64)).
				Role(ui.RoleAlertDialog).Label(title).Description(message)
			modalPanel(c, panel)
			panel.Padding(24).Radius(14).Background(pal.Surface).Border(1, pal.Border).Gap(14)
			panel.Children(func() {
				ui.Text(c, title).FontSize(18).Bold().TextColor(pal.Text)
				ui.Text(c, message).FillWidth().FontSize(12).TextColor(pal.Muted)
				if failed {
					ui.Text(c, a.shutdown.err).FillWidth().MaxLines(4).FontSize(11).TextColor(pal.Danger).Tooltip(a.shutdown.err)
				} else {
					ui.Progress(c, -1).FillWidth()
				}
				ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
					if failed {
						softButton(c, "返回应用").OnClick(a.dismissShutdownError)
						softPrimaryButton(c, "重试"+action).OnClick(func() { a.requestQuit(a.shutdown.relaunch) })
					} else {
						softPrimaryButton(c, "正在"+action+"…").Disabled(true)
					}
				})
			})
		})
	})
}
