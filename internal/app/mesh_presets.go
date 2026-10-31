package app

import (
	"slices"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/mesh"
)

var meshServicePresets = []struct {
	name, description string
	service           mesh.Service
}{
	{"SSH", "终端与 SFTP · 22/TCP", mesh.Service{Protocol: "tcp", Port: 22}},
	{"Windows 远程桌面", "远程桌面 · 3389/TCP", mesh.Service{Protocol: "tcp", Port: 3389}},
	{"macOS 屏幕共享", "屏幕共享 · 5900/TCP", mesh.Service{Protocol: "tcp", Port: 5900}},
}

func (a *App) openMeshServices(s mesh.Snapshot, allow bool) {
	a.meshServicesDraft, a.meshServicesError = mesh.FormatServices(s.Status.Services), ""
	a.meshConfirmAllow, a.meshEditServices = allow, !allow
	a.meshAdvancedServices = false
	for _, service := range s.Status.Services {
		preset := false
		for _, p := range meshServicePresets {
			if service == p.service {
				preset = true
			}
		}
		if !preset {
			a.meshAdvancedServices = true
		}
	}
}

func (a *App) meshServicePresets(c *ui.Context) {
	pal := paletteFor(c)
	services, parseErr := mesh.ParseServices(a.meshServicesDraft)
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		for _, preset := range meshServicePresets {
			ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
				selected := slices.Contains(services, preset.service)
				previous := selected
				ui.Checkbox(c, &selected, tr(preset.name)).Label(tr("授权 ") + tr(preset.name))
				if selected != previous {
					if parseErr != nil {
						a.meshServicesError = tr("请先修正自定义端口，再选择预设")
					} else {
						if selected {
							services = append(services, preset.service)
						} else {
							services = slices.DeleteFunc(services, func(s mesh.Service) bool { return s == preset.service })
						}
						a.meshServicesDraft, a.meshServicesError = mesh.FormatServices(services), ""
					}
				}
				ui.Text(c, tr(preset.description)).Grow(1).FontSize(11).TextColor(pal.Muted)
			})
		}
	})
	label := tr("自定义端口…")
	if a.meshAdvancedServices {
		label = tr("收起自定义端口")
	}
	softButton(c, label).OnClick(func() { a.meshAdvancedServices = !a.meshAdvancedServices })
	if a.meshAdvancedServices {
		ui.TextInput(c, &a.meshServicesDraft).FillWidth().Label(tr("授权端口")).Placeholder(tr("例如 8080/tcp, 5353/udp"))
		ui.Text(c, tr("逗号分隔，省略协议为 TCP；最多 16 个，不支持范围。")).FillWidth().FontSize(11).TextColor(pal.Muted)
	} else if a.meshServicesDraft != "" {
		ui.Text(c, tr("已选端口：")+a.meshServicesDraft).FillWidth().FontSize(11).TextColor(pal.Muted)
	}
	ui.Text(c, tr("仅授权访问，不会替你启动系统服务。端口转发到本机 127.0.0.1 的同端口，包括仅监听本地的服务。")).FillWidth().FontSize(11).TextColor(pal.Muted)
}
