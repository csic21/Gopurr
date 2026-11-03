package app

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"gopurr/internal/mesh"
	"gopurr/internal/remote"
)

var remoteBindOptions = []string{"0.0.0.0", "127.0.0.1"}

func remoteBindValue(idx int) string {
	if idx >= 0 && idx < len(remoteBindOptions) {
		return remoteBindOptions[idx]
	}
	return remote.DefaultBind
}

func remoteBindIndex(bind string) int {
	switch strings.TrimSpace(bind) {
	case "127.0.0.1", "localhost", "::1":
		return 1
	default:
		return 0
	}
}

type remoteSettingsState struct {
	remoteCfg    remote.Config
	remoteBase   remote.Config
	remotePath   string
	remoteServer *remote.Server
	remoteMu     sync.Mutex

	remoteEnabled      bool
	remoteBindIdx      int
	remotePortDraft    string
	remoteToken        string
	remoteTokenVisible bool
	remoteBusy         bool
	remoteError        string
	remoteStatus       string
	remoteConfirmToken bool
	remoteConfirmMesh  bool
	remoteShow         bool
}

func (a *App) loadRemoteSettings() {
	if a.remotePath == "" {
		return
	}
	cfg, err := remote.LoadConfig(a.remotePath)
	if err != nil {
		a.remoteError = tr("读取远程访问设置失败：") + err.Error()
	}
	cfg = cfg.Normalize()
	a.remoteCfg, a.remoteBase = cfg, cfg
	a.remoteEnabled = cfg.Enabled
	a.remoteBindIdx = remoteBindIndex(cfg.Bind)
	a.remotePortDraft = strconv.Itoa(cfg.Port)
	a.remoteToken = cfg.Token
}

func (a *App) remoteConfigSnapshot() remote.Config {
	a.remoteMu.Lock()
	defer a.remoteMu.Unlock()
	return a.remoteCfg
}

func (a *App) remoteServerSnapshot() *remote.Server {
	a.remoteMu.Lock()
	defer a.remoteMu.Unlock()
	return a.remoteServer
}

func (a *App) remoteDraft() (remote.Config, error) {
	port, err := strconv.Atoi(strings.TrimSpace(a.remotePortDraft))
	if err != nil || port < 1 || port > 65535 {
		return remote.Config{}, fmt.Errorf("%s", tr("远程访问端口须为 1–65535 的整数"))
	}
	bind := remoteBindValue(a.remoteBindIdx)
	if err := remote.ValidateBind(bind); err != nil {
		return remote.Config{}, fmt.Errorf("%s", tr("监听地址须为 IP 地址"))
	}
	if port == a.mixedPort() {
		return remote.Config{}, fmt.Errorf("%s", tr("远程访问端口不能与混合代理端口相同"))
	}
	cfg := remote.Config{Enabled: a.remoteEnabled, Bind: bind, Port: port, Token: a.remoteToken}.Normalize()
	if err := cfg.Validate(); err != nil {
		return remote.Config{}, fmt.Errorf("%s", tr("远程访问设置无效"))
	}
	return cfg, nil
}

func (a *App) remoteSettingsDirty() bool {
	if a.remoteEnabled != a.remoteBase.Enabled {
		return true
	}
	if !a.remoteEnabled {
		return false
	}
	if remoteBindValue(a.remoteBindIdx) != a.remoteBase.Bind {
		return true
	}
	port, err := strconv.Atoi(strings.TrimSpace(a.remotePortDraft))
	return err != nil || port != a.remoteBase.Port
}

func (a *App) requestRemoteSettings() {
	if a.remoteBusy || a.remotePath == "" || a.ShuttingDown() {
		return
	}
	cfg, err := a.remoteDraft()
	if err != nil {
		a.remoteError = err.Error()
		a.invalidate()
		return
	}
	if cfg.Enabled && cfg.Token == "" {
		if err := cfg.EnsureToken(); err != nil {
			a.remoteError = tr("无法生成访问令牌：") + err.Error()
			a.invalidate()
			return
		}
	}
	a.remoteBusy, a.remoteError, a.remoteStatus = true, "", ""
	a.invalidate()
	oldCfg := a.remoteConfigSnapshot()
	oldServer := a.remoteServerSnapshot()
	path := a.remotePath
	go func() {
		if oldServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = oldServer.Stop(ctx)
			cancel()
		}
		var srv *remote.Server
		if cfg.Enabled {
			srv = remote.New(cfg, webHost{a})
			srv.ErrorHandler = a.remoteRuntimeError
			if err := srv.Start(); err != nil {
				restored := a.restartRemote(oldCfg, oldServer != nil)
				a.finishRemoteChange(restored, oldCfg, fmt.Errorf(tr("远程访问服务启动失败：%w"), err))
				return
			}
		}
		if err := cfg.Save(path); err != nil {
			if srv != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				_ = srv.Stop(ctx)
				cancel()
			}
			restored := a.restartRemote(oldCfg, oldServer != nil)
			a.finishRemoteChange(restored, oldCfg, fmt.Errorf(tr("无法保存远程访问设置：%w"), err))
			return
		}
		a.finishRemoteChange(srv, cfg, nil)
	}()
}

