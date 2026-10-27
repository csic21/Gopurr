package app

import (
	"bytes"
	"encoding/json"
	"image/png"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"gopurr/internal/core"
	"gopurr/internal/kernel"
	"gopurr/internal/patch"
)

func extensionTestApp(t *testing.T) *App {
	t.Helper()
	// Short names also fit macOS's Unix socket path limit in controller tests.
	dir, err := os.MkdirTemp("", "gopurr-ext-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	ov := patch.Default()
	ov.ExtraRules = []string{"DOMAIN-SUFFIX,saved.example,DIRECT"}
	ov.MergeYAML = "profile:\n  store-selected: true\n"
	a := &App{
		ov:      ov,
		ovPath:  filepath.Join(dir, "overrides.json"),
		cfgText: "rules:\n  - MATCH,DIRECT\n",
	}
	a.initExtensionDrafts()
	if err := a.saveOverridesAtomic(); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestExtraRuleEditing(t *testing.T) {
	draft := "# keep this comment\r\n DOMAIN,a.example,DIRECT \r\n\r\nDOMAIN,a.example,DIRECT\nDOMAIN,b.example,REJECT\n"
	want := []string{"DOMAIN,a.example,DIRECT", "DOMAIN,a.example,DIRECT", "DOMAIN,b.example,REJECT"}
	if got := extraRuleLines(draft); !reflect.DeepEqual(got, want) {
		t.Fatalf("rule lines = %#v", got)
	}
	removed := removeExtraRule(draft, 1)
	if !strings.HasPrefix(removed, "# keep this comment\r\n DOMAIN,a.example,DIRECT \r\n\r\n") {
		t.Fatalf("deleting a duplicate damaged the rest of the draft: %q", removed)
	}
	if got := extraRuleLines(removed); !reflect.DeepEqual(got, []string{want[0], want[2]}) {
		t.Fatalf("deleted the wrong rule: %#v", got)
	}
	for _, index := range []int{-1, 99} {
		if got := removeExtraRule(draft, index); got != draft {
			t.Fatalf("invalid index %d changed the draft", index)
		}
	}
}

func TestExtraRuleBuilder(t *testing.T) {
	got, err := buildExtraRule("DOMAIN-SUFFIX", " example.com ", " 节点选择 ")
	if err != nil || got != "DOMAIN-SUFFIX,example.com,节点选择" {
		t.Fatalf("build = %q, %v", got, err)
	}
	for _, fields := range [][3]string{
		{"", "example.com", "DIRECT"},
		{"DOMAIN", "  ", "DIRECT"},
		{"DOMAIN", "example.com", ""},
		{"DOMAIN", "example.com,REJECT", "DIRECT"},
		{"DOMAIN", "example.com", "DIRECT\nREJECT"},
	} {
		if _, err := buildExtraRule(fields[0], fields[1], fields[2]); err == nil {
			t.Errorf("accepted invalid builder input: %#v", fields)
		}
	}
	groups := []core.Group{{Name: "节点选择"}, {Name: "DIRECT"}, {Name: "节点选择"}, {Name: ""}}
	if got := ruleTargets(groups, "暂时离线的策略组"); !reflect.DeepEqual(got, []string{"DIRECT", "REJECT", "节点选择", "暂时离线的策略组"}) {
		t.Fatalf("targets lost a pending selection or contain duplicates: %#v", got)
	}
}

func TestExtensionSaveKeepsOtherDraft(t *testing.T) {
	a := extensionTestApp(t)
	a.extraRulesDraft = "# local rules\nDOMAIN,example.com,DIRECT\n\nIP-CIDR,192.168.0.0/16,DIRECT,no-resolve"
	a.mergeYAMLDraft = "dns: [" // an independent, unfinished draft
	if err := a.saveExtraRulesDraft(); err != nil {
		t.Fatal(err)
	}
	if a.extraRulesSaved != a.extraRulesDraft || a.extraRulesError != "" {
		t.Fatal("successful rule save did not update the saved state")
	}
	if a.mergeYAMLDraft != "dns: [" || a.ov.MergeYAML != a.mergeYAMLSaved {
		t.Fatal("saving rules applied or discarded the YAML draft")
	}
	if !strings.Contains(a.extraRulesStatus, "下次启动") {
		t.Fatalf("stopped kernel status = %q", a.extraRulesStatus)
	}
	var saved patch.Overrides
	if err := json.Unmarshal(readExtensionFile(t, a.ovPath), &saved); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved, a.ov) {
		t.Fatalf("persisted overrides differ from the accepted candidate: %#v", saved)
	}
	if info, err := os.Stat(a.ovPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("overrides should remain private: %v, %v", info, err)
	}

	// Clear each persisted extension explicitly, without clearing the other.
	a.extraRulesDraft = ""
	if err := a.saveExtraRulesDraft(); err != nil || len(a.ov.ExtraRules) != 0 {
		t.Fatalf("clear rules: %v, %#v", err, a.ov.ExtraRules)
	}
	a.mergeYAMLDraft = ""
	if err := a.saveMergeYAMLDraft(); err != nil || a.ov.MergeYAML != "" {
		t.Fatalf("clear YAML: %v, %q", err, a.ov.MergeYAML)
	}
}

func TestExtensionValidationPreservesSavedState(t *testing.T) {
	for _, field := range []string{"YAML", "rules", "base config"} {
		t.Run(field, func(t *testing.T) {
			a := extensionTestApp(t)
			previous := a.ov
			before := readExtensionFile(t, a.ovPath)
			var err error
			switch field {
			case "YAML":
				a.mergeYAMLDraft = "dns: ["
				err = a.saveMergeYAMLDraft()
				if a.mergeYAMLDraft != "dns: [" || a.mergeYAMLError == "" || a.mergeYAMLSaved != previous.MergeYAML {
					t.Fatal("YAML failure discarded the draft or hid the error")
				}
			case "rules":
				a.extraRulesDraft = "IP-CIDR,not-a-network,DIRECT"
				err = a.saveExtraRulesDraft()
				if a.extraRulesDraft != "IP-CIDR,not-a-network,DIRECT" || a.extraRulesError == "" {
					t.Fatal("rule failure discarded the draft or hid the error")
				}
			case "base config":
				a.cfgText = "rules: ["
				a.extraRulesDraft = "DOMAIN,valid.example,DIRECT"
				err = a.saveExtraRulesDraft()
			}
			if err == nil || !reflect.DeepEqual(a.ov, previous) {
				t.Fatalf("validation did not roll back overrides: %v, %#v", err, a.ov)
			}
			if !bytes.Equal(readExtensionFile(t, a.ovPath), before) {
				t.Fatal("validation failure touched the saved file")
			}
		})
	}
}

// A tiny controller exercises the real Kernel.Start and runtime.yaml rollback,
// while avoiding a daemon process, network dependencies and OS proxy changes.
func TestExtensionRunningSaveTransaction(t *testing.T) {
	const candidate = "profile:\n  store-selected: false\n"
	for _, outcome := range []string{"accepted", "kernel rejected", "write failed", "rollback rejected"} {
		t.Run(outcome, func(t *testing.T) {
			a := extensionTestApp(t)
			previous := a.ov
			originalPath := a.ovPath
			before := readExtensionFile(t, originalPath)
			mgr, err := kernel.NewManager(filepath.Dir(a.ovPath))
			if err != nil {
				t.Fatal(err)
			}
			a.mgr, a.k = mgr, kernel.New(mgr)
			runtimeBefore, err := a.configBytes()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(mgr.ConfigPath(), runtimeBefore, 0o600); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			var reloads [][]byte
			persistedTooEarly := false
			listener, err := net.Listen("unix", filepath.Join(filepath.Dir(originalPath), "core.sock"))
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/version":
					_, _ = w.Write([]byte(`{"version":"test","meta":true}`))
				case r.URL.Path == "/configs" && r.Method == http.MethodPut:
					body, readErr := os.ReadFile(mgr.ConfigPath())
					if readErr != nil {
						http.Error(w, readErr.Error(), http.StatusInternalServerError)
						return
					}
					persisted, _ := os.ReadFile(originalPath)
					mu.Lock()
					reloads = append(reloads, body)
					persistedTooEarly = persistedTooEarly || !bytes.Equal(persisted, before)
					mu.Unlock()
					if outcome == "kernel rejected" || outcome == "rollback rejected" && bytes.Equal(body, runtimeBefore) {
						http.Error(w, `{"message":"test core rejected candidate"}`, http.StatusBadRequest)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				default:
					http.NotFound(w, r)
				}
			})}
			go func() { _ = server.Serve(listener) }()
			t.Cleanup(func() { _ = server.Close() })
			if outcome == "write failed" || outcome == "rollback rejected" {
				// Renaming a file over a directory fails even when run as root.
				a.ovPath = filepath.Join(filepath.Dir(originalPath), "blocked")
				if err := os.Mkdir(a.ovPath, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			a.mergeYAMLDraft = candidate
			err = a.saveMergeYAMLDraft()
			mu.Lock()
			defer mu.Unlock()
			if persistedTooEarly {
				t.Fatal("overrides were persisted before the kernel accepted them")
			}
			if outcome == "accepted" {
				if err != nil || a.mergeYAMLSaved != a.mergeYAMLDraft || len(reloads) != 1 {
					t.Fatalf("accepted save = %v, %d reloads", err, len(reloads))
				}
				if bytes.Equal(readExtensionFile(t, originalPath), before) || !strings.Contains(a.mergeYAMLStatus, "重新载入") {
					t.Fatal("accepted config was not persisted or reported as applied")
				}
				return
			}
			if err == nil || a.mergeYAMLError == "" || !reflect.DeepEqual(a.ov, previous) {
				t.Fatalf("failed transaction was not reported/rolled back: %v, %#v", err, a.ov)
			}
			if a.mergeYAMLDraft != candidate || a.mergeYAMLSaved != previous.MergeYAML {
				t.Fatal("failed save changed the draft or saved marker")
			}
			if !bytes.Equal(readExtensionFile(t, originalPath), before) {
				t.Fatal("failed transaction changed the persisted overrides")
			}
			if outcome == "rollback rejected" {
				if len(reloads) != 2 || !strings.Contains(a.mergeYAMLError, "旧配置重新载入也失败") {
					t.Fatalf("rollback failure was hidden: %s", a.mergeYAMLError)
				}
			} else if !bytes.Equal(readExtensionFile(t, mgr.ConfigPath()), runtimeBefore) {
				t.Fatal("failed transaction left the new runtime on disk")
			}
			if outcome == "write failed" && (len(reloads) != 2 || !bytes.Equal(reloads[1], runtimeBefore)) {
				t.Fatal("persistence failure did not reload the previous runtime")
			}
			entries, err := os.ReadDir(filepath.Dir(originalPath))
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".overrides-") {
					t.Fatalf("failed atomic write leaked temporary file: %s", entry.Name())
				}
			}
		})
	}
}

