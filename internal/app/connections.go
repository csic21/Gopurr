package app

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/core"
)

type connectionState struct {
	connectionRuleState
	connList        ui.ListState
	connQuery       string
	connFetched     time.Time
	connRows        []core.Conn
	connError       string
	connSelected    core.Conn
	showConn        bool
	confirmCloseAll bool
	connRefreshing  bool
	connClosing     bool
	connSearch      []string
	connIndexedRows []core.Conn
	connFiltered    []core.Conn
	connFilterQuery string
	connFilterReady bool
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
	rows, err := a.k.CachedConnections(context.Background())
	a.acceptConnections(rows, err)
}

func (a *App) acceptConnections(rows []core.Conn, err error) {
	a.connFetched = time.Now()
	if err != nil {
		a.connError = tr("读取连接失败: ") + err.Error()
		return
	}
	a.connRows, a.connError = rows, ""
	a.connFilterReady = false
}

// UI callbacks only schedule work. The cached kernel snapshot is also shared
// with remote web readers, so opening both does not double the controller load.
func (a *App) requestConnections() {
	if a.connRefreshing || a.k == nil || a.ShuttingDown() {
		return
	}
	a.connRefreshing = true
	generation := a.networkGeneration.Load()
	go func() {
		rows, err := a.k.CachedConnections(context.Background())
		search := connectionSearchText(rows)
		runOnMain(func() {
			a.connRefreshing = false
			if a.ShuttingDown() {
				return
			}
			if generation == a.networkGeneration.Load() {
				a.acceptConnections(rows, err)
				if err == nil {
					a.connSearch, a.connIndexedRows = search, rows
				}
			} else {
				a.connFetched = time.Time{}
			}
			a.invalidate()
		})
	}()
}

func connectionSearchText(rows []core.Conn) []string {
	search := make([]string, len(rows))
	for i, cn := range rows {
		search[i] = strings.ToLower(strings.Join([]string{cn.Host, cn.Process, cn.ProcessPath, cn.SourceIP, cn.DestinationIP, cn.Rule, cn.RulePayload, cn.Chain, cn.Network}, " "))
	}
	return search
}

func (a *App) visibleConnections() []core.Conn {
	rows := a.connRows
	same := len(rows) == len(a.connIndexedRows) && (len(rows) == 0 || &rows[0] == &a.connIndexedRows[0])
	if !same {
		a.connSearch, a.connIndexedRows, a.connFilterReady = connectionSearchText(rows), rows, false
	}
	query := strings.ToLower(strings.TrimSpace(a.connQuery))
	if query == "" {
		return rows
	}
	if a.connFilterReady && a.connFilterQuery == query {
		return a.connFiltered
	}
	filtered := make([]core.Conn, 0, len(rows))
	for i, text := range a.connSearch {
		if strings.Contains(text, query) {
			filtered = append(filtered, rows[i])
		}
	}
	a.connFiltered, a.connFilterQuery, a.connFilterReady = filtered, query, true
	return filtered
}

