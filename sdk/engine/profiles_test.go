package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopurr/internal/core"
	"gopurr/internal/profile"
)

func testProfileChanges(t *testing.T, e *Engine, id, sourceURL string, sourceBody *atomic.Value) {
	t.Helper()
	params := map[string]string{"id": id}
	call(t, e, "engine.start", nil)
	call(t, e, "proxies.select", map[string]string{"group": "出口", "member": "直连 B"})
	var plan TunnelPlan
	_ = json.Unmarshal(call(t, e, "engine.tunnelPlan", map[string]any{"appScope": map[string]any{"mode": "exclude", "packages": []string{"com.example.browser"}}}), &plan)
	if _, err := e.validatePendingPlanLocked(plan.Token); err != nil {
		t.Fatal(err)
	}
	before, err := e.store.Content(id)
	if err != nil {
		t.Fatal(err)
	}
	var changed ProfileMutation
	_ = json.Unmarshal(call(t, e, "profiles.update", map[string]string{"id": id, "name": "Renamed", "url": sourceURL + "/moved?token=source-secret"}), &changed)
	if !changed.Saved || changed.Applied || changed.ReconnectRequired {
		t.Fatalf("source metadata edit altered runtime: %+v", changed)
	}
	after, _ := e.store.Content(id)
	if string(before) != string(after) {
		t.Fatal("metadata edit overwrote cached content")
	}
	if _, err := e.validatePendingPlanLocked(plan.Token); err != nil {
		t.Fatalf("metadata-only edit invalidated matching data plan: %v", err)
	}
	editor := call(t, e, "profiles.editor", params)
	if !strings.Contains(string(editor), "source-secret") {
		t.Fatal("explicit editor did not return source URL")
	}
	if strings.Contains(string(call(t, e, "profiles.list", nil)), "source-secret") {
		t.Fatal("source credential leaked in list")
	}

	next := strings.ReplaceAll(directConfig, "直连 B", "新节点 C")
	sourceBody.Store(next)
	_ = json.Unmarshal(call(t, e, "profiles.refresh", params), &changed)
	if !changed.Saved || changed.Applied || !changed.ReconnectRequired || !changed.Status.Running || changed.Status.Busy {
		t.Fatalf("staged refresh result: %+v", changed)
	}
	var groups GroupList
	_ = json.Unmarshal(call(t, e, "proxies.groups", nil), &groups)
	if !hasMember(groups, "直连 B") || hasMember(groups, "新节点 C") {
		t.Fatal("refresh silently replaced the active runtime")
	}
	if _, err := e.validatePendingPlanLocked(plan.Token); asError(err).Code != "stale_plan" {
		t.Fatalf("old plan survived source commit: %v", err)
	}
	sourceBody.Store("rules: ['MATCH,missing']\n")
	_, err = e.Call(context.Background(), "profiles.refresh", json.RawMessage(fmt.Sprintf(`{"id":%q}`, id)))
	if err == nil {
		t.Fatal("invalid refresh was saved")
	}
	after, _ = e.store.Content(id)
	if string(after) != next {
		t.Fatal("failed refresh discarded previous valid cache")
	}
	sourceBody.Store(next + "# index-failure\n")
	index := filepath.Join(e.home, "profiles.json")
	backup := index + ".sdk-test"
	if err := os.Rename(index, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(index, 0700); err != nil {
		t.Fatal(err)
	}
	_, err = e.Call(context.Background(), "profiles.refresh", json.RawMessage(fmt.Sprintf(`{"id":%q}`, id)))
	if err == nil {
		t.Fatal("refresh ignored disk index failure")
	}
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, index); err != nil {
		t.Fatal(err)
	}
	after, _ = e.store.Content(id)
	if string(after) != next {
		t.Fatal("failed disk commit changed profile cache")
	}
	if _, err := e.Call(context.Background(), "profiles.delete", json.RawMessage(fmt.Sprintf(`{"id":%q}`, id))); asError(err).Code != "profile_in_use" {
		t.Fatalf("deleted running profile: %v", err)
	}
	call(t, e, "engine.stop", nil)
	call(t, e, "engine.start", nil)
	var status Status
	_ = json.Unmarshal(call(t, e, "engine.status", nil), &status)
	if status.ReconnectRequired {
		t.Fatal("reconnect did not accept staged profile")
	}
	_ = json.Unmarshal(call(t, e, "proxies.groups", nil), &groups)
	if !hasMember(groups, "新节点 C") || hasMember(groups, "直连 B") {
		t.Fatal("reconnect did not use saved profile")
	}
	if groups.Groups[0].Now != "直连 A" || len(e.manualSelections[id]) != 0 {
		t.Fatalf("removed selection did not safely fall back to default: %+v", groups)
	}
	assertSavedSelection(t, e, id, "")
	call(t, e, "proxies.select", map[string]string{"group": "出口", "member": "新节点 C"})
	call(t, e, "engine.stop", nil)
	call(t, e, "engine.start", nil)
	_ = json.Unmarshal(call(t, e, "proxies.groups", nil), &groups)
	if groups.Groups[0].Now != "新节点 C" {
		t.Fatalf("refreshed profile lost manual selection on reconnect: %+v", groups)
	}
	testDelayDoesNotBlockDisconnect(t, e)
	if err := os.Rename(index, backup); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(index, 0700); err != nil {
		t.Fatal(err)
	}
	_, err = e.Call(context.Background(), "profiles.delete", json.RawMessage(fmt.Sprintf(`{"id":%q}`, id)))
	if err == nil {
		t.Fatal("delete ignored index failure")
	}
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(backup, index); err != nil {
		t.Fatal(err)
	}
	if _, err := e.store.Content(id); err != nil {
		t.Fatalf("failed deletion lost source: %v", err)
	}
	call(t, e, "profiles.delete", params)
	if len(e.manualSelections[id]) != 0 {
		t.Fatal("deleted profile retained remembered selections")
	}
	assertSavedSelection(t, e, id, "")
	if e.store.ActiveID() != "" {
		t.Fatal("deleting stopped active profile left selection")
	}
}

