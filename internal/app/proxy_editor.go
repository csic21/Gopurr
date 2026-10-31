package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/patch"
)

type proxyEditorState struct {
	showProxyEditor                                 bool
	proxyEditorID, proxyEditorTitle, proxyEditorRaw string
	proxyEditorTab                                  int
	proxyEditorNode, proxyEditorGroup               string
	proxyEditorNodes                                map[string]*proxyNodeForm
	proxyEditorGroups                               map[string]*proxyGroupForm
	proxyEditorNodeOrder, proxyEditorGroupOrder     []string
	proxyEditorBaseNodes                            map[string]patch.ProxyNode
	proxyEditorBaseGroups                           map[string]patch.ProxyGroup
	proxyEditorError                                string
	proxyEditorNext                                 int
}

type proxyNodeForm struct {
	Source, Name, Server, Port string
	Missing                    bool
}
type proxyGroupForm struct {
	Source, Name, Type               string
	Members                          []string
	MemberPick                       string
	MembersTouched, Deleted, Missing bool
}

func (a *App) openProxyEditor() {
	if a.store == nil || a.store.ActiveID() == "" {
		a.notice = tr("请先选择订阅")
		return
	}
	a.openProfileProxyEditor(a.store.ActiveID())
}

func (a *App) openProfileProxyEditor(id string) {
	if a.store == nil {
		return
	}
	p, err := a.store.Get(id)
	if err != nil {
		a.notice = err.Error()
		return
	}
	local := a.profileOverrides(id)
	edits := patch.CloneProxyEdits(local.ProxyEdits)
	raw, err := a.proxyEditorBaseConfig(id)
	if err != nil {
		a.notice = err.Error()
		return
	}
	nodes, groups, err := patch.ReadProxyEditor(raw)
	if err != nil {
		a.notice = err.Error()
		return
	}
	a.proxyEditorState = proxyEditorState{showProxyEditor: true, proxyEditorID: id, proxyEditorTitle: p.Name, proxyEditorRaw: string(raw), proxyEditorNodes: map[string]*proxyNodeForm{}, proxyEditorGroups: map[string]*proxyGroupForm{}, proxyEditorBaseNodes: map[string]patch.ProxyNode{}, proxyEditorBaseGroups: map[string]patch.ProxyGroup{}}
	for _, n := range nodes {
		a.proxyEditorBaseNodes[n.Name] = n
		a.proxyEditorNodeOrder = append(a.proxyEditorNodeOrder, n.Name)
		a.proxyEditorNodes[n.Name] = &proxyNodeForm{Source: n.Name, Name: n.Name, Server: n.Server, Port: proxyPortText(n.Port)}
	}
	for _, g := range groups {
		a.proxyEditorBaseGroups[g.Name] = g
		a.proxyEditorGroupOrder = append(a.proxyEditorGroupOrder, g.Name)
		a.proxyEditorGroups[g.Name] = &proxyGroupForm{Source: g.Name, Name: g.Name, Type: g.Type, Members: slices.Clone(g.Members), MemberPick: "DIRECT"}
	}
	if edits != nil {
		for _, e := range edits.Nodes {
			f := a.proxyEditorNodes[e.Source]
			if f == nil {
				f = &proxyNodeForm{Source: e.Source, Name: e.Source, Missing: true}
				a.proxyEditorNodes[e.Source] = f
				a.proxyEditorNodeOrder = append(a.proxyEditorNodeOrder, e.Source)
			}
			if e.Name != nil {
				f.Name = *e.Name
			}
			if e.Server != nil {
				f.Server = *e.Server
			}
			if e.Port != nil {
				f.Port = strconv.Itoa(*e.Port)
			}
		}
		for _, e := range edits.Groups {
			key := e.Source
			if key == "" {
				a.proxyEditorNext++
				key = fmt.Sprintf("new:%d", a.proxyEditorNext)
			}
			f := a.proxyEditorGroups[key]
			if f == nil {
				f = &proxyGroupForm{Source: e.Source, Name: e.Source, Type: "select", MemberPick: "DIRECT", Missing: e.Source != ""}
				a.proxyEditorGroups[key] = f
				a.proxyEditorGroupOrder = append(a.proxyEditorGroupOrder, key)
			}
			if e.Name != nil {
				f.Name = *e.Name
			}
			if e.Type != nil {
				f.Type = *e.Type
			}
			if e.Members != nil {
				f.Members = slices.Clone(*e.Members)
				f.MembersTouched = true
			}
			f.Deleted = e.Delete
		}
	}
	if len(a.proxyEditorNodeOrder) > 0 {
		a.proxyEditorNode = a.proxyEditorNodeOrder[0]
	}
	if len(a.proxyEditorGroupOrder) > 0 {
		a.proxyEditorGroup = a.proxyEditorGroupOrder[0]
	}
}

