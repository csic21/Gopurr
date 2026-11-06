package app

import (
	"crypto/rand"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"go.yaml.in/yaml/v3"
	"gopurr/internal/patch"
)

func nodeFormFromBase(n patch.ProxyNode) *proxyNodeForm {
	f := &proxyNodeForm{Source: n.Name, Name: n.Name, Server: n.Server, Port: proxyPortText(n.Port)}
	f.setOptions(n.Options)
	return f
}

func (f *proxyNodeForm) changeProtocol(previous string) {
	if previous == f.Type {
		return
	}
	f.Cipher = ""
	if f.Type == "ss" {
		f.Cipher = "aes-128-gcm"
	}
	if f.Type == "vmess" {
		f.Cipher = "auto"
	}
	if f.Type != "http" && f.Type != "socks5" {
		f.Username = ""
	}
	if f.Type == "vmess" || f.Type == "vless" {
		f.Password = ""
	} else {
		f.UUID = ""
	}
}

func (f *proxyNodeForm) setOptions(options map[string]any) {
	f.originalOptions = patch.MergeProxyOptions(options, nil)
	options = patch.MergeProxyOptions(options, nil)
	for key, field := range map[string]*string{"type": &f.Type, "username": &f.Username, "password": &f.Password, "uuid": &f.UUID, "cipher": &f.Cipher} {
		*field = ""
		if value := options[key]; value != nil {
			*field = fmt.Sprint(value)
		}
		delete(options, key)
	}
	f.Advanced = ""
	if len(options) > 0 {
		raw, _ := yaml.Marshal(options)
		f.Advanced = string(raw)
	}
}

func (f *proxyNodeForm) options() (map[string]any, error) {
	options := map[string]any{}
	if strings.TrimSpace(f.Advanced) != "" {
		decoder := yaml.NewDecoder(strings.NewReader(f.Advanced))
		if err := decoder.Decode(&options); err != nil {
			return nil, fmt.Errorf(tr("高级参数须为 YAML 键值映射：%w"), err)
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("%s", tr("高级参数只接受一个 YAML 文档"))
		}
	}
	if options == nil {
		options = map[string]any{}
	}
	for _, key := range []string{"name", "type", "server", "port", "username", "password", "uuid", "cipher"} {
		if _, ok := options[key]; ok {
			return nil, fmt.Errorf(tr("请在表单中填写 %s，不要放入高级参数"), key)
		}
	}
	for key, value := range map[string]string{"type": f.Type, "username": f.Username, "password": f.Password, "uuid": f.UUID, "cipher": f.Cipher} {
		if original, exists := f.originalOptions[key]; exists && fmt.Sprint(original) == value {
			options[key] = original
		} else if value != "" {
			options[key] = value
		}
	}
	if !f.Missing {
		if slices.Contains([]string{"ss", "trojan", "hysteria2"}, f.Type) && f.Password == "" {
			return nil, fmt.Errorf("%s", tr("请填写节点密码"))
		}
		if slices.Contains([]string{"vmess", "vless"}, f.Type) && strings.TrimSpace(f.UUID) == "" {
			return nil, fmt.Errorf("%s", tr("请填写节点 UUID"))
		}
		if f.Type == "ss" && strings.TrimSpace(f.Cipher) == "" {
			return nil, fmt.Errorf("%s", tr("请填写加密方式"))
		}
	}
	return options, nil
}

