package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/profile"
)

func aggregationTestSource(t *testing.T, a *App, name, server string) profile.Profile {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.yaml")
	body := "proxies:\n  - name: 香港节点\n    type: ss\n    server: " + server + "\n    port: 443\n    cipher: aes-128-gcm\n    password: secret\nrules:\n  - MATCH,DIRECT\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := a.store.AddFile(path, name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAggregationEditorSavesIndependentConfigAndRetainsFailedDraft(t *testing.T) {
	a := subscriptionTestApp(t)
	a.booted, a.ticking, a.page = true, true, "subs"
	source := aggregationTestSource(t, a, "来源", "one.example")
	a.openAggregationEditor("")
	a.aggregationName = "工作聚合"
	for i := range a.aggregationSources {
		a.aggregationSources[i].Selected = true
		a.aggregationSources[i].Prefix = "工作 | "
	}
	a.previewAggregation()
	if a.aggregationError != "" || a.aggregationStats.Output != 1 || !strings.Contains(a.aggregationPreview, "工作 | 香港节点") {
		t.Fatalf("preview failed: %s", a.aggregationError)
	}
	a.saveAggregation()
	if a.aggregationError != "" || a.showAggregation {
		t.Fatalf("save failed: %s", a.aggregationError)
	}
	var aggregate profile.Profile
	for _, p := range a.store.List() {
		if p.IsAggregate() {
			aggregate = p
		}
	}
	if aggregate.ID == "" || aggregate.Name != "工作聚合" || a.store.ActiveID() != source.ID {
		t.Fatal("aggregate missing or source activation changed")
	}
	original, _ := a.store.Content(source.ID)
	if strings.Contains(string(original), "工作 | ") {
		t.Fatal("source overwritten")
	}
	a.openAggregationEditor(aggregate.ID)
	a.aggregationInclude = "["
	a.saveAggregation()
	if !a.showAggregation || a.aggregationInclude != "[" || a.aggregationError == "" {
		t.Fatal("failed save lost draft")
	}
	p, _ := a.store.Get(aggregate.ID)
	if p.Aggregate.Include != "" {
		t.Fatal("failed save changed saved spec")
	}
}

func TestAggregationEditorSourceOrderAndSmallWindow(t *testing.T) {
	a := subscriptionTestApp(t)
	a.booted, a.ticking, a.page = true, true, "subs"
	first := aggregationTestSource(t, a, "工作与家庭共用的长期订阅来源名称 · 包含多个地区的备用连接", "one.example")
	second := aggregationTestSource(t, a, "另一个来源", "two.example")
	a.openAggregationEditor("")
	for i := range a.aggregationSources {
		a.aggregationSources[i].Selected = true
	}
	before := a.aggregationSources[0].ID
	a.moveAggregationSource(0, 1)
	if a.aggregationSpec().Sources[1].ID != before {
		t.Fatal("source order not editable")
	}
	a.moveAggregationSource(-1, 1) // ignore stale control index
	tr := ui.NewTester(a.View, 840, 560)
	tr.SetTitleBar(ui.TitleBar{Height: 38, Left: 80})
	tr.Frame()
	for _, label := range []string{"保存聚合", "预览聚合", "取消", "聚合配置名称"} {
		r, ok := tr.Find(label)
		if !ok || r.X < 0 || r.X+r.W > 840 || r.Y+r.H > 560 {
			t.Fatalf("clipped %s: %+v", label, r)
		}
	}
	if first.ID == second.ID {
		t.Fatal("fixture IDs collided")
	}
	a.aggregationError = strings.Repeat("来源配置中包含尚未支持的代理集合，请选择直接列出节点的来源。", 20)
	tr.Frame()
	for _, label := range []string{"保存聚合", "预览聚合", "取消"} {
		r, ok := tr.Find(label)
		if !ok || r.Y+r.H > 560 {
			t.Fatalf("long error hid %s", label)
		}
	}
	if err := tr.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if a.showAggregation {
		t.Fatal("cancel did not close")
	}
}