func proxyPortText(port int) string {
	if port == 0 {
		return ""
	}
	return strconv.Itoa(port)
}

func (a *App) proxyEditorEdits() (*patch.ProxyEdits, error) {
	edits := &patch.ProxyEdits{}
	for _, key := range a.proxyEditorNodeOrder {
		f := a.proxyEditorNodes[key]
		if f == nil {
			continue
		}
		base := a.proxyEditorBaseNodes[f.Source]
		e := patch.NodeEdit{Source: f.Source}
		if f.Name != base.Name || f.Missing {
			v := strings.TrimSpace(f.Name)
			e.Name = &v
		}
		if f.Server != base.Server {
			v := strings.TrimSpace(f.Server)
			e.Server = &v
		}
		if f.Port != proxyPortText(base.Port) {
			v, err := strconv.Atoi(strings.TrimSpace(f.Port))
			if err != nil || v < 1 || v > 65535 {
				return nil, fmt.Errorf("%s", tr("节点端口须为 1–65535"))
			}
			e.Port = &v
		}
		if f.Missing || e.Name != nil || e.Server != nil || e.Port != nil {
			edits.Nodes = append(edits.Nodes, e)
		}
	}
	for _, key := range a.proxyEditorGroupOrder {
		f := a.proxyEditorGroups[key]
		if f == nil {
			continue
		}
		base := a.proxyEditorBaseGroups[f.Source]
		if f.Source == "" && f.Deleted {
			continue
		}
		e := patch.GroupEdit{Source: f.Source, Delete: f.Deleted}
		if !f.Deleted {
			if f.Name != base.Name || f.Source == "" || f.Missing {
				v := strings.TrimSpace(f.Name)
				e.Name = &v
			}
			if f.Type != base.Type || f.Source == "" {
				v := f.Type
				e.Type = &v
			}
			if f.MembersTouched || f.Source == "" {
				members := slices.Clone(f.Members)
				e.Members = &members
			}
		}
		if f.Missing || e.Delete || e.Name != nil || e.Type != nil || e.Members != nil {
			edits.Groups = append(edits.Groups, e)
		}
	}
	if len(edits.Nodes)+len(edits.Groups) == 0 {
		return nil, nil
	}
	previous := a.profileOverrides(a.proxyEditorID).ProxyEdits
	if previous != nil {
		for i := range edits.Nodes {
			for _, old := range previous.Nodes {
				if old.Source == edits.Nodes[i].Source {
					edits.Nodes[i].Aliases = proxyEditorAliases(old.Aliases, old.Source, old.Name, edits.Nodes[i].Name)
				}
			}
		}
		for i := range edits.Groups {
			for _, old := range previous.Groups {
				if old.Source != "" && old.Source == edits.Groups[i].Source {
					edits.Groups[i].Aliases = proxyEditorAliases(old.Aliases, old.Source, old.Name, edits.Groups[i].Name)
				}
			}
		}
	}
	return edits, nil
}

func proxyEditorAliases(old []string, source string, previous, next *string) []string {
	aliases := slices.Clone(old)
	final := source
	if next != nil {
		final = *next
	}
	if previous != nil && *previous != source && *previous != final && !slices.Contains(aliases, *previous) {
		aliases = append(aliases, *previous)
	}
	return aliases
}

func (a *App) saveProxyEditor() {
	edits, err := a.proxyEditorEdits()
	if err != nil {
		a.proxyEditorError = err.Error()
		return
	}
	local := a.profileOverrides(a.proxyEditorID)
	local.ProxyEdits = edits
	if _, err := a.commitProfileOverrides(a.proxyEditorID, local); err != nil {
		a.proxyEditorError = err.Error()
		return
	}
	a.showProxyEditor = false
	a.notice = tr("节点与策略组修改已保存")
}

func (a *App) proxyEditorTarget(name string) string {
	for _, f := range a.proxyEditorNodes {
		if f.Source == name {
			return f.Name
		}
	}
	for _, f := range a.proxyEditorGroups {
		if f.Source == name && !f.Deleted {
			return f.Name
		}
	}
	return name
}

