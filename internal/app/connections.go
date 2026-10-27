package app

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/core"
)

type connectionState struct {
	connList        ui.ListState
	connQuery       string
	connFetched     time.Time
	connRows        []core.Conn
	connError       string
	connSelected    core.Conn
	showConn        bool
	confirmCloseAll bool
}

func filterConnections(rows []core.Conn, query string) []core.Conn {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return rows
	}
	out := make([]core.Conn, 0, len(rows))
	for _, cn := range rows {
		text := strings.Join([]string{cn.Host, cn.Process, cn.ProcessPath, cn.SourceIP, cn.DestinationIP, cn.Rule, cn.RulePayload, cn.Chain, cn.Network}, " ")
		if strings.Contains(strings.ToLower(text), q) {
			out = append(out, cn)
		}
	}
	return out
}

func (a *App) refreshConnections() {
	rows, err := a.k.ConnectionsWithError()
	a.connFetched = time.Now()
	if err != nil {
		a.connError = "读取连接失败: " + err.Error()
		return
	}
	a.connRows, a.connError = rows, ""
}

func (a *App) connsPage(c *ui.Context) {
	pal := paletteFor(c)
	a.connList.KeepOffset = true
	if time.Since(a.connFetched) >= 2*time.Second {
		a.refreshConnections()
	}
	rows := filterConnections(a.connRows, a.connQuery)
	a.pageHeader(c, "连接", fmt.Sprintf("%d 条活跃会话 · 显示 %d 条", len(a.connRows), len(rows)), func() {
		ui.Button(c, "断开全部…").Disabled(len(a.connRows) == 0).OnClick(func() { a.confirmCloseAll = true })
	})
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.TextInput(c, &a.connQuery).Grow(1).MinWidth(0).Placeholder("搜索主机、进程、IP、规则或链路").Label("搜索连接")
		ui.Button(c, "刷新").OnClick(a.refreshConnections)
	})
	if a.connError != "" {
		settingsFeedback(c, a.connError, true)
	}
	if len(rows) == 0 {
		if a.connQuery != "" {
			emptyState(c, "没有匹配的连接", "尝试主机名、进程名或 IP 地址。")
		} else {
			emptyState(c, "暂无连接", "打开系统代理或 TUN 后，这里会实时出现会话。")
		}
	} else {
		ui.Row(c).FillWidth().PaddingX(12).Gap(8).Children(func() {
			ui.Text(c, "主机 / 进程").Grow(1).MinWidth(0).FontSize(11).TextColor(pal.Faint)
			ui.Text(c, "上行").Width(64).FontSize(11).TextColor(pal.Faint).TextAlign(ui.End)
			ui.Text(c, "下行").Width(64).FontSize(11).TextColor(pal.Faint).TextAlign(ui.End)
			ui.Box(c).Width(52)
		})
		ui.Column(c).FillWidth().FillHeight().Radius(12).Background(pal.Surface).Border(1, pal.Border).Children(func() {
			ui.List(c, &a.connList, len(rows)).FillWidth().FillHeight().
				ItemKey(func(i int) any {
					if rows[i].ID != "" {
						return rows[i].ID
					}
					return i
				}).Rows(func(listRow ui.ListRow) {
				c := listRow.Context
				i := listRow.Index
				cn := rows[i]
				if i > 0 {
					hairline(c)
				}
				row := ui.Row(c).FillWidth().Padding(12).Gap(8).AlignItems(ui.Center)
				if row.Hovered() {
					row.Background(pal.Hover)
				}
				row.Children(func() {
					ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
						ui.Text(c, cn.Host).FillWidth().SingleLine().Ellipsis("…").Tooltip(cn.Host).FontSize(13).FontWeight(500).TextColor(pal.Text)
						subtitle := cn.Process
						if subtitle == "" {
							subtitle = strings.ToUpper(cn.Network)
						}
						ui.Text(c, subtitle+" · "+cn.Rule+" · "+cn.Chain).FillWidth().SingleLine().Ellipsis("…").Tooltip(cn.Chain).FontSize(11).TextColor(pal.Muted)
					})
					ui.Text(c, fmtBytes(cn.Up)).Width(64).FontSize(11).TextColor(pal.Up).TextAlign(ui.End)
					ui.Text(c, fmtBytes(cn.Down)).Width(64).FontSize(11).TextColor(pal.Down).TextAlign(ui.End)
					ui.Button(c, "详情").Label("查看连接 " + cn.Host).OnClick(func() { a.connSelected = cn; a.showConn = true })
				})
			})
		})
	}
	a.connectionDialogs(c)
}

