package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestSubscriptionAddDialogRetainsErrorsAndClosesAfterSuccess(t *testing.T) {
	a := subscriptionTestApp(t)
	if _, err := a.store.Add(startSubscriptionSource(t), "已有配置"); err != nil {
		t.Fatal(err)
	}
	a.booted, a.ticking, a.page = true, true, "subs"
	tr := ui.NewTester(a.View, 840, 560)
	tr.Advance(200 * time.Millisecond)
	if err := tr.Click("添加订阅"); err != nil {
		t.Fatal(err)
	}
	if err := tr.Click("添加"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	_, message := a.subState()
	if message == "" || !a.showAddSub {
		t.Fatal("validation error dismissed the add dialog")
	}
	if err := tr.Click("取消"); err != nil {
		t.Fatal(err)
	}
	if err := tr.Click("添加订阅"); err != nil {
		t.Fatal(err)
	}
	tr.Frame()
	if !a.showAddSub {
		t.Fatal("a previous failed attempt prevents reopening")
	}
	a.subURL, a.subName = startSubscriptionSource(t), "第二个配置"
	tr.Frame()
	if err := tr.Click("添加"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		busy, message := a.subState()
		if !busy {
			if message != "" {
				t.Fatal(message)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("subscription download did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	tr.Frame()
	if a.showAddSub || len(a.store.List()) != 2 || !tr.HasText("第二个配置") {
		t.Fatal("successful addition did not return to the configuration list")
	}
}

func subscriptionTestApp(t *testing.T) *App {
	t.Helper()
	dir, err := os.MkdirTemp("", "gopurr-sub-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("GOPURR_HOME", dir)
	a := New()
	secret, err := os.ReadFile(filepath.Join(dir, "core.secret"))
	if err != nil {
		t.Fatal(err)
	}
	startFakeCore(t, dir, string(secret))
	return a
}

func TestFirstSubscriptionIsApplied(t *testing.T) {
	a := subscriptionTestApp(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("rules:\n  - DOMAIN-SUFFIX,imported.example,DIRECT\n  - MATCH,DIRECT\n"))
	}))
	defer srv.Close()
	a.subURL = srv.URL
	a.addSubscription()
	deadline := time.Now().Add(3 * time.Second)
	for {
		busy, errText := a.subState()
		if !busy {
			if errText != "" {
				t.Fatal(errText)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("import did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	raw, err := os.ReadFile(a.mgr.ConfigPath())
	if err != nil || !strings.Contains(string(raw), "imported.example") {
		t.Fatalf("first import not applied: %s, %v", raw, err)
	}
	active := a.store.ActiveID()
	if active == "" {
		t.Fatal("imported profile not activated")
	}
	a.removeSubscription(active)
	if len(a.store.List()) != 1 {
		t.Fatal("active profile deletion was not protected")
	}
}

func TestSubscriptionSwitchRollsBackOnInvalidOverrides(t *testing.T) {
	a := subscriptionTestApp(t)
	oldFile, newFile := filepath.Join(a.dir, "old.yaml"), filepath.Join(a.dir, "new.yaml")
	if err := os.WriteFile(oldFile, []byte("proxy-groups:\n  - name: 旧策略\n    type: select\n    proxies: [DIRECT]\nrules: [MATCH,DIRECT]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newFile, []byte("rules:\n  - MATCH,DIRECT\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old, err := a.store.AddFile(oldFile, "")
	if err != nil {
		t.Fatal(err)
	}
	next, err := a.store.AddFile(newFile, "")
	if err != nil {
		t.Fatal(err)
	}
	a.ov.ExtraRules = []string{"DOMAIN,example.com,旧策略"}
	a.useSubscription(next.ID)
	if a.store.ActiveID() != old.ID {
		t.Fatal("failed switch replaced active profile")
	}
	_, errText := a.subState()
	if !strings.Contains(errText, "旧策略") {
		t.Fatalf("failure did not identify invalid override target: %s", errText)
	}
}