func (a *App) touchProxyMembers(f *proxyGroupForm) {
	if !f.MembersTouched {
		for i, name := range f.Members {
			f.Members[i] = a.proxyEditorTarget(name)
		}
		f.MembersTouched = true
	}
}

func (a *App) renameProxyMemberDraft(old, next string) {
	for _, g := range a.proxyEditorGroups {
		if g.MembersTouched {
			for i, name := range g.Members {
				if name == old {
					g.Members[i] = next
				}
			}
		}
	}
}

func (a *App) proxyMemberOptions(current *proxyGroupForm) []string {
	out := []string{"DIRECT", "REJECT"}
	for _, key := range a.proxyEditorNodeOrder {
		f := a.proxyEditorNodes[key]
		if !f.Missing && f.Name != "" && !slices.Contains(out, f.Name) {
			out = append(out, f.Name)
		}
	}
	for _, key := range a.proxyEditorGroupOrder {
		f := a.proxyEditorGroups[key]
		if f != current && !f.Deleted && !f.Missing && f.Name != "" && !slices.Contains(out, f.Name) {
			out = append(out, f.Name)
		}
	}
	return out
}

func (a *App) addProxyEditorGroup() {
	a.proxyEditorNext++
	key := fmt.Sprintf("new:%d", a.proxyEditorNext)
	a.proxyEditorGroups[key] = &proxyGroupForm{Name: fmt.Sprintf(tr("本地策略组 %d"), a.proxyEditorNext), Type: "select", Members: []string{"DIRECT"}, MembersTouched: true, MemberPick: "DIRECT"}
	a.proxyEditorGroupOrder = append(a.proxyEditorGroupOrder, key)
	a.proxyEditorTab, a.proxyEditorGroup = 1, key
}

func (a *App) resetProxyEditorItem() {
	if a.proxyEditorTab == 0 {
		key := a.proxyEditorNode
		f := a.proxyEditorNodes[key]
		if f == nil {
			return
		}
		if f.Missing {
			delete(a.proxyEditorNodes, key)
			a.proxyEditorNodeOrder = slices.DeleteFunc(a.proxyEditorNodeOrder, func(s string) bool { return s == key })
			a.proxyEditorNode = ""
		} else {
			base := a.proxyEditorBaseNodes[key]
			a.renameProxyMemberDraft(f.Name, base.Name)
			*f = proxyNodeForm{Source: key, Name: base.Name, Server: base.Server, Port: proxyPortText(base.Port)}
		}
	} else {
		key := a.proxyEditorGroup
		f := a.proxyEditorGroups[key]
		if f == nil {
			return
		}
		if f.Missing || f.Source == "" {
			delete(a.proxyEditorGroups, key)
			a.proxyEditorGroupOrder = slices.DeleteFunc(a.proxyEditorGroupOrder, func(s string) bool { return s == key })
			a.proxyEditorGroup = ""
		} else {
			base := a.proxyEditorBaseGroups[key]
			a.renameProxyMemberDraft(f.Name, base.Name)
			*f = proxyGroupForm{Source: key, Name: base.Name, Type: base.Type, Members: slices.Clone(base.Members), MemberPick: "DIRECT"}
		}
	}
	a.proxyEditorError = ""
}