func TestExtensionTabInteraction(t *testing.T) {
	a := extensionTestApp(t)
	// 700 DIPs of content, matching the app at a normal desktop window size.
	tr := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme(c.Theme().Dark))
		ui.Column(c).Fill().Padding(20).Gap(12).Children(func() { a.rulesPage(c) })
	}, 740, 700)
	tr.Frame()
	click := func(label string) {
		t.Helper()
		if err := tr.Click(label); err != nil {
			t.Fatal(err)
		}
		tr.Frame()
	}
	assertVisible := func(labels ...string) {
		t.Helper()
		for _, label := range labels {
			r, ok := tr.Find(label)
			if !ok || r.X < 0 || r.Y < 0 || r.X+r.W > 740 || r.Y+r.H > 700 {
				t.Fatalf("%q is missing or clipped: %+v", label, r)
			}
		}
	}
	click("额外规则")
	assertVisible("额外规则草稿", "添加到草稿", "保存额外规则")
	click("额外规则草稿")
	tr.Command("selectAll")
	tr.Type("DOMAIN,unsaved.example,DIRECT")
	tr.Frame()
	if a.extraRulesDraft != "DOMAIN,unsaved.example,DIRECT" {
		t.Fatalf("textarea did not edit the rule draft: %q", a.extraRulesDraft)
	}
	writeExtensionShot(t, tr, "rules-extra")
	click("逐条删除")
	assertVisible("删除第 1 条额外规则")
	click("删除第 1 条额外规则")
	if len(extraRuleLines(a.extraRulesDraft)) != 0 {
		t.Fatal("per-rule delete did not update the draft")
	}
	click("额外规则草稿")
	tr.Type("DOMAIN,unsaved.example,DIRECT")
	tr.Frame()
	click("YAML 覆写")
	assertVisible("YAML 覆写草稿", "查看模板", "保存 YAML 覆写")
	click("YAML 覆写草稿")
	tr.Command("selectAll")
	tr.Type("dns: [")
	tr.Frame()
	before := readExtensionFile(t, a.ovPath)
	click("保存 YAML 覆写")
	if a.mergeYAMLError == "" || !tr.HasText(a.mergeYAMLError) || !bytes.Equal(readExtensionFile(t, a.ovPath), before) {
		t.Fatal("invalid YAML was persisted or its error was hidden")
	}
	assertVisible(a.mergeYAMLError, "保存 YAML 覆写")
	writeExtensionShot(t, tr, "rules-yaml-error")
	click("查看模板")
	if a.mergeYAMLDraft != "dns: [" || !bytes.Equal(readExtensionFile(t, a.ovPath), before) {
		t.Fatal("viewing a template modified or saved the draft")
	}
	click("额外规则")
	if a.extraRulesDraft != "DOMAIN,unsaved.example,DIRECT" || a.mergeYAMLDraft != "dns: [" {
		t.Fatal("switching tabs discarded one of the drafts")
	}
	click("清空草稿")
	if a.extraRulesDraft != "" || !bytes.Equal(readExtensionFile(t, a.ovPath), before) {
		t.Fatal("clearing the draft applied it immediately")
	}
	click("放弃修改")
	if a.extraRulesDraft != a.extraRulesSaved || a.mergeYAMLDraft != "dns: [" {
		t.Fatal("reset affected the wrong draft")
	}
}

func readExtensionFile(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// Opt-in screenshots go outside the checkout so ordinary test runs don't
// mutate docs or other agents' artifacts.
func writeExtensionShot(t *testing.T, tr *ui.Tester, name string) {
	t.Helper()
	dir := os.Getenv("GOPURR_RULES_RENDER_DIR")
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tr.Image()); err != nil {
		t.Fatal(err)
	}
	t.Logf("screenshot: %s", f.Name())
}