func (a *App) connsPage(c *ui.Context) {
	pal := paletteFor(c)
	a.connList.KeepOffset = true
	if time.Since(a.connFetched) >= 2*time.Second {
		a.requestConnections()
	}
	rows := a.visibleConnections()
	a.pageHeader(c, tr("连接"), fmt.Sprintf(tr("%d 条活跃会话 · 显示 %d 条"), len(a.connRows), len(rows)), func() {
		softButton(c, tr("断开全部…")).Disabled(len(a.connRows) == 0).OnClick(func() { a.confirmCloseAll = true })
	})
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		searchRow(c, &a.connQuery, tr("搜索主机、进程、IP、规则或链路")).Grow(1).MinWidth(0).Label(tr("搜索连接"))
		softButton(c, tr("刷新")).Disabled(a.connRefreshing).OnClick(a.requestConnections)
	})
	if a.connError != "" {
		settingsFeedback(c, a.connError, true)
	}
	if len(rows) == 0 {
		if a.connRefreshing && len(a.connRows) == 0 {
			emptyListState(c, iconConns, tr("正在读取连接…"), tr("连接信息将在读取完成后显示。"), nil)
		} else if a.connQuery != "" {
			emptyListState(c, iconSearch, tr("没有匹配的连接"), tr("尝试主机名、进程名或 IP 地址。"), func() { a.connQuery = "" })
		} else {
			emptyListState(c, iconConns, tr("暂无连接"), tr("打开系统代理或 TUN 后，这里会实时出现会话。"), nil)
		}
	} else {
		width, _ := c.Size()
		detailSide := a.showConn && width >= 1000
		showRouteColumn := width >= 1000 && !detailSide
		ui.Row(c).FillWidth().Grow(1).MinHeight(0).Gap(12).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).FillHeight().Radius(12).Background(pal.Surface).Children(func() {
				ui.Row(c).FillWidth().Padding(12).Gap(8).Children(func() {
					ui.Text(c, tr("主机 / 进程")).Grow(1).MinWidth(0).FontSize(11).TextColor(pal.Faint)
					if showRouteColumn {
						ui.Text(c, tr("规则 / 出口")).Width(180).FontSize(11).TextColor(pal.Faint)
					}
					ui.Text(c, tr("上行")).Width(60).FontSize(11).TextColor(pal.Faint).TextAlign(ui.End)
					ui.Text(c, tr("下行")).Width(60).FontSize(11).TextColor(pal.Faint).TextAlign(ui.End)
					ui.Box(c).Width(52)
				})
				hairline(c)
				ui.List(c, &a.connList, len(rows)).FillWidth().Grow(1).MinHeight(0).
					ItemKey(func(i int) any {
						if rows[i].ID != "" {
							return rows[i].ID
						}
						return i
					}).Rows(func(listRow ui.ListRow) {
					c := listRow.Context
					i := listRow.Index
					cn := rows[i]
					row := ui.Row(c).FillWidth().Padding(12).Gap(8).AlignItems(ui.Center).
						BorderWidth(0, 0, 1, 0).BorderColor(pal.Grid).Transition(colorMotion())
					if a.showConn && a.connSelected.ID == cn.ID {
						row.Background(pal.Selected)
					} else if row.Hovered() {
						row.Background(pal.Hover)
					}
					row.Children(func() {
						ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
							ui.Text(c, cn.Host).FillWidth().SingleLine().Ellipsis("…").Tooltip(cn.Host).FontSize(13).FontWeight(500).TextColor(pal.Text)
							subtitle := cn.Process
							if subtitle == "" {
								subtitle = strings.ToUpper(cn.Network)
							}
							ui.Text(c, subtitle).FillWidth().SingleLine().Ellipsis("…").Tooltip(subtitle).FontSize(11).TextColor(pal.Muted)
							if !showRouteColumn {
								ui.Text(c, cn.Rule+" → "+cn.Chain).FillWidth().SingleLine().Ellipsis("…").Tooltip(cn.Rule + " · " + cn.Chain).FontSize(11).TextColor(pal.Faint)
							}
						})
						if showRouteColumn {
							ui.Column(c).Width(180).Gap(4).Children(func() {
								ui.Text(c, cn.Rule).FontSize(11).TextColor(pal.Muted)
								ui.Text(c, cn.Chain).FillWidth().SingleLine().Ellipsis("…").Tooltip(cn.Chain).FontSize(11).TextColor(pal.Faint)
							})
						}
						ui.Text(c, fmtBytes(cn.Up)).Width(60).FontSize(11).FontFeatures("tnum").TextColor(pal.Text).TextAlign(ui.End)
						ui.Text(c, fmtBytes(cn.Down)).Width(60).FontSize(11).FontFeatures("tnum").TextColor(pal.Text).TextAlign(ui.End)
						softButton(c, tr("详情")).Label(tr("查看连接 ") + cn.Host).OnClick(func() { a.connSelected = cn; a.showConn = true })
					})
				})
			})
			if detailSide {
				ui.Column(c).Width(300).FillHeight().Padding(16).Gap(12).Radius(12).Background(pal.Surface).
					Label(tr("连接详情侧栏")).Key("connection-inspector").Transition(ui.ElementTransition{
					Animation: &gentlePanel, Colors: true, Enter: &ui.Motion{X: 12, Opacity: 0},
				}).Children(func() { a.connectionDetails(c, true) })
			}
		})
	}
	a.connectionDialogs(c)
}

func (a *App) disconnectConnection(id string) {
	if err := a.k.CloseConnection(id); err != nil {
		a.connError = tr("断开失败: ") + err.Error()
		return
	}
	a.showConn = false
	a.requestConnections()
}