func (a *App) proxyEditorDialog(c *ui.Context) {
	if !a.showProxyEditor {
		return
	}
	pal := paletteFor(c)
	width, height := c.Size()
	ui.Overlay(c, func() {
		back := ui.Box(c.Key("proxy-editor")).Absolute().Left(0).Top(0).Right(0).Bottom(0).Center().Modal()
		modalBackdrop(back)
		if back.OverlayShortcut(0, ui.KeyEscape) {
			a.showProxyEditor = false
		}
		back.Children(func() {
			panel := ui.Column(c).Width(min(float32(780), width-64)).MaxHeight(height-64).Padding(20).Gap(12).Radius(14).Background(pal.Surface).Border(1, pal.Border).Role(ui.RoleDialog).Label(tr("节点与策略组"))
			modalPanel(c, panel)
			panel.Children(func() {
				ui.Text(c, tr("节点与策略组")).FontSize(18).FontWeight(600).TextColor(pal.Text)
				ui.Text(c, a.proxyEditorTitle).FillWidth().SingleLine().Ellipsis("…").Tooltip(a.proxyEditorTitle).FontSize(12).TextColor(pal.Muted)
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					softSegmented(c, &a.proxyEditorTab, tr("节点"), tr("策略组")).Label(tr("编辑内容"))
					ui.Spacer(c)
					softButton(c, tr("添加策略组")).OnClick(a.addProxyEditorGroup)
				})
				ui.Scroll(c).FillWidth().Shrink(1).MinHeight(0).Children(func() {
					ui.Column(c).FillWidth().Gap(12).Children(func() {
						ui.Text(c, tr("修改仅保存在当前订阅的本地覆写中，订阅更新后继续生效。未修改字段沿用最新订阅。")).FillWidth().FontSize(11).TextColor(pal.Muted)
						if a.proxyEditorTab == 0 {
							a.proxyEditorNodeForm(c)
						} else {
							a.proxyEditorGroupForm(c)
						}
						if a.proxyEditorError != "" {
							extensionError(c, a.proxyEditorError)
						}
					})
				})
				ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
					softButton(c, tr("恢复所选项")).OnClick(a.resetProxyEditorItem)
					ui.Spacer(c)
					softButton(c, tr("取消")).OnClick(func() { a.showProxyEditor = false })
					softPrimaryButton(c, tr("保存并应用")).OnClick(a.saveProxyEditor)
				})
			})
		})
	})
}

func proxyEditorField(c *ui.Context, label string, body func()) {
	ui.Column(c).FillWidth().Gap(5).Children(func() { ui.Text(c, label).FontSize(11.5).TextColor(paletteFor(c).Muted); body() })
}

func (a *App) proxyEditorNodeForm(c *ui.Context) {
	pal := paletteFor(c)
	if len(a.proxyEditorNodeOrder) == 0 {
		ui.Text(c, tr("当前配置没有内联节点，可编辑策略组；代理集合中的节点由订阅提供方管理。")).FillWidth().FontSize(12).TextColor(pal.Muted)
		return
	}
	keys := a.proxyEditorNodeOrder
	labels := make([]string, len(keys))
	selected := ""
	for i, key := range keys {
		f := a.proxyEditorNodes[key]
		labels[i] = fmt.Sprintf("%d. %s", i+1, f.Name)
		if key == a.proxyEditorNode {
			selected = labels[i]
		}
	}
	if selected == "" {
		a.proxyEditorNode = keys[0]
		selected = labels[0]
	}
	proxyEditorField(c, tr("选择节点"), func() {
		ui.Select(c, &selected, labels).FillWidth().Label(tr("选择节点")).OnChange(func() {
			for i, label := range labels {
				if selected == label {
					a.proxyEditorNode = keys[i]
				}
			}
		})
	})
	f := a.proxyEditorNodes[a.proxyEditorNode]
	base := a.proxyEditorBaseNodes[f.Source]
	if f.Missing {
		ui.Text(c, tr("订阅已移除此项；请点击恢复所选项以移除失效的本地修改。")).FillWidth().FontSize(11.5).TextColor(pal.Warn)
	}
	ui.Text(c, base.Type).FontSize(11).TextColor(pal.Faint)
	proxyEditorField(c, tr("节点名称"), func() {
		old := f.Name
		if ui.TextInput(c, &f.Name).FillWidth().Label(tr("节点名称")).Changed() {
			a.renameProxyMemberDraft(old, f.Name)
		}
	})
	ui.Row(c).FillWidth().Gap(12).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			proxyEditorField(c, tr("服务器地址"), func() {
				ui.TextInput(c, &f.Server).FillWidth().Label(tr("服务器地址")).Disabled(base.Server == "" && !f.Missing)
			})
		})
		ui.Column(c).Width(120).Children(func() {
			proxyEditorField(c, tr("节点端口"), func() {
				ui.TextInput(c, &f.Port).FillWidth().Label(tr("节点端口")).Disabled(base.Port == 0 && !f.Missing)
			})
		})
	})
	ui.Text(c, tr("协议、密码与其他高级参数保持订阅原值；不提供地址或端口的节点只支持改名。")).FillWidth().FontSize(11).TextColor(pal.Faint)
}