// restartRemote puts the previous listener back when a new configuration fails
// to bind or save. Returning nil is fine: the caller reports the failure.
func (a *App) restartRemote(cfg remote.Config, hadServer bool) *remote.Server {
	if !cfg.Enabled || !hadServer {
		return nil
	}
	srv := remote.New(cfg, webHost{a})
	srv.ErrorHandler = a.remoteRuntimeError
	if err := srv.Start(); err != nil {
		return nil
	}
	return srv
}

func (a *App) finishRemoteChange(srv *remote.Server, base remote.Config, err error) {
	runOnMain(func() {
		if err != nil {
			a.remoteBusy = false
			a.remoteError = err.Error()
			a.remoteStatus = ""
			a.remoteMu.Lock()
			a.remoteServer = srv
			a.remoteMu.Unlock()
			a.invalidate()
			return
		}
		a.remoteMu.Lock()
		a.remoteServer = srv
		a.remoteCfg = base
		a.remoteMu.Unlock()
		a.remoteBase = base
		a.remoteEnabled = base.Enabled
		a.remoteBindIdx = remoteBindIndex(base.Bind)
		a.remotePortDraft = strconv.Itoa(base.Port)
		a.remoteToken = base.Token
		a.remoteBusy = false
		a.remoteError = ""
		if base.Enabled {
			a.remoteStatus = tr("远程访问已开启")
		} else {
			a.remoteStatus = tr("远程访问已关闭")
		}
		a.invalidate()
	})
}

func (a *App) remoteRuntimeError(err error) {
	runOnMain(func() {
		a.remoteError = tr("远程访问服务异常退出：") + err.Error()
		a.invalidate()
	})
}

func (a *App) startRemoteAtBoot() {
	cfg := a.remoteConfigSnapshot()
	if !cfg.Enabled {
		return
	}
	runOnMain(func() { a.remoteBusy = true })
	var mixedPort int
	runOnMain(func() { mixedPort = a.mixedPort() })
	if cfg.Port == mixedPort {
		a.finishRemoteChange(nil, cfg, fmt.Errorf("%s", tr("远程访问端口不能与混合代理端口相同")))
		return
	}
	if cfg.Token == "" {
		if err := cfg.EnsureToken(); err != nil {
			a.finishRemoteChange(nil, cfg, fmt.Errorf(tr("无法生成访问令牌：%w"), err))
			return
		}
	}
	srv := remote.New(cfg, webHost{a})
	srv.ErrorHandler = a.remoteRuntimeError
	if err := srv.Start(); err != nil {
		a.finishRemoteChange(nil, cfg, fmt.Errorf(tr("远程访问服务启动失败：%w"), err))
		return
	}
	if err := cfg.Save(a.remotePath); err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = srv.Stop(ctx)
		cancel()
		a.finishRemoteChange(nil, cfg, fmt.Errorf(tr("无法保存远程访问设置：%w"), err))
		return
	}
	a.finishRemoteChange(srv, cfg, nil)
}

func (a *App) stopRemoteServer() {
	srv := a.remoteServerSnapshot()
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = srv.Stop(ctx)
	a.remoteMu.Lock()
	a.remoteServer = nil
	a.remoteMu.Unlock()
}

func (a *App) regenerateRemoteToken() {
	if a.remoteBusy {
		return
	}
	token, err := remote.GenerateToken()
	if err != nil {
		a.remoteError = tr("无法生成访问令牌：") + err.Error()
		a.invalidate()
		return
	}
	a.remoteToken = token
	a.remoteTokenVisible = true
	if a.remoteEnabled {
		a.requestRemoteSettings()
		return
	}
	cfg := a.remoteConfigSnapshot()
	cfg.Token = token
	go func() {
		err := cfg.Save(a.remotePath)
		runOnMain(func() {
			if err != nil {
				a.remoteError = tr("无法保存远程访问设置：") + err.Error()
			} else {
				a.remoteMu.Lock()
				a.remoteCfg = cfg
				a.remoteMu.Unlock()
				a.remoteBase.Token = token
				a.remoteStatus = tr("访问令牌已更新")
			}
			a.invalidate()
		})
	}()
}