func (a *App) addProxyEditorNode() {
	id := rand.Text()
	key := "local:" + id
	name := ""
	for n := 1; ; n++ {
		name = fmt.Sprintf(tr("本地节点 %d"), n)
		if !slices.Contains(a.proxyMemberOptions(nil), name) {
			break
		}
	}
	a.proxyEditorNodes[key] = &proxyNodeForm{LocalID: id, Name: name, Type: "ss", Port: "443", Cipher: "aes-128-gcm"}
	a.proxyEditorNodeOrder = append(a.proxyEditorNodeOrder, key)
	a.proxyEditorTab, a.proxyEditorNode, a.proxyEditorAdvanced = 0, key, false
	a.proxyEditorError = ""
	var target *proxyGroupForm
	for _, key := range a.proxyEditorGroupOrder {
		g := a.proxyEditorGroups[key]
		if g.Deleted || g.Missing {
			continue
		}
		if target == nil {
			target = g
		}
		if g.Name == a.proxyGroup || g.Source == a.proxyGroup {
			target = g
			break
		}
	}
	if target == nil {
		a.addProxyEditorGroup()
		target = a.proxyEditorGroups[a.proxyEditorGroup]
		a.proxyEditorTab, a.proxyEditorNode = 0, key
	}
	a.touchProxyMembers(target)
	target.Members = append(target.Members, name)
}

func (a *App) removeProxyMemberDraft(name string) {
	for _, group := range a.proxyEditorGroups {
		if !slices.Contains(group.Members, name) {
			continue
		}
		a.touchProxyMembers(group)
		group.Members = slices.DeleteFunc(group.Members, func(member string) bool { return member == name })
	}
}

func (a *App) openNewProxyNode() {
	a.openProxyEditor()
	if a.showProxyEditor {
		a.addProxyEditorNode()
	}
}

func (a *App) openProxyMemberEditor(name string) {
	a.openProxyEditor()
	if !a.showProxyEditor {
		return
	}
	for key, node := range a.proxyEditorNodes {
		if node.Name == name {
			a.proxyEditorTab, a.proxyEditorNode = 0, key
			return
		}
	}
	for key, group := range a.proxyEditorGroups {
		if group.Name == name && !group.Deleted {
			a.proxyEditorTab, a.proxyEditorGroup = 1, key
			return
		}
	}
	a.showProxyEditor = false
	a.notice = tr("此节点由代理集合提供，暂不支持单独覆写。可新增本地节点并加入策略组。")
}

func (a *App) proxyNodeCredentials(c *ui.Context, f *proxyNodeForm) {
	pal := paletteFor(c)
	field := func(label string, value *string, secret bool) {
		proxyEditorField(c, label, func() {
			e := ui.TextInput(c, value).FillWidth().Label(label)
			if secret {
				e.Password()
			}
		})
	}
	ui.Row(c).FillWidth().Gap(12).Children(func() {
		if f.Type == "ss" || f.Type == "vmess" {
			ui.Column(c).Width(180).Children(func() { field(tr("加密方式"), &f.Cipher, false) })
		}
		if f.Type == "http" || f.Type == "socks5" {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() { field(tr("用户名（可选）"), &f.Username, false) })
		}
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			switch f.Type {
			case "vmess", "vless":
				field("UUID", &f.UUID, true)
			case "ss", "trojan", "hysteria2":
				field(tr("节点密码"), &f.Password, true)
			case "http", "socks5":
				field(tr("密码（可选）"), &f.Password, true)
			default:
				// Uncommon protocols can still edit their original authentication.
				if f.Password != "" {
					field(tr("节点密码"), &f.Password, true)
				}
				if f.UUID != "" {
					field("UUID", &f.UUID, true)
				}
			}
		})
	})
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		softButton(c, tr("高级参数…")).OnClick(func() { a.proxyEditorAdvanced = true })
		ui.Text(c, tr("TLS、传输与其他协议选项")).Grow(1).MinWidth(0).FontSize(11).TextColor(pal.Faint)
	})
	if f.Source == "" {
		var groups []string
		for _, key := range a.proxyEditorGroupOrder {
			g := a.proxyEditorGroups[key]
			if !g.Deleted && slices.Contains(g.Members, f.Name) {
				groups = append(groups, g.Name)
			}
		}
		ui.Text(c, fmt.Sprintf(tr("本地节点 · 所属策略组：%s。可在“策略组”中调整成员。"), strings.Join(groups, "、"))).FillWidth().FontSize(11).TextColor(pal.Muted)
	}
}
