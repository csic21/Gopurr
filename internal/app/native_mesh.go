package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"

	"gopurr/internal/mesh"
	"gopurr/internal/profile"
	"gopurr/internal/securefs"
	"gopurr/internal/service"
)

func (a *App) nativeMeshCall(ctx context.Context, method string, p map[string]json.RawMessage) (any, error) {
	if method == "desktop.mesh.probe" {
		var client meshClient
		var port uint16
		if err := json.Unmarshal(p["port"], &port); err != nil || port == 0 {
			return nil, errors.New("port must be 1–65535")
		}
		_, err := nativeMain(ctx, func() (any, error) {
			client = a.meshClient
			if client == nil {
				return nil, errors.New("mesh unavailable")
			}
			return nil, nil
		})
		if err != nil {
			return nil, err
		}
		return nativeJSON(client.Probe(ctx, nativeString(p, "id"), nativeString(p, "protocol"), port)), nil
	}
	return nativeMain(ctx, func() (any, error) {
		if a.meshClient == nil {
			return nil, errors.New("mesh unavailable")
		}
		switch method {
		case "desktop.mesh.action":
			switch action := nativeString(p, "action"); action {
			case "refresh":
				a.meshClient.Refresh()
			case "clearHistory":
				a.meshClient.ClearHistory()
			case "connect":
				a.beginMeshConnect()
			case "disconnect":
				a.meshClient.Do("disconnect")
			case "block", "protect", "shieldsUp":
				a.meshClient.Do("block")
			case "allow", "shieldsDown":
				// As in the original UI, opening access must specify actual ports.
				services := a.meshClient.Snapshot().Status.Services
				if len(services) == 0 {
					return nil, errors.New(tr("请先连接组网并明确授权本机服务端口"))
				}
				a.meshClient.SetServices(mesh.FormatServices(services), true)
			case "login":
				auth := a.meshClient.Snapshot().Status.AuthURL
				if !mesh.ValidAuthURL(auth) {
					return nil, errors.New(tr("登录地址未通过验证，请重新连接"))
				}
				return map[string]any{"url": auth}, nil
			case "prepare":
				if busyNetwork(a) {
					return nil, errors.New(tr("网络操作进行中，请稍后重试"))
				}
				if a.k == nil {
					return nil, errors.New("kernel unavailable")
				}
				if !a.k.IsRoot() {
					if !service.Available() {
						return nil, errors.New(tr("当前平台不支持 TUN 系统服务"))
					}
					a.meshPreparePending = true
					a.installService()
				} else if !a.applyMeshPreparation() {
					return nil, errors.New(a.meshPrepareError)
				}
			default:
				return nil, fmt.Errorf("unknown mesh action: %s", action)
			}
		case "desktop.mesh.services":
			raw := nativeString(p, "protocol")
			if raw == "" {
				raw = nativeString(p, "services")
			}
			if _, err := mesh.ParseServices(raw); err != nil {
				return nil, err
			}
			if nativeBool(p, "enabled") && raw == "" {
				return nil, errors.New(tr("请先连接组网并明确授权本机服务端口"))
			}
			a.meshClient.SetServices(raw, nativeBool(p, "enabled"))
		case "desktop.mesh.block":
			id := nativeString(p, "id")
			if _, found := a.currentMeshPeer(id); !found {
				return nil, errors.New(tr("设备已不在当前网络中，请刷新后重试"))
			}
			a.meshClient.SetDeviceBlocked(id, nativeBool(p, "blocked"))
		case "desktop.mesh.autoConnect":
			a.meshClient.SetAutoConnect(nativeBool(p, "enabled"))
		case "desktop.mesh.shortcut.forget":
			return nil, a.forgetMeshShortcut(nativeString(p, "id"))
		case "desktop.mesh.shortcut", "desktop.mesh.shortcut.save":
			id := nativeString(p, "id")
			device, found := a.currentMeshPeer(id)
			if !found {
				return nil, errors.New(tr("设备已不在当前网络中，请刷新后重试"))
			}
			var port uint16
			if err := json.Unmarshal(p["port"], &port); err != nil {
				return nil, err
			}
			shortcut := mesh.Shortcut{Kind: nativeString(p, "kind"), Port: port, User: nativeString(p, "user")}
			if err := shortcut.Validate(device); err != nil {
				return nil, err
			}
			if method == "desktop.mesh.shortcut.save" {
				return nil, a.rememberMeshShortcut(device, shortcut)
			}
			a.reconcileMeshRoutes()
			if !a.meshAccessReady() {
				return nil, errors.New(tr("请先在此处准备本机网络，再打开客户端"))
			}
			var uri string
			if shortcut.Kind == "rdp" {
				data, err := shortcut.RDPFile(device)
				if err != nil {
					return nil, err
				}
				dir := filepath.Join(a.dir, "mesh-shortcuts")
				if err := securefs.PrivateDir(dir); err != nil {
					return nil, err
				}
				hash := sha256.Sum256([]byte(id))
				path := filepath.Join(dir, hex.EncodeToString(hash[:12])+".rdp")
				if err := securefs.WriteFile(path, data, 0600); err != nil {
					return nil, err
				}
				uri = (&url.URL{Scheme: "file", Path: path}).String()
			} else {
				var err error
				uri, err = shortcut.URI(device)
				if err != nil {
					return nil, err
				}
			}
			if nativeBool(p, "remember") {
				if err := a.rememberMeshShortcut(device, shortcut); err != nil {
					return nil, err
				}
			} else if err := a.forgetMeshShortcut(id); err != nil {
				return nil, err
			}
			return map[string]any{"url": uri, "address": shortcut.Address(device)}, nil
		}
		return map[string]any{"accepted": true}, nil
	})
}

// Host lifecycle hooks are configured by the main-thread desktop launcher.
func (a *App) NativeSetAutoLaunchHandler(handler func(bool) error) { a.applyAutoLaunch = handler }
func (a *App) NativePage() string                                  { return a.page }
func (a *App) NativeSetPage(page string) {
	for _, n := range navItems {
		if n.id == page {
			a.page = page
			a.invalidate()
			return
		}
	}
}

// NativeImportURL validates an external request without downloading it or
// constructing the legacy editor. The host delivers this payload to the NSH
// subscription editor, which owns the draft and asks the user to submit it.
// Like other lifecycle hooks this method is called by the main-thread host.
func (a *App) NativeImportURL(raw string) (map[string]any, error) {
	if a.ShuttingDown() {
		return nil, errors.New(tr("应用正在退出"))
	}
	link, err := profile.ParseImportLink(raw)
	if err != nil {
		return nil, err
	}
	a.NativeSetPage("subs")
	return map[string]any{"url": link.URL, "name": link.Name}, nil
}