func (a *App) remoteAccessURLs() []string {
	cfg := a.remoteConfigSnapshot()
	if !cfg.Enabled {
		return nil
	}
	urls := remote.AccessURLs(cfg)
	if len(urls) > 6 {
		urls = urls[:6]
	}
	if a.meshClient == nil {
		return urls
	}
	status := a.meshClient.Snapshot().Status
	if !status.Running {
		return urls
	}
	if ip := status.Self.IP(); ip != "" {
		u := "http://" + net.JoinHostPort(ip, strconv.Itoa(cfg.Port))
		if !slices.Contains(urls, u) {
			urls = append(urls, u)
		}
	}
	return urls
}

func (a *App) openRemotePortInMesh() {
	if a.meshClient == nil {
		a.remoteError = tr("当前版本没有内置组网")
		a.invalidate()
		return
	}
	snap := a.meshClient.Snapshot()
	if !snap.Status.Running {
		a.remoteError = tr("请先在组网页连接组网")
		a.invalidate()
		return
	}
	cfg := a.remoteConfigSnapshot()
	if !cfg.Enabled || cfg.Port < 1 {
		a.remoteError = tr("请先保存并开启远程访问")
		a.invalidate()
		return
	}
	if a.remoteServerSnapshot() == nil {
		a.remoteError = tr("远程访问服务尚未就绪")
		a.invalidate()
		return
	}
	services := append([]mesh.Service(nil), snap.Status.Services...)
	target := mesh.Service{Protocol: "tcp", Port: uint16(cfg.Port)}
	if !slices.Contains(services, target) {
		services = append(services, target)
	}
	if len(services) > 16 {
		a.remoteError = tr("组网服务端口最多 16 个")
		a.invalidate()
		return
	}
	a.meshClient.SetServices(mesh.FormatServices(services), true)
	a.remoteError = ""
	a.remoteStatus = tr("已请求在组网中开放端口；请确认组网状态")
	a.invalidate()
}

func (a *App) remoteSummary() string {
	if a.remoteBusy {
		return tr("正在应用…")
	}
	if a.remoteError != "" {
		return a.remoteError
	}
	if !a.remoteEnabled {
		return tr("默认关闭；通过浏览器远程查看与调整")
	}
	port := a.remoteBase.Port
	if port == 0 {
		port = a.remoteConfigSnapshot().Port
	}
	if port == 0 {
		port = remote.DefaultPort
	}
	return fmt.Sprintf(tr("已开启 · 端口 %d"), port)
}

func (a *App) remoteSettingsPanel(c *ui.Context) {
	pal := paletteFor(c)
	if a.remotePortDraft == "" {
		a.remotePortDraft = strconv.Itoa(remote.DefaultPort)
	}
	settingsGroup(c, "", func() {
		settingsRow(c, tr("启用远程访问"), tr("通过浏览器查看状态、切换节点与网络开关"), func() {
			softSwitch(c, &a.remoteEnabled).Label(tr("启用远程访问开关")).Disabled(a.remoteBusy)
		})
		if a.remoteEnabled {
			hairline(c)
			settingsRow(c, tr("监听地址"), tr("所有网卡允许组网、局域网和公网端口映射；仅本机最安全"), func() {
				softSegmented(c, &a.remoteBindIdx, tr("所有网卡"), tr("仅本机")).
					Label(tr("远程访问监听地址")).Disabled(a.remoteBusy)
			})
			hairline(c)
			settingsRow(c, tr("端口"), tr("默认 7899；不能与混合代理端口相同"), func() {
				ui.TextInput(c, &a.remotePortDraft).Width(96).Label(tr("远程访问端口")).Disabled(a.remoteBusy)
			})
			hairline(c)
			settingsRow(c, tr("访问令牌"), tr("登录网页时输入；泄露等同于代理控制权"), func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					label := tr("显示")
					if a.remoteTokenVisible {
						label = tr("隐藏")
					}
					softButton(c, label).Disabled(a.remoteToken == "").OnClick(func() {
						a.remoteTokenVisible = !a.remoteTokenVisible
					})
					softButton(c, tr("复制令牌")).Disabled(a.remoteToken == "").OnClick(func() {
						a.services().WriteClipboard(a.remoteToken)
						a.remoteStatus = tr("访问令牌已复制")
					})
					softButton(c, tr("重新生成")).Disabled(a.remoteBusy).OnClick(func() {
						a.remoteConfirmToken = true
					})
				})
			})
			token := "••••••••••••••••"
			if a.remoteToken == "" {
				token = tr("保存并开启后生成")
			} else if a.remoteTokenVisible {
				token = a.remoteToken
			}
			ui.Column(c).FillWidth().Padding(0, 16, 14).Gap(4).Children(func() {
				ui.Text(c, token).FillWidth().Font("monospace").FontSize(11.5).TextColor(pal.Muted).Selectable().MaxLines(2)
				ui.Text(c, tr("令牌只保存在本机，请勿分享。")).FillWidth().FontSize(11).TextColor(pal.Faint)
			})
			hairline(c)
			urls := a.remoteAccessURLs()
			settingsRow(c, tr("访问地址"), tr("在浏览器中打开；公网地址由你的端口映射决定"), func() {
				softButton(c, tr("复制地址")).Disabled(len(urls) == 0).OnClick(func() {
					a.services().WriteClipboard(strings.Join(urls, "\n"))
					a.remoteStatus = tr("访问地址已复制")
				})
			})
			ui.Column(c).FillWidth().Padding(0, 16, 14).Gap(4).Children(func() {
				if len(urls) == 0 {
					ui.Text(c, tr("保存并开启后显示本机地址")).FillWidth().FontSize(11.5).TextColor(pal.Faint)
				} else {
					for _, u := range urls {
						ui.Text(c, u).FillWidth().Font("monospace").FontSize(11.5).TextColor(pal.Muted).Selectable().MaxLines(1)
					}
				}
				ui.Text(c, tr("通过组网访问还需在「组网 → 本机访问」中开放此端口；修改端口后需重新开放。")).FillWidth().FontSize(11).TextColor(pal.Faint)
			})
			hairline(c)
			settingsRow(c, tr("组网访问"), tr("把 Web 端口加入组网的入站访问授权"), func() {
				connected := a.meshClient != nil && a.meshClient.Snapshot().Status.Running
				softButton(c, tr("在组网中开放此端口")).Disabled(a.remoteBusy || !connected).OnClick(func() {
					a.remoteConfirmMesh = true
				})
			})
			ui.Text(c, tr("公网访问请使用 HTTPS 反向代理；直接暴露 HTTP 会泄露令牌和流量。")).
				FillWidth().Padding(0, 16, 14).FontSize(11).TextColor(pal.Danger)
		}
	})
}

