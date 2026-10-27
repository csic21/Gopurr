package kernel

import (
	"net/http"
	"os"
	"reflect"
	"testing"

	"gopurr/internal/core"
)

func groupNames(groups []core.Group) []string {
	names := make([]string, len(groups))
	for i, group := range groups {
		names[i] = group.Name
	}
	return names
}

func TestRemoteGroupsFollowSubscriptionOrder(t *testing.T) {
	_, remote := uxController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"proxies":{
			"Ⓜ️ Microsoft":{"type":"Selector","all":["DIRECT"]},
			"🎥 Netflix":{"type":"Selector","all":["DIRECT"]},
			"🔰 手动选择":{"type":"Selector","all":["DIRECT"]},
			"GLOBAL":{"type":"Selector","all":["DIRECT","node","🔰 手动选择","🎥 Netflix","Ⓜ️ Microsoft","🔰 手动选择","missing"]},
			"DIRECT":{"type":"Direct"},"node":{"type":"VLESS"}
		}}`))
	}))
	want := []string{"🔰 手动选择", "🎥 Netflix", "Ⓜ️ Microsoft", "GLOBAL"}
	for i := 0; i < 5; i++ {
		if got := groupNames(remote.Groups()); !reflect.DeepEqual(got, want) {
			t.Fatalf("controller map lost subscription order: %v", got)
		}
	}
}

func TestKernelGroupsFollowRuntimeConfig(t *testing.T) {
	manager, _ := uxController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version":
			_, _ = w.Write([]byte(`{"version":"test"}`))
		case "/configs":
			w.WriteHeader(http.StatusNoContent)
		case "/proxies":
			// An explicitly configured GLOBAL can have a different member order.
			_, _ = w.Write([]byte(`{"proxies":{
				"Microsoft":{"type":"Selector","all":["DIRECT"]},
				"手动选择":{"type":"Selector","all":["DIRECT"]},
				"Netflix":{"type":"Selector","all":["DIRECT"]},
				"GLOBAL":{"type":"Selector","all":["Microsoft","手动选择"]}
			}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	raw := []byte("proxy-groups:\n  - {name: 手动选择}\n  - {name: Netflix}\n  - {name: Microsoft}\n")
	if err := os.WriteFile(manager.ConfigPath(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	k := New(manager)
	if got := groupNames(k.Groups()); !reflect.DeepEqual(got, []string{"手动选择", "Netflix", "Microsoft", "GLOBAL"}) {
		t.Fatalf("persisted config order = %v", got)
	}
	if err := k.Start([]byte("proxy-groups:\n  - name: Netflix\n  - name: 手动选择\n  - name: Microsoft\n")); err != nil {
		t.Fatal(err)
	}
	if got := groupNames(k.Groups()); !reflect.DeepEqual(got, []string{"Netflix", "手动选择", "Microsoft", "GLOBAL"}) {
		t.Fatalf("reloaded config order = %v", got)
	}
}
