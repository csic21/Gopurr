package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/mesh"
	"gopurr/internal/securefs"
)

var meshConnectionKinds = []string{"ssh", "rdp", "vnc"}

func (a *App) loadMeshShortcuts() {
	if a.meshShortcutsReady {
		return
	}
	a.meshShortcutsReady = true
	a.meshShortcuts = map[string]mesh.Shortcut{}
	path := filepath.Join(a.dir, "mesh-shortcuts.json")
	if err := securefs.PrivateFile(path); err != nil {
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() > 1<<20 {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var saved map[string]mesh.Shortcut
	if json.Unmarshal(raw, &saved) == nil && len(saved) <= 200 {
		a.meshShortcuts = saved
	}
}

func (a *App) rememberMeshShortcut(d mesh.Device, shortcut mesh.Shortcut) error {
	if err := shortcut.Validate(d); err != nil {
		return err
	}
	a.loadMeshShortcuts()
	next := make(map[string]mesh.Shortcut, len(a.meshShortcuts)+1)
	for id, s := range a.meshShortcuts {
		next[id] = s
	}
	shortcut.LastUsed = time.Now()
	next[d.ID] = shortcut
	if len(next) > 200 {
		var ids []string
		for id := range next {
			if id != d.ID {
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return next[ids[i]].LastUsed.Before(next[ids[j]].LastUsed) })
		delete(next, ids[0])
	}
	return a.saveMeshShortcuts(next)
}

func (a *App) forgetMeshShortcut(id string) error {
	a.loadMeshShortcuts()
	if _, ok := a.meshShortcuts[id]; !ok {
		return nil
	}
	next := make(map[string]mesh.Shortcut, len(a.meshShortcuts))
	for key, s := range a.meshShortcuts {
		if key != id {
			next[key] = s
		}
	}
	return a.saveMeshShortcuts(next)
}

func (a *App) saveMeshShortcuts(next map[string]mesh.Shortcut) error {
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := securefs.WriteFile(filepath.Join(a.dir, "mesh-shortcuts.json"), raw, 0600); err != nil {
		return err
	}
	a.meshShortcuts = next
	return nil
}

func (a *App) openMeshQuick(d mesh.Device) {
	a.loadMeshShortcuts()
	s, ok := a.meshShortcuts[d.ID]
	if !ok || s.Validate(d) != nil {
		s = mesh.DefaultShortcut(d.OS)
	}
	a.meshQuickDevice, a.meshQuick = d, true
	a.meshQuickKind = 0
	for i, kind := range meshConnectionKinds {
		if s.Kind == kind {
			a.meshQuickKind = i
		}
	}
	a.meshQuickPort, a.meshQuickUser, a.meshQuickRemember = strconv.Itoa(int(s.Port)), s.User, true
	a.meshQuickError, a.meshQuickNotice = "", ""
	a.meshLaunchGeneration++
	a.cancelMeshProbe()
}

func (a *App) selectedMeshShortcut() (mesh.Shortcut, error) {
	if a.meshQuickKind < 0 || a.meshQuickKind >= len(meshConnectionKinds) {
		return mesh.Shortcut{}, errors.New(tr("请选择连接方式"))
	}
	port, err := strconv.ParseUint(a.meshQuickPort, 10, 16)
	if err != nil || port == 0 {
		return mesh.Shortcut{}, errors.New(tr("端口须为 1–65535"))
	}
	s := mesh.Shortcut{Kind: meshConnectionKinds[a.meshQuickKind], Port: uint16(port), User: a.meshQuickUser}
	if s.Kind != "ssh" {
		s.User = ""
	}
	return s, s.Validate(a.meshQuickDevice)
}

func (a *App) currentMeshPeer(id string) (mesh.Device, bool) {
	if a.meshClient == nil {
		return mesh.Device{}, false
	}
	for _, d := range a.meshClient.Snapshot().Status.Peers {
		if d.ID == id {
			return d, true
		}
	}
	return mesh.Device{}, false
}

func (a *App) launchMeshShortcut(c *ui.Context, d mesh.Device, s mesh.Shortcut, remember bool) {
	a.meshQuickError, a.meshQuickNotice = "", ""
	current, found := a.currentMeshPeer(d.ID)
	if !found {
		a.openMeshQuick(d)
		a.meshQuickError = tr("设备已不在当前网络中，请刷新后重试")
		return
	}
	d = current
	if err := s.Validate(d); err != nil {
		a.meshQuickError = err.Error()
		return
	}
	// Login and address changes can precede the background route tick. Apply
	// the current peer routes before handing an address to a native client.
	a.reconcileMeshRoutes()
	if !a.meshAccessReady() {
		if !a.meshQuick {
			a.openMeshQuick(d)
		}
		a.meshQuickError = tr("请先在此处准备本机网络，再打开客户端")
		if a.meshRouteError != "" {
			a.meshQuickError = a.meshRouteError
		}
		return
	}
	ips, addr, password := a.meshRuntimeRoutes()
	if strings.Join(ips, ",")+addr+password != a.meshRouteKey {
		if !a.meshQuick {
			a.openMeshQuick(d)
		}
		a.meshQuickError = tr("组网路由仍在更新，请稍后重试")
		return
	}
	a.meshLaunchGeneration++
	generation := a.meshLaunchGeneration
	complete := func(err error) {
		active := generation == a.meshLaunchGeneration && !a.meshGuide && !a.meshPrepareConfirm && !a.meshConfirmAllow
		if err != nil {
			if !active {
				return
			}
			if !a.meshQuick {
				a.openMeshQuick(d)
			}
			a.meshQuickError = tr("未能打开客户端；请确认已安装对应客户端，也可复制地址手动连接")
			return
		}
		var saveErr error
		if remember {
			saveErr = a.rememberMeshShortcut(d, s)
		} else {
			saveErr = a.forgetMeshShortcut(d.ID)
		}
		if !active {
			return
		}
		a.meshQuickNotice = tr("已请求打开客户端，请在客户端完成登录")
		if saveErr != nil {
			a.meshQuickError = tr("已请求打开客户端，但快捷设置未能保存")
		}
	}
	if s.Kind == "ssh" && runtime.GOOS == "windows" {
		launch := a.meshSSHLaunch
		if launch == nil {
			launch = mesh.LaunchSSH
		}
		complete(launch(d, s))
		return
	}
	uri := ""
	var err error
	if s.Kind == "rdp" {
		var data []byte
		data, err = s.RDPFile(d)
		if err == nil {
			dir := filepath.Join(a.dir, "mesh-shortcuts")
			err = securefs.PrivateDir(dir)
			if err == nil {
				hash := sha256.Sum256([]byte(d.ID))
				path := filepath.Join(dir, hex.EncodeToString(hash[:12])+".rdp")
				err = securefs.WriteFile(path, data, 0600)
				p := filepath.ToSlash(path)
				if runtime.GOOS == "windows" {
					p = "/" + p
				}
				uri = (&url.URL{Scheme: "file", Path: p}).String()
			}
		}
	} else {
		uri, err = s.URI(d)
	}
	if err != nil {
		a.meshQuickError = err.Error()
		return
	}
	c.OpenURLThen(uri, complete)
}

func (a *App) cancelMeshProbe() {
	a.meshProbeMu.Lock()
	defer a.meshProbeMu.Unlock()
	if a.meshProbeCancel != nil {
		a.meshProbeCancel()
	}
	a.meshProbeSerial++
	a.meshProbeBusy = false
	a.meshProbeResult = mesh.ProbeResult{}
}

func (a *App) startMeshProbe(d mesh.Device, s mesh.Shortcut) {
	a.cancelMeshProbe()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	a.meshProbeMu.Lock()
	a.meshProbeBusy, a.meshProbeCancel = true, cancel
	id := a.meshProbeSerial
	a.meshProbeMu.Unlock()
	client := a.meshClient
	go func() {
		defer cancel()
		result := client.Probe(ctx, d.ID, s.Kind, s.Port)
		a.meshProbeMu.Lock()
		if a.meshProbeSerial == id {
			a.meshProbeBusy, a.meshProbeResult = false, result
		}
		a.meshProbeMu.Unlock()
		a.invalidate()
	}()
}

func (a *App) meshProbeSnapshot() (bool, mesh.ProbeResult) {
	a.meshProbeMu.Lock()
	defer a.meshProbeMu.Unlock()
	return a.meshProbeBusy, a.meshProbeResult
}

func (a *App) meshQuickDialog(c *ui.Context, snap mesh.Snapshot) {
	if !a.meshQuick {
		return
	}
	pal := paletteFor(c)
	width, height := c.Size()
	d, known := a.currentMeshPeer(a.meshQuickDevice.ID)
	if known {
		a.meshQuickDevice = d
	} else {
		d = a.meshQuickDevice
	}
	ui.Overlay(c, func() {
		back := ui.Box(c.Key("mesh-quick-dialog")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().Modal()
		modalBackdrop(back)
		closeDialog := func() { a.meshQuick = false; a.meshLaunchGeneration++; a.cancelMeshProbe() }
		if back.OverlayShortcut(0, ui.KeyEscape) {
			closeDialog()
		}
		back.Children(func() {
			panel := ui.Column(c).Width(min(float32(560), width-64)).MaxHeight(height-72).Padding(24).Gap(16).Radius(14).Background(pal.Surface).Border(1, pal.Border).Role(ui.RoleDialog).Label(tr("连接组网设备"))
			modalPanel(c, panel)
			panel.Children(func() {
				ui.Text(c, tr("连接 ")+d.Name).FillWidth().MaxLines(2).Ellipsis("…").Tooltip(d.Name).FontSize(18).FontWeight(600).TextColor(pal.Text)
				ui.Scroll(c).FillWidth().Shrink(1).MinHeight(0).Children(func() {
					ui.Column(c).FillWidth().Gap(12).Children(func() {
						ui.Text(c, meshOwner(d)+" · "+d.IP()).FillWidth().SingleLine().Ellipsis("…").Tooltip(meshOwner(d) + " · " + d.IP()).FontSize(11.5).TextColor(pal.Muted)
						previous := a.meshQuickKind
						softSegmented(c, &a.meshQuickKind, "SSH", tr("远程桌面"), tr("屏幕共享")).Label(tr("组网连接方式"))
						if previous != a.meshQuickKind {
							a.meshQuickPort = []string{"22", "3389", "5900"}[a.meshQuickKind]
							a.meshQuickError, a.meshQuickNotice = "", ""
							a.cancelMeshProbe()
						}
						ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
							ui.Text(c, tr("服务端口")).FontSize(12).TextColor(pal.Text)
							before := a.meshQuickPort
							ui.TextInput(c, &a.meshQuickPort).Width(100).Label(tr("连接服务端口"))
							if before != a.meshQuickPort {
								a.cancelMeshProbe()
							}
							if a.meshQuickKind == 0 {
								ui.TextInput(c, &a.meshQuickUser).Grow(1).MinWidth(0).Label(tr("SSH 用户名")).Placeholder(tr("用户名（留空用客户端默认）"))
							}
						})
						remember := a.meshQuickRemember
						ui.Checkbox(c, &remember, tr("记住连接方式（不保存密码）")).Label(tr("记住组网连接方式"))
						a.meshQuickRemember = remember
						if !known {
							ui.Text(c, tr("设备已不在当前网络中，请刷新")).FillWidth().FontSize(11.5).TextColor(pal.Warn)
						}
						if !a.meshAccessReady() {
							a.meshAccessPreparation(c, true)
						}
						busy, result := a.meshProbeSnapshot()
						if busy {
							ui.Text(c, tr("正在检测组网通路，最多等待 8 秒…")).FillWidth().FontSize(11.5).TextColor(pal.Muted)
						}
						if result.Message != "" {
							color := pal.Muted
							if result.State == "verified" {
								color = pal.Success
							} else if result.State == "failed" {
								color = pal.Warn
							}
							ui.Text(c, result.Message).FillWidth().FontSize(11.5).TextColor(color)
						}
						if a.meshQuickError != "" {
							ui.Text(c, a.meshQuickError).FillWidth().FontSize(11.5).TextColor(pal.Danger)
						}
						if a.meshQuickNotice != "" {
							ui.Text(c, a.meshQuickNotice).FillWidth().FontSize(11.5).TextColor(pal.Success)
						}
						ui.Text(c, tr("对方需开放相应端口并启动服务；客户端会独立验证账号和主机身份。远程桌面需本机已安装兼容客户端。")).FillWidth().FontSize(11).TextColor(pal.Muted)
					})
				})
				ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
					softButton(c, tr("关闭")).OnClick(closeDialog)
					softButton(c, tr("复制地址")).OnClick(func() {
						if s, err := a.selectedMeshShortcut(); err == nil {
							c.WriteClipboard(s.Address(d))
							a.meshQuickNotice = tr("连接地址已复制")
						} else {
							a.meshQuickError = err.Error()
						}
					})
					busy, _ := a.meshProbeSnapshot()
					label := tr("检测连接")
					if busy {
						label = tr("取消检测")
					}
					softButton(c, label).Disabled(!known || !snap.Status.Running).OnClick(func() {
						if busy {
							a.cancelMeshProbe()
							return
						}
						if s, err := a.selectedMeshShortcut(); err == nil {
							a.startMeshProbe(d, s)
						} else {
							a.meshQuickError = err.Error()
						}
					})
					softPrimaryButton(c, tr("打开客户端")).Disabled(!known || !snap.Status.Running || !a.meshAccessReady()).OnClick(func() {
						if s, err := a.selectedMeshShortcut(); err == nil {
							a.launchMeshShortcut(c, d, s, a.meshQuickRemember)
						} else {
							a.meshQuickError = err.Error()
						}
					})
				})
			})
		})
	})
}