func (a *App) remoteSettingsStatus(c *ui.Context) {
	if a.remoteError == "" && a.remoteStatus == "" {
		return
	}
	pal := paletteFor(c)
	message, color := a.remoteStatus, pal.Success
	if a.remoteError != "" {
		message, color = a.remoteError, pal.Danger
	}
	ui.Text(c, message).FillWidth().FontSize(11).TextColor(color)
}

func (a *App) remoteSaveBar(c *ui.Context) {
	pal := paletteFor(c)
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr("修改地址或端口后需要重新保存。")).Grow(1).MinWidth(0).FontSize(11).TextColor(pal.Faint)
		if a.remoteBusy {
			ui.Text(c, tr("正在应用…")).FontSize(11).TextColor(pal.Muted)
		}
		softPrimaryButton(c, tr("保存并应用")).Disabled(a.remoteBusy || !a.remoteSettingsDirty()).
			OnClick(a.requestRemoteSettings)
	})
}

func (a *App) remoteSettingsModal(c *ui.Context) {
	if !a.remoteShow {
		return
	}
	width, height := c.Size()
	scrollHeight := height - 230
	if scrollHeight < 180 {
		scrollHeight = 180
	}
	softModal(c, "remote-settings", &a.remoteShow, func() {
		ui.Column(c).Width(min(float32(600), width-32)).MaxHeight(height - 32).Padding(16).Gap(10).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(12).Children(func() {
				ui.Text(c, tr("远程 Web 控制台")).Grow(1).FontSize(18).FontWeight(600).TextColor(paletteFor(c).Text)
				softButton(c, tr("关闭")).OnClick(func() { a.remoteShow = false })
			})
			ui.Text(c, tr("浏览器访问需要访问令牌；默认关闭，修改后保存生效。")).FillWidth().FontSize(11.5).TextColor(paletteFor(c).Muted)
			ui.Scroll(c).FillWidth().Height(scrollHeight).Children(func() {
				ui.Column(c).FillWidth().Gap(10).Children(func() {
					a.remoteSettingsPanel(c)
				})
			})
			a.remoteSettingsStatus(c)
			a.remoteSaveBar(c)
		})
	})
}

func (a *App) remoteSettingsDialogs(c *ui.Context) {
	if softAlertDialog(c, &a.remoteConfirmToken, tr("重新生成访问令牌？"), tr("重新生成后，现有网页登录会立即失效。"), tr("取消"), tr("重新生成")) == 1 {
		a.regenerateRemoteToken()
	}
	if softAlertDialog(c, &a.remoteConfirmMesh, tr("允许组网设备访问 Web 控制台？"), tr("这会把 Web 端口加入组网的本机访问，并同时允许当前已授权的其他端口。只应在可信设备上使用。"), tr("取消"), tr("允许")) == 1 {
		a.openRemotePortInMesh()
	}
}