func assertSavedSelection(t *testing.T, e *Engine, id, want string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(e.home, preferencesFile))
	if err != nil {
		t.Fatal(err)
	}
	var saved runtimePreferences
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Selections[id]["出口"] != want {
		t.Fatalf("stale selection on disk: %+v", saved)
	}
}

func hasMember(groups GroupList, name string) bool {
	for _, group := range groups.Groups {
		for _, member := range group.Members {
			if member.Name == name {
				return true
			}
		}
	}
	return false
}

func testDelayDoesNotBlockDisconnect(t *testing.T, e *Engine) {
	t.Helper()
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done() }))
	defer server.Close()
	params, _ := json.Marshal(map[string]string{"name": "直连 A", "url": server.URL})
	done := make(chan error, 1)
	go func() { _, err := e.Call(context.Background(), "proxies.delay", params); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("delay test did not reach local server")
	}
	statusDone := make(chan error, 1)
	go func() { _, err := e.Call(context.Background(), "engine.status", nil); statusDone <- err }()
	select {
	case err := <-statusDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("status waited for latency test")
	}
	stopDone := make(chan error, 1)
	go func() { _, err := e.Call(context.Background(), "engine.stop", nil); stopDone <- err }()
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("disconnect waited for latency test")
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("delay result survived disconnect: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("latency call survived disconnect")
	}
	if len(e.delays) != 0 {
		t.Fatal("late delay result was accepted after stop")
	}
}

func TestStatusAndDisconnectDoNotWaitForSubscriptionDownload(t *testing.T) {
	store, err := profile.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{store: store, core: &core.Core{}, scope: defaultScope()}
	started := make(chan struct{})
	finished := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(finished) }))
	defer server.Close()
	params, _ := json.Marshal(map[string]string{"name": "Slow source", "url": server.URL + "/?token=never-log-this"})
	imported := make(chan error, 1)
	go func() { _, err := e.Call(context.Background(), "profiles.import", params); imported <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("download never started")
	}
	statusDone := make(chan Status, 1)
	go func() {
		value, err := e.Call(context.Background(), "engine.status", nil)
		if err != nil {
			statusDone <- Status{}
			return
		}
		statusDone <- value.(Status)
	}()
	select {
	case status := <-statusDone:
		if !status.Busy || status.Operation == nil || status.Operation.Phase != "downloading" {
			t.Fatalf("missing live operation: %+v", status)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("status waited for HTTP")
	}
	stopDone := make(chan error, 1)
	go func() { _, err := e.Call(context.Background(), "engine.stop", nil); stopDone <- err }()
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("disconnect waited for HTTP")
	}
	select {
	case err := <-imported:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("download not cancelled: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled download remained alive")
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("HTTP connection survived disconnect")
	}
	if len(store.List()) != 0 {
		t.Fatal("disconnected import was saved")
	}
	value, err := e.Call(context.Background(), "engine.status", nil)
	if err != nil {
		t.Fatal(err)
	}
	if value.(Status).Busy {
		t.Fatal("busy state survived cancelled import")
	}
}

func TestProfileEditorRenameAndDeleteArePortable(t *testing.T) {
	store, err := profile.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.AddBytesChecked("Original", []byte("rules: ['MATCH,DIRECT']\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	e := &Engine{store: store, core: &core.Core{}}
	params, _ := json.Marshal(map[string]string{"id": p.ID, "name": "Renamed"})
	result, err := e.Call(context.Background(), "profiles.update", params)
	if err != nil {
		t.Fatal(err)
	}
	change := result.(ProfileMutation)
	if !change.Saved || change.Applied || change.ReconnectRequired {
		t.Fatalf("rename pretended to change runtime: %+v", change)
	}
	id, _ := json.Marshal(map[string]string{"id": p.ID})
	editor, err := e.Call(context.Background(), "profiles.editor", id)
	if err != nil {
		t.Fatal(err)
	}
	if editor.(ProfileEditor).Name != "Renamed" || editor.(ProfileEditor).URL != "" {
		t.Fatalf("bad local editor: %+v", editor)
	}
	raw, err := store.Content(p.ID)
	if err != nil || string(raw) != "rules: ['MATCH,DIRECT']\n" {
		t.Fatal("rename altered cached bytes")
	}
	if _, err := e.Call(context.Background(), "profiles.delete", id); err != nil {
		t.Fatal(err)
	}
	if len(store.List()) != 0 || store.ActiveID() != "" {
		t.Fatal("deleted portable profile is still active")
	}
}