func (a *App) disconnectConnection(id string) {
	if err := a.k.CloseConnection(id); err != nil {
		a.connError = "断开失败: " + err.Error()
		return
	}
	a.showConn = false
	a.refreshConnections()
}

func (a *App) connectionDialogs(c *ui.Context) {
	pal := paletteFor(c)
	ui.Modal(c, &a.showConn, func() {
		cn := a.connSelected
		ui.Column(c).Width(540).MaxWidth(540).Padding(20).Gap(12).Children(func() {
			ui.Text(c, "连接详情").FontSize(16).Bold().TextColor(pal.Text)
			ui.Scroll(c).FillWidth().Height(280).Children(func() {
				ui.Column(c).FillWidth().Gap(10).Children(func() {
					detail := func(label, value string) {
						if value == "" {
							value = "—"
						}
						ui.Text(c, label).FontSize(11).TextColor(pal.Faint)
						ui.Text(c, value).FillWidth().FontSize(12).TextColor(pal.Text).Selectable()
					}
					detail("主机", cn.Host)
					detail("进程", cn.Process)
					detail("进程路径", cn.ProcessPath)
					detail("源地址", connectionAddress(cn.SourceIP, cn.SourcePort))
					detail("目标地址", connectionAddress(cn.DestinationIP, cn.DestinationPort))
					detail("协议", strings.ToUpper(cn.Network))
					detail("命中规则", strings.TrimSpace(cn.Rule+" "+cn.RulePayload))
					detail("代理链路", cn.Chain)
					detail("开始时间", cn.Start.Local().Format("2006-01-02 15:04:05"))
					detail("流量", fmt.Sprintf("上行 %s · 下行 %s", fmtBytes(cn.Up), fmtBytes(cn.Down)))
				})
			})
			if a.connError != "" {
				settingsFeedback(c, a.connError, true)
			}
			ui.Row(c).FillWidth().Gap(8).Children(func() {
				ui.Button(c, "复制主机名").OnClick(func() { a.services().WriteClipboard(cn.Host) })
				ui.Spacer(c)
				ui.Button(c, "关闭").OnClick(func() { a.showConn = false })
				ui.PrimaryButton(c, "断开连接").Disabled(cn.ID == "").OnClick(func() { a.disconnectConnection(cn.ID) })
			})
		})
	})
	ui.Modal(c, &a.confirmCloseAll, func() {
		ui.Column(c).Width(440).Padding(20).Gap(16).Children(func() {
			ui.Text(c, "断开全部连接？").FontSize(16).Bold().TextColor(pal.Text)
			ui.Text(c, "当前会话会中断，应用通常会自动重新连接。").FillWidth().FontSize(12).TextColor(pal.Muted)
			if a.connError != "" {
				settingsFeedback(c, a.connError, true)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				ui.Button(c, "取消").OnClick(func() { a.confirmCloseAll = false })
				ui.PrimaryButton(c, "断开全部").OnClick(func() {
					if err := a.k.CloseAllConnections(); err != nil {
						a.connError = "断开失败: " + err.Error()
						return
					}
					a.confirmCloseAll = false
					a.refreshConnections()
				})
			})
		})
	})
}
func connectionAddress(ip, port string) string {
	if ip == "" {
		return ""
	}
	if port == "" {
		return ip
	}
	return net.JoinHostPort(ip, port)
}
