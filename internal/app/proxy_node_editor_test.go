package app

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/core"
	"gopurr/internal/patch"
)

func TestProxyEditorLocalNodeLifecycle(t *testing.T) {
	a, id, path := proxyEditorTestApp(t)
	a.proxyGroup = "Choice"
	a.addProxyEditorNode()
	key := a.proxyEditorNode
	f := a.proxyEditorNodes[key]
	old := f.Name
	f.Name, f.Server, f.Password = "My server", "local.example.com", "local-secret"
	a.renameProxyMemberDraft(old, f.Name)
	f.Advanced = "udp: true\n"
	if !slices.Contains(a.proxyEditorGroups["Choice"].Members, f.Name) {
		t.Fatal("new node not joined to current group")
	}
	a.saveProxyEditor()
	if a.proxyEditorError != "" || a.showProxyEditor {
		t.Fatalf("save: %s", a.proxyEditorError)
	}
	updated := strings.Replace(proxyEditorSource, "initial-secret", "refreshed-secret", 1)
	updated = strings.Replace(updated, "[One, Two, DIRECT]", "[One, Two, REJECT, DIRECT]", 1)
	if err := os.WriteFile(path, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(id); err != nil {
		t.Fatal(err)
	}
	a.openProfileProxyEditor(id)
	if !slices.Contains(a.proxyEditorGroups["Choice"].Members, "REJECT") {
		t.Fatal("adding a node froze the group's inherited members")
	}
	f = a.proxyEditorNodes[key]
	if f == nil || f.Missing || f.Password != "local-secret" {
		t.Fatal("local node did not survive reopening and refresh")
	}
	a.proxyEditorNode = key
	f.Name = "Renamed local"
	a.renameProxyMemberDraft("My server", f.Name)
	local := a.profileOverrides(id)
	local.ExtraRules = []string{"DOMAIN,local.example.com,My server"}
	if err := a.store.SetOverrides(id, local); err != nil {
		t.Fatal(err)
	}
	a.saveProxyEditor()
	if a.proxyEditorError != "" {
		t.Fatal(a.proxyEditorError)
	}
	raw, err := a.configForProfile(id, a.profileOverrides(id))
	if err != nil || !strings.Contains(string(raw), "refreshed-secret") || !strings.Contains(string(raw), "DOMAIN,local.example.com,Renamed local") {
		t.Fatalf("refresh / rename failed: %v", err)
	}
	a.openProfileProxyEditor(id)
	a.proxyEditorNode = key
	a.resetProxyEditorItem()
	if slices.Contains(a.proxyEditorGroups["Choice"].Members, "Renamed local") {
		t.Fatal("removal left a dangling member")
	}
	a.saveProxyEditor()
	if a.proxyEditorError == "" {
		t.Fatal("removing a node referenced by a rule should require fixing the rule")
	}
	local = a.profileOverrides(id)
	local.ExtraRules = nil
	if err := a.store.SetOverrides(id, local); err != nil {
		t.Fatal(err)
	}
	a.saveProxyEditor()
	if a.proxyEditorError != "" && a.showProxyEditor {
		t.Fatal(a.proxyEditorError)
	}
}

func TestProxyEditorCredentialsAndNestedOptionsAreSurgical(t *testing.T) {
	a, id, path := proxyEditorTestApp(t)
	source := strings.Replace(proxyEditorSource, "password: initial-secret", "password: initial-secret, plugin: v2ray-plugin, plugin-opts: {mode: websocket, host: original.example.com, path: /original}", 1)
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(id); err != nil {
		t.Fatal(err)
	}
	a.openProfileProxyEditor(id)
	f := a.proxyEditorNodes["One"]
	f.Password = "edited-secret"
	f.Advanced = strings.Replace(f.Advanced, "/original", "/local", 1)
	a.saveProxyEditor()
	if a.proxyEditorError != "" {
		t.Fatal(a.proxyEditorError)
	}
	local := a.profileOverrides(id)
	e := local.ProxyEdits.Nodes[0]
	if len(e.Options) != 2 || e.Options["cipher"] != nil || e.Options["type"] != nil {
		t.Fatal("untouched protocol fields persisted")
	}
	source = strings.Replace(source, "original.example.com", "refreshed.example.com", 1)
	if err := os.WriteFile(path, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Refresh(id); err != nil {
		t.Fatal(err)
	}
	raw, err := a.configForProfile(id, local)
	if err != nil {
		t.Fatal(err)
	}
	nodes, _, err := patch.ReadProxyEditor(raw)
	if err != nil {
		t.Fatal(err)
	}
	opts := nodes[0].Options
	plugin := opts["plugin-opts"].(map[string]any)
	if opts["password"] != "edited-secret" || plugin["path"] != "/local" || plugin["host"] != "refreshed.example.com" {
		t.Fatal("nested edits replaced untouched subscription parameters")
	}
}

func TestProxyEditorAddAndRowEditControls(t *testing.T) {
	a, id, _ := proxyEditorTestApp(t)
	a.store.Active = id
	a.showProxyEditor = false
	g := core.Group{Name: "Choice", Type: "Selector", Members: []string{"One", "Two"}, Now: "One"}
	tr := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Children(func() { a.nodeRow(c, g, "Two") })
		a.proxyEditorDialog(c)
	}, 840, 560)
	tr.SetPreferences(ui.Preferences{ReduceMotion: true})
	tr.Frame()
	if err := tr.Click("编辑 Two"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if !a.showProxyEditor || a.proxyEditorNode != "Two" || a.proxySelecting {
		t.Fatal("row edit did not target the node independently of selection")
	}
	if err := tr.Click("新增节点"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	for _, label := range []string{"节点名称", "节点协议", "服务器地址", "节点端口", "节点密码", "移除本地节点", "取消", "保存并应用"} {
		r, ok := tr.Find(label)
		if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > 840 || r.Y+r.H > 560 {
			t.Fatalf("clipped or missing %s: %+v", label, r)
		}
	}
	a.saveProxyEditor()
	if !a.showProxyEditor || a.proxyEditorError == "" {
		t.Fatal("incomplete node was saved")
	}
	a.requestCloseProxyEditor()
	if !a.proxyEditorDiscard {
		t.Fatal("new-node draft was discarded without protection")
	}
}

func TestProxyEditorRejectsMalformedAdvancedOptions(t *testing.T) {
	for _, raw := range []string{"[one, two]", "tls: true\n---\nport: 443", "tls: true\n---\n[", "name: hidden", "password: hidden"} {
		f := proxyNodeForm{Type: "socks5", Advanced: raw}
		if _, err := f.options(); err == nil {
			t.Fatalf("accepted invalid advanced options %q", raw)
		}
	}
}
