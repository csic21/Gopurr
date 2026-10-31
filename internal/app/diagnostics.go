package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/core"
	"gopurr/internal/diagnostic"
	"gopurr/internal/sysproxy"
)

type diagnosticState struct {
	diagMu                         sync.Mutex
	diagSteps                      []diagnostic.Step
	diagRunning                    bool
	diagCancel                     context.CancelFunc
	diagGeneration                 uint64
	diagURL, diagError, diagNotice string
}

func (a *App) diagnosticSnapshot() ([]diagnostic.Step, bool) {
	a.diagMu.Lock()
	defer a.diagMu.Unlock()
	return append([]diagnostic.Step(nil), a.diagSteps...), a.diagRunning
}

func (a *App) cancelDiagnostics() {
	a.diagMu.Lock()
	cancel := a.diagCancel
	a.diagMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *App) startDiagnostics() {
	a.diagError, a.diagNotice = "", ""
	if err := validateTestURL(a.diagURL); err != nil {
		a.diagError = err.Error()
		return
	}
	a.diagMu.Lock()
	if a.diagRunning {
		a.diagMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.diagCancel, a.diagRunning = cancel, true
	a.diagGeneration++
	generation := a.diagGeneration
	a.diagSteps = diagnostic.Steps()
	a.diagMu.Unlock()
	port := a.mixedPort()
	opt := diagnostic.Options{URL: a.diagURL, Port: port}
	if a.mgr != nil {
		opt.Probe = a.mgr.Remote().Probe
		opt.Direct = func(ctx context.Context, address string) error {
			_, err := a.mgr.Remote().DelayContext(ctx, "DIRECT", address, 5000)
			return err
		}
	}
	if sysproxy.Available() {
		opt.ProxyStatus = func(context.Context) (bool, bool, error) {
			state, err := sysproxy.Current()
			return state.Enabled, state.Server == "127.0.0.1" && state.Port == port, err
		}
	}
	dns, _, err := a.effectiveFeatures()
	if (err != nil || dns.Enable) && a.mgr != nil {
		opt.KernelDNS = a.mgr.Remote().QueryDNS
		if status, err := a.mgr.Status(); err == nil && status.ControllerVersion < core.ControllerVersion {
			opt.KernelDNS = func(context.Context, string) error { return diagnostic.ErrOldController }
		}
	}
	go func() {
		defer cancel()
		diagnostic.Run(ctx, opt, func(steps []diagnostic.Step) {
			a.diagMu.Lock()
			if a.diagGeneration == generation {
				a.diagSteps = steps
			}
			a.diagMu.Unlock()
			a.invalidate()
		})
		a.diagMu.Lock()
		if a.diagGeneration == generation {
			a.diagRunning, a.diagCancel = false, nil
		}
		a.diagMu.Unlock()
		a.invalidate()
	}()
}

func (a *App) diagnosticsPage(c *ui.Context) {
	pal := paletteFor(c)
	if a.diagURL == "" {
		a.diagURL = a.Prefs().TestURL
	}
	steps, running := a.diagnosticSnapshot()
	hasResult := len(steps) != 0
	if !hasResult {
		steps = diagnostic.Steps()
	}
	ui.Scroll(c).FillWidth().Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).FillWidth().Gap(16).Children(func() {
			ui.Column(c).FillWidth().Gap(8).Children(func() {
				ui.Text(c, tr("检查哪里没有连通")).FontSize(15).Bold().TextColor(pal.Text)
				summary := tr("分别检查 DNS、直连与代理出口，不修改网络设置。")
				if hasResult {
					summary = diagnosticSummary(steps, running)
				}
				ui.Text(c, summary).FillWidth().FontSize(12).TextColor(pal.Muted)
				ui.Text(c, tr("测试地址 · 检测时向此地址发送直连和代理请求")).FillWidth().FontSize(11).TextColor(pal.Faint)
				ui.TextInput(c, &a.diagURL).FillWidth().Disabled(running).Label(tr("诊断测试地址")).Placeholder(defaultPrefs().TestURL)
			})
			ui.Column(c).FillWidth().Radius(12).Background(pal.Surface).Border(1, pal.Border).Children(func() {
				for i, step := range steps {
					if i > 0 {
						hairline(c)
					}
					ui.Row(c).FillWidth().Padding(12, 16).Gap(12).AlignItems(ui.Center).Children(func() {
						color := pal.Faint
						switch step.State {
						case diagnostic.Passed:
							color = pal.Success
						case diagnostic.Failed:
							color = pal.Danger
						case diagnostic.Running:
							color = pal.Accent
						case diagnostic.Info:
							color = pal.Warn
						}
						ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
							ui.Text(c, tr(step.Name)).FontSize(13).FontWeight(500).TextColor(pal.Text)
							detail := trDiagnosticDetail(step.Detail)
							if detail == "" {
								detail = tr("开始诊断后显示结果")
							}
							ui.Text(c, detail).FillWidth().FontSize(11).TextColor(pal.Muted)
						})
						ui.Column(c).Width(54).Gap(4).AlignItems(ui.End).Children(func() {
							ui.Text(c, tr(string(step.State))).FontSize(11.5).TextColor(color)
							if step.Duration > 0 {
								duration := fmt.Sprintf("%.1f s", step.Duration.Seconds())
								if step.Duration < time.Second {
									duration = fmt.Sprintf("%d ms", max(1, step.Duration.Milliseconds()))
								}
								ui.Text(c, duration).FontSize(10.5).TextColor(pal.Faint)
							}
						})
					})
				}
			})
			featureNote(c, tr("直连失败而代理成功时，代理仍可正常使用。代理请求遵循当前路由规则，直连模式或 DIRECT 规则不会强制经过节点。"))
		})
	})
	if a.diagError != "" {
		extensionError(c, a.diagError)
	}
	if a.diagNotice != "" {
		settingsFeedback(c, a.diagNotice, false)
	}
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		softButton(c, tr("复制诊断报告")).Disabled(!hasResult || running).OnClick(func() {
			a.services().WriteClipboard(diagnosticReport(steps))
			a.diagNotice = tr("已复制脱敏报告")
		})
		ui.Spacer(c)
		if running {
			ui.Text(c, tr("正在逐项检测…")).FontSize(11).TextColor(pal.Muted)
			softButton(c, tr("取消诊断")).OnClick(a.cancelDiagnostics)
		} else {
			label := tr("开始诊断")
			if hasResult {
				label = tr("重新诊断")
			}
			softPrimaryButton(c, label).OnClick(a.startDiagnostics)
		}
	})
}

func diagnosticSummary(steps []diagnostic.Step, running bool) string {
	passed, failed := 0, 0
	canceled := false
	for _, step := range steps {
		if running && step.State == diagnostic.Running {
			return tr("正在检测：") + tr(step.Name)
		}
		if step.State == diagnostic.Passed {
			passed++
		}
		if step.State == diagnostic.Failed {
			failed++
		}
		canceled = canceled || step.Detail == "已取消"
	}
	if running {
		return tr("正在准备下一项检测…")
	}
	if canceled {
		return tr("诊断已取消；已完成的检测结果仍然保留。")
	}
	if len(steps) == 7 && steps[6].State == diagnostic.Passed && steps[5].State == diagnostic.Failed {
		return tr("代理请求通过，直连请求失败；可滚动查看详细结果。")
	}
	return fmt.Sprintf(tr("本轮检测：%d 项通过，%d 项失败；其余为提示或跳过。"), passed, failed)
}