func (a *App) requestConnectionClose(id string, all bool) {
	if a.connClosing || a.k == nil || a.ShuttingDown() {
		return
	}
	a.connClosing = true
	go func() {
		var err error
		if all {
			err = a.k.CloseAllConnections()
		} else {
			err = a.k.CloseConnection(id)
		}
		runOnMain(func() {
			a.connClosing = false
			if a.ShuttingDown() {
				return
			}
			if err != nil {
				a.connError = tr("断开失败: ") + err.Error()
			} else {
				a.showConn, a.confirmCloseAll = false, false
				a.connError = ""
				a.requestConnections()
			}
			a.invalidate()
		})
	}()
}

func (a *App) connectionDialogs(c *ui.Context) {
	pal := paletteFor(c)
	width, _ := c.Size()
	// When there is room, the inspector keeps the list available for choosing
	// another connection. Small windows use a focused, scrollable modal.
	if width < 1000 || len(a.visibleConnections()) == 0 {
		softModal(c, "connection-detail", &a.showConn, func() {
			ui.Column(c).Width(540).MaxWidth(540).Padding(20).Gap(12).Children(func() {
				a.connectionDetails(c, false)
			})
		})
	}
	softModal(c, "close-connections", &a.confirmCloseAll, func() {
		ui.Column(c).Width(440).Padding(20).Gap(16).Children(func() {
			ui.Text(c, tr("断开全部连接？")).FontSize(16).Bold().TextColor(pal.Text)
			ui.Text(c, tr("当前会话会中断，应用通常会自动重新连接。")).FillWidth().FontSize(12).TextColor(pal.Muted)
			if a.connError != "" {
				settingsFeedback(c, a.connError, true)
			}
			ui.Row(c).FillWidth().Gap(8).Justify(ui.End).Children(func() {
				softButton(c, tr("取消")).OnClick(func() { a.confirmCloseAll = false })
				softPrimaryButton(c, tr("断开全部")).Disabled(a.connClosing).OnClick(func() { a.requestConnectionClose("", true) })
			})
		})
	})
	a.connectionRuleDialog(c)
}

func (a *App) connectionDetails(c *ui.Context, side bool) {
	pal := paletteFor(c)
	cn := a.connSelected
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr("连接详情")).Grow(1).FontSize(16).FontWeight(600).TextColor(pal.Text)
		softButton(c, tr("关闭")).OnClick(func() { a.showConn = false })
	})
	scroll := ui.Scroll(c).FillWidth()
	if side {
		scroll.Grow(1).MinHeight(0)
	} else {
		scroll.Height(280)
	}
	scroll.Children(func() {
		ui.Column(c).FillWidth().Gap(16).Children(func() {
			detail := func(label, value string) {
				if value == "" {
					value = "—"
				}
				ui.Column(c).FillWidth().Gap(4).Children(func() {
					ui.Text(c, label).FontSize(11).TextColor(pal.Faint)
					ui.Text(c, value).FillWidth().FontSize(12).TextColor(pal.Text).Selectable()
				})
			}
			detail(tr("主机"), cn.Host)
			detail(tr("进程"), cn.Process)
			detail(tr("命中规则"), strings.TrimSpace(cn.Rule+" "+cn.RulePayload))
			detail(tr("代理链路"), cn.Chain)
			detail(tr("流量"), fmt.Sprintf(tr("上行 %s · 下行 %s"), fmtBytes(cn.Up), fmtBytes(cn.Down)))
			detail(tr("源地址"), connectionAddress(cn.SourceIP, cn.SourcePort))
			detail(tr("目标地址"), connectionAddress(cn.DestinationIP, cn.DestinationPort))
			detail(tr("协议"), strings.ToUpper(cn.Network))
			detail(tr("开始时间"), cn.Start.Local().Format("2006-01-02 15:04:05"))
			detail(tr("进程路径"), cn.ProcessPath)
		})
	})
	if a.connError != "" {
		settingsFeedback(c, a.connError, true)
	}
	hairline(c)
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		softButton(c, tr("复制主机名")).OnClick(func() { a.services().WriteClipboard(cn.Host) })
		softButton(c, tr("创建规则…")).Disabled(len(connectionRuleChoices(cn)) == 0).OnClick(func() { a.openConnectionRule(cn) })
		ui.Spacer(c)
		softButton(c, tr("断开连接")).TextColor(pal.Danger).Disabled(cn.ID == "" || a.connClosing).OnClick(func() { a.requestConnectionClose(cn.ID, false) })
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
