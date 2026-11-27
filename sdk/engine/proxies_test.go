package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gopurr/internal/patch"
	"gopurr/internal/profile"
)

func testMobileProxyEditorCreateLocalVPNSafetyAndRefreshSurvival(t *testing.T, e *Engine) {
	t.Helper()
	call(t, e, "engine.stop", nil)

	// 1. proxies.parse
	var parsed profile.ParseResult
	_ = json.Unmarshal(call(t, e, "proxies.parse", map[string]any{
		"text": "trojan://secret@vps.example.com:443#MyVPS\ninvalid-line\n",
	}), &parsed)
	if parsed.Kind != "nodes" || len(parsed.Nodes) != 1 || len(parsed.Failures) != 1 {
		t.Fatalf("unexpected mobile parse result: %+v", parsed)
	}

	// 2. profiles.createLocal + pure local proxies.save
	var created ProfileMutation
	_ = json.Unmarshal(call(t, e, "profiles.createLocal", map[string]any{
		"name":     "Local VPS Pack",
		"nodes":    parsed.Nodes,
		"activate": true,
	}), &created)
	if !created.Saved || created.Profile == nil || !created.Profile.IsPureLocal || created.Profile.NodeCount != 1 {
		t.Fatalf("createLocal failed: %+v", created)
	}
	localID := created.Profile.ID
	var localEd ProxyEditorPayload
	_ = json.Unmarshal(call(t, e, "proxies.editor", map[string]any{"id": localID}), &localEd)
	if localEd.ProfileID != localID || !localEd.IsPureLocal || len(localEd.Nodes) != 1 {
		t.Fatalf("unexpected local proxy editor: %+v", localEd)
	}

	// Add a second node to pure local profile -> persists directly into YAML
	var localSave ProfileMutation
	_ = json.Unmarshal(call(t, e, "proxies.save", map[string]any{
		"id": localID,
		"edits": map[string]any{
			"nodes": []any{
				map[string]any{
					"source":  "",
					"localId": "v2",
					"name":    "SecondVPS",
					"server":  "vps2.example.com",
					"port":    8443,
					"options": map[string]any{"type": "trojan", "password": "pw2"},
				},
			},
			"groups": []any{
				map[string]any{"source": profile.AggregateSelect, "appendMembers": []string{"SecondVPS"}},
			},
		},
	}), &localSave)
	if !localSave.Saved || localSave.Profile.NodeCount != 2 {
		t.Fatalf("pure local save failed: %+v", localSave)
	}
	rawLocal, _ := e.store.Content(localID)
	if !strings.Contains(string(rawLocal), "SecondVPS") {
		t.Fatalf("pure local save did not write YAML: %s", rawLocal)
	}

	// 3. Import remote subscription with empty name (inferred from profile-title) + custom UA + updateHours + activate
	var subBody atomic.Value
	subBody.Store(`proxies:
  - {name: SubNodeA, type: direct}
proxy-groups:
  - {name: Proxy, type: select, proxies: [SubNodeA, DIRECT]}
rules:
  - DOMAIN-SUFFIX,example.com,Proxy
  - MATCH,DIRECT
`)
	var gotUA atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA.Store(r.Header.Get("User-Agent"))
		w.Header().Set("profile-title", "Provider Rules")
		w.Header().Set("subscription-userinfo", "upload=100; download=200; total=1000; expire=1900000000")
		_, _ = w.Write([]byte(subBody.Load().(string)))
	}))
	defer server.Close()

	hours := 6
	var imported Profile
	_ = json.Unmarshal(call(t, e, "profiles.import", map[string]any{
		"url":         server.URL + "/sub",
		"userAgent":   "MobileUA/2.0",
		"updateHours": hours,
		"activate":    true,
	}), &imported)
	if imported.Name != "Provider Rules" || gotUA.Load() != "MobileUA/2.0" || imported.Info.Total != 1000 || !imported.Active {
		t.Fatalf("enriched import failed: %+v ua=%v", imported, gotUA.Load())
	}

	// 4. profiles.update with explicit clear of updateHours (JSON null)
	var updated ProfileMutation
	_ = json.Unmarshal(call(t, e, "profiles.update", map[string]any{
		"id":          imported.ID,
		"userAgent":   "MobileUA/3.0",
		"useProxy":    false,
		"updateHours": nil,
	}), &updated)
	if updated.Profile.UserAgent != "MobileUA/3.0" || updated.Profile.UpdateHours != nil {
		t.Fatalf("profiles.update did not clear updateHours: %+v", updated.Profile)
	}

	// 5. Start engine (simulating live mobile session), then add personal VPS node via proxies.save:
	// MUST persist to disk and set reconnectRequired=true WITHOUT hot-reloading live runtime!
	call(t, e, "engine.start", nil)
	var saveMut ProfileMutation
	_ = json.Unmarshal(call(t, e, "proxies.save", map[string]any{
		"id": imported.ID,
		"edits": map[string]any{
			"nodes": []any{
				map[string]any{
					"source":  "",
					"localId": "my-vps-1",
					"name":    "MyPersonalVPS",
					"server":  "198.51.100.1",
					"port":    443,
					"options": map[string]any{"type": "trojan", "password": "vps-secret"},
				},
			},
			"groups": []any{
				map[string]any{"source": "Proxy", "appendMembers": []string{"MyPersonalVPS"}},
			},
		},
	}), &saveMut)
	if !saveMut.Saved || saveMut.Applied || !saveMut.ReconnectRequired || !saveMut.Status.ReconnectRequired {
		t.Fatalf("Android VPN safety violated on proxies.save: %+v", saveMut)
	}
	if saveMut.Profile.LocalNodeCount != 1 || saveMut.Profile.NodeCount != 2 {
		t.Fatalf("profile counts not updated: %+v", saveMut.Profile)
	}
	// Live running groups must NOT have hot-reloaded MyPersonalVPS yet!
	var liveGroups GroupList
	_ = json.Unmarshal(call(t, e, "proxies.groups", nil), &liveGroups)
	if hasMember(liveGroups, "MyPersonalVPS") {
		t.Fatal("proxies.save hot-reloaded live mobile runtime")
	}

	// 6. Refresh subscription with changed upstream nodes AND updated rules -> MyPersonalVPS SURVIVES!
	subBody.Store(`proxies:
  - {name: SubNodeB, type: direct}
  - {name: SubNodeC, type: direct}
proxy-groups:
  - {name: Proxy, type: select, proxies: [SubNodeB, SubNodeC, DIRECT]}
rules:
  - DOMAIN-SUFFIX,example.com,Proxy
  - DOMAIN-SUFFIX,openai.com,Proxy
  - MATCH,DIRECT
`)
	var refreshMut ProfileMutation
	_ = json.Unmarshal(call(t, e, "profiles.refresh", map[string]any{"id": imported.ID}), &refreshMut)
	if !refreshMut.Saved || refreshMut.Applied || !refreshMut.ReconnectRequired {
		t.Fatalf("refresh after local VPS add failed: %+v", refreshMut)
	}

	// Reconnect (stop -> start) -> both new upstream nodes AND MyPersonalVPS are active!
	call(t, e, "engine.stop", nil)
	call(t, e, "engine.start", nil)
	var afterGroups GroupList
	_ = json.Unmarshal(call(t, e, "proxies.groups", nil), &afterGroups)
	if !hasMember(afterGroups, "MyPersonalVPS") || !hasMember(afterGroups, "SubNodeB") || !hasMember(afterGroups, "SubNodeC") {
		t.Fatalf("local VPS or refreshed upstream nodes missing after reconnect: %+v", afterGroups)
	}
	// Verify proxy editor shows dual-mode fields cleanly
	var subEd ProxyEditorPayload
	_ = json.Unmarshal(call(t, e, "proxies.editor", map[string]any{"id": imported.ID}), &subEd)
	if len(subEd.Groups) != 1 || subEd.Groups[0].Explicit || len(subEd.Groups[0].AppendMembers) != 1 || subEd.Groups[0].AppendMembers[0] != "MyPersonalVPS" {
		t.Fatalf("unexpected subscription proxy editor dual-mode payload: %+v", subEd.Groups)
	}

	// 7. Deleting local VPS node cleanly removes AppendMembers reference
	var delMut ProfileMutation
	_ = json.Unmarshal(call(t, e, "proxies.save", map[string]any{
		"id": imported.ID,
		"edits": map[string]any{
			"nodes":  []any{},
			"groups": []any{map[string]any{"source": "Proxy", "appendMembers": []string{"MyPersonalVPS"}}},
		},
	}), &delMut)
	if !delMut.Saved || delMut.Profile.LocalNodeCount != 0 || delMut.Profile.EditedGroupCount != 0 {
		t.Fatalf("deleting local node did not prune group edit: %+v", delMut.Profile)
	}
	// Ensure no stale Patch edits remain
	pAfter, _ := e.store.Get(imported.ID)
	if pAfter.Overrides != nil && pAfter.Overrides.ProxyEdits != nil && len(pAfter.Overrides.ProxyEdits.Nodes)+len(pAfter.Overrides.ProxyEdits.Groups) > 0 {
		t.Fatalf("stale edits remained: %+v", pAfter.Overrides.ProxyEdits)
	}
	_ = patch.ProxyEdits{}
}