func (a *App) proxyEditorGroupForm(c *ui.Context) {
	pal := paletteFor(c)
	if len(a.proxyEditorGroupOrder) == 0 {
		ui.Text(c, tr("暂无策略组，点击添加策略组开始。")).FillWidth().FontSize(12).TextColor(pal.Muted)
		return
	}
	keys := a.proxyEditorGroupOrder
	labels := make([]string, len(keys))
	selected := ""
	for i, key := range keys {
		f := a.proxyEditorGroups[key]
		labels[i] = fmt.Sprintf("%d. %s", i+1, f.Name)
		if f.Deleted {
			labels[i] += " · " + tr("待删除")
		}
		if key == a.proxyEditorGroup {
			selected = labels[i]
		}
	}
	if selected == "" {
		a.proxyEditorGroup = keys[0]
		selected = labels[0]
	}
	proxyEditorField(c, tr("选择策略组"), func() {
		ui.Select(c, &selected, labels).FillWidth().Label(tr("选择策略组")).OnChange(func() {
			for i, label := range labels {
				if selected == label {
					a.proxyEditorGroup = keys[i]
				}
			}
		})
	})
	f := a.proxyEditorGroups[a.proxyEditorGroup]
	if f.Missing {
		ui.Text(c, tr("订阅已移除此项；请点击恢复所选项以移除失效的本地修改。")).FillWidth().FontSize(11.5).TextColor(pal.Warn)
	}
	if f.Deleted {
		ui.Text(c, tr("此策略组将在保存时删除。仍被其他策略组或规则使用时，保存会提示修正。")).FillWidth().FontSize(12).TextColor(pal.Warn)
		return
	}
	ui.Row(c).FillWidth().Gap(12).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			proxyEditorField(c, tr("策略组名称"), func() {
				old := f.Name
				if ui.TextInput(c, &f.Name).FillWidth().Label(tr("策略组名称")).Changed() {
					a.renameProxyMemberDraft(old, f.Name)
				}
			})
		})
		ui.Column(c).Width(180).Children(func() {
			proxyEditorField(c, tr("策略组类型"), func() {
				types := []string{"select", "url-test", "fallback", "load-balance"}
				labels := []string{tr("手动选择"), tr("自动选择"), tr("故障转移"), tr("负载均衡")}
				if !slices.Contains(types, f.Type) {
					types = append(types, f.Type)
					labels = append(labels, f.Type)
				}
				localizedSelect(c, &f.Type, types, labels).FillWidth().Label(tr("策略组类型"))
			})
		})
	})
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr("有序成员")).FontSize(12).FontWeight(550).TextColor(pal.Text)
		ui.Spacer(c)
		softButton(c, tr("删除策略组")).OnClick(func() { f.Deleted = true })
	})
	ui.Row(c).FillWidth().Gap(8).Children(func() {
		ui.Select(c, &f.MemberPick, a.proxyMemberOptions(f)).Grow(1).MinWidth(0).Label(tr("选择添加成员"))
		already := false
		for _, name := range f.Members {
			if !f.MembersTouched {
				name = a.proxyEditorTarget(name)
			}
			if name == f.MemberPick {
				already = true
			}
		}
		softButton(c, tr("添加成员")).Disabled(f.MemberPick == "" || already).OnClick(func() {
			a.touchProxyMembers(f)
			if !slices.Contains(f.Members, f.MemberPick) {
				f.Members = append(f.Members, f.MemberPick)
			}
		})
	})
	for i, name := range f.Members {
		member := name
		if !f.MembersTouched {
			member = a.proxyEditorTarget(name)
		}
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("%d", i+1)).Width(24).FontSize(11).TextColor(pal.Faint)
			ui.Text(c, member).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").Tooltip(member).FontSize(12).TextColor(pal.Text)
			softButton(c, tr("上移")).Label(fmt.Sprintf(tr("上移第 %d 个成员"), i+1)).Disabled(i == 0).OnClick(func() { a.touchProxyMembers(f); f.Members[i-1], f.Members[i] = f.Members[i], f.Members[i-1] })
			softButton(c, tr("下移")).Label(fmt.Sprintf(tr("下移第 %d 个成员"), i+1)).Disabled(i == len(f.Members)-1).OnClick(func() { a.touchProxyMembers(f); f.Members[i], f.Members[i+1] = f.Members[i+1], f.Members[i] })
			softButton(c, tr("移除")).Label(fmt.Sprintf(tr("移除第 %d 个成员"), i+1)).OnClick(func() { a.touchProxyMembers(f); f.Members = slices.Delete(f.Members, i, i+1) })
		})
	}
	if len(f.Members) == 0 {
		ui.Text(c, tr("尚未添加成员；使用代理集合的原策略组仍保留其集合配置。")).FillWidth().FontSize(11).TextColor(pal.Muted)
	}
}
