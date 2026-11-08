package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopurr/internal/applications"
	"gopurr/internal/backup"
	"gopurr/internal/patch"
	"gopurr/internal/profile"
	"gopurr/internal/securefs"
	"gopurr/internal/workflow"
)

func nativeParams(raw json.RawMessage) (map[string]json.RawMessage, error) {
	p := map[string]json.RawMessage{}
	if len(raw) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("invalid desktop parameters: %w", err)
	}
	if p == nil {
		return nil, errors.New("desktop parameters must be an object")
	}
	return p, nil
}
func nativeString(p map[string]json.RawMessage, key string) string {
	var s string
	_ = json.Unmarshal(p[key], &s)
	return s
}
func nativeBool(p map[string]json.RawMessage, key string) bool {
	var b bool
	_ = json.Unmarshal(p[key], &b)
	return b
}
func nativeMerge(p map[string]json.RawMessage, v any, keys ...string) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(body, &fields)
	if len(keys) == 0 {
		for k, v := range p {
			fields[k] = v
		}
	} else {
		for _, k := range keys {
			if raw, ok := p[k]; ok {
				fields[k] = raw
			}
		}
	}
	body, err = json.Marshal(fields)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

// NativeCall is the authenticated, local desktop RPC entry point. It never
// opens an old UI dialog: filesystem paths and confirmations come from NSH.
func (a *App) NativeCall(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if a == nil {
		return nil, errors.New("desktop model is unavailable")
	}
	p, err := nativeParams(raw)
	if err != nil {
		return nil, err
	}
	requiredBool := ""
	switch method {
	case "desktop.systemProxy.set", "desktop.tun.set", "desktop.mesh.autoConnect", "desktop.mesh.services":
		requiredBool = "enabled"
	case "desktop.mesh.block":
		requiredBool = "blocked"
	}
	if requiredBool != "" {
		var value bool
		field, ok := p[requiredBool]
		if !ok || string(field) == "null" || json.Unmarshal(field, &value) != nil {
			return nil, fmt.Errorf("%s must be a boolean", requiredBool)
		}
	}
	h := webHost{a}
	switch method {
	case "desktop.snapshot":
		return a.NativeSnapshot(ctx, nativeString(p, "page"), nativeString(p, "scopeId"))
	case "desktop.group.select":
		return nil, h.Select(ctx, nativeString(p, "group"), nativeString(p, "member"))
	case "desktop.group.delay", "desktop.node.delay":
		var names []string
		_ = json.Unmarshal(p["names"], &names)
		if n := nativeString(p, "name"); n != "" {
			names = []string{n}
		}
		return h.Delay(ctx, nativeString(p, "group"), names)
	case "desktop.mode.set":
		return nil, h.SetMode(ctx, nativeString(p, "mode"))
	case "desktop.systemProxy.set":
		return nil, h.SetSystemProxy(ctx, nativeBool(p, "enabled"))
	case "desktop.tun.set":
		return nil, h.SetTun(ctx, nativeBool(p, "enabled"))
	case "desktop.connections.close":
		return nil, h.CloseConnection(ctx, nativeString(p, "id"))
	case "desktop.connections.closeAll":
		return nil, h.CloseAllConnections(ctx)
	case "desktop.logs.clear":
		return nil, h.ClearLogs(ctx)
	case "desktop.applications.scan":
		return applications.Discover(ctx)
	case "desktop.applications.inspect":
		return applications.Inspect(ctx, nativeString(p, "path"))
	case "desktop.profiles.get":
		return nativeMain(ctx, func() (any, error) {
			if a.store == nil {
				return nil, errors.New("profile store unavailable")
			}
			id := nativeString(p, "id")
			item, e := a.store.Get(id)
			if e != nil {
				return nil, e
			}
			body, e := a.store.Content(id)
			if e != nil {
				return nil, e
			}
			b, _ := json.Marshal(item)
			var out map[string]any
			_ = json.Unmarshal(b, &out)
			out["content"] = string(body)
			return out, nil
		})
	case "desktop.profiles.import", "desktop.profiles.update", "desktop.profiles.updateAll", "desktop.profiles.edit", "desktop.profiles.activate", "desktop.profiles.delete", "desktop.profiles.aggregate", "desktop.profiles.aggregatePreview":
		return a.nativeProfileCall(ctx, method, p)
	case "desktop.proxies.editor":
		return nativeMain(ctx, func() (any, error) { return a.nativeProxyEditor(nativeString(p, "id")) })
	case "desktop.rules.save", "desktop.features.save", "desktop.features.policy", "desktop.proxies.save":
		return a.nativeSaveOverrides(ctx, method, p)
	case "desktop.library.save":
		return a.nativeModelJob(ctx, func(w *App) (any, error) {
			next := w.ov
			if e := nativeMerge(p, &next, "library", "templateIds"); e != nil {
				return nil, e
			}
			applied, e := w.commitOverrideLibrary(next)
			return map[string]any{"applied": applied}, e
		}, func(w *App) { a.ov = w.ov })
	case "desktop.runtime.local.save":
		return a.nativeModelJob(ctx, func(w *App) (any, error) {
			if w.store.ActiveID() != "" {
				return nil, errors.New("select the local configuration before editing it")
			}
			w.rawDraft = nativeString(p, "yaml")
			w.saveLocalConfig()
			if w.rawError != "" {
				return nil, errors.New(w.rawError)
			}
			return map[string]any{"saved": true}, nil
		}, func(w *App) { a.cfgText = w.cfgText })
	case "desktop.network.save":
		return a.nativeSaveNetwork(ctx, p)
	case "desktop.mesh.action", "desktop.mesh.services", "desktop.mesh.block", "desktop.mesh.autoConnect", "desktop.mesh.probe", "desktop.mesh.shortcut", "desktop.mesh.shortcut.save", "desktop.mesh.shortcut.forget":
		return a.nativeMeshCall(ctx, method, p)
	}
	return nativeMain(ctx, func() (any, error) {
		switch method {
		case "desktop.page.set":
			page := nativeString(p, "page")
			for _, n := range navItems {
				if n.id == page {
					a.NativeSetPage(page)
					return nil, nil
				}
			}
			return nil, errors.New("unknown desktop page")
		case "desktop.logs.export":
			return nil, securefs.WriteFile(nativeString(p, "path"), []byte(nativeString(p, "text")), 0600)
		case "desktop.prefs.save":
			if wrapped, ok := p["prefs"]; ok {
				var err error
				p, err = nativeParams(wrapped)
				if err != nil {
					return nil, err
				}
			}
			next := a.Prefs()
			if err := nativeMerge(p, &next); err != nil {
				return nil, err
			}
			if err := a.changePrefs(func(cur *Prefs) {
				previousHours := cur.AutoUpdateHours
				_ = nativeMerge(p, cur)
				if previousHours == 0 && cur.AutoUpdateHours > 0 {
					cur.LastAutoUpdate = time.Now()
				}
			}); err != nil {
				return nil, err
			}
			return a.Prefs(), nil
		case "desktop.kernel.toggle":
			if a.k == nil {
				return nil, errors.New("kernel unavailable")
			}
			if busyNetwork(a) {
				return nil, errors.New(tr("网络操作进行中，请稍后重试"))
			}
			a.Toggle()
			return map[string]any{"accepted": true}, nil
		case "desktop.kernel.reload", "desktop.engine.reload":
			if a.k == nil {
				return nil, errors.New("kernel unavailable")
			}
			if busyNetwork(a) {
				return nil, errors.New(tr("网络操作进行中，请稍后重试"))
			}
			a.Reload()
			return map[string]any{"accepted": true}, nil
		case "desktop.runtime.config":
			readOnly := a.store != nil && a.store.ActiveID() != ""
			body := []byte(a.cfgText)
			var err error
			if readOnly {
				body, err = a.configBytes()
				if err != nil {
					return nil, err
				}
			}
			return map[string]any{"yaml": string(body), "readOnly": readOnly, "source": a.configSource()}, nil

		case "desktop.runtime.reveal":
			return map[string]any{"path": a.dir}, nil
		case "desktop.remote.get":
			return a.remoteConfigSnapshot(), nil
		case "desktop.remote.token":
			return map[string]any{"token": a.remoteConfigSnapshot().Token}, nil
		case "desktop.remote.regenerateToken":
			if a.remoteBusy {
				return nil, errors.New("remote access operation in progress")
			}
			a.regenerateRemoteToken()
			return map[string]any{"accepted": true}, nil
		case "desktop.remote.save":
			if a.remoteBusy {
				return nil, errors.New("remote access operation in progress")
			}
			cfg := a.remoteConfigSnapshot()
			if err := nativeMerge(p, &cfg); err != nil {
				return nil, err
			}
			if cfg.Bind != "127.0.0.1" && cfg.Bind != "0.0.0.0" {
				return nil, errors.New("choose localhost or LAN listen address")
			}
			a.remoteEnabled, a.remoteBindIdx, a.remotePortDraft, a.remoteToken = cfg.Enabled, remoteBindIndex(cfg.Bind), strconv.Itoa(cfg.Port), cfg.Token
			a.remoteError = ""
			a.requestRemoteSettings()
			if a.remoteError != "" {
				return nil, errors.New(a.remoteError)
			}
			return map[string]any{"accepted": true}, nil
		case "desktop.service.install", "desktop.service.restart", "desktop.service.uninstall":
			if a.k == nil {
				return nil, errors.New("kernel unavailable")
			}
			if busyNetwork(a) {
				return nil, errors.New(tr("网络操作进行中，请稍后重试"))
			}
			a.changeService(strings.TrimPrefix(method, "desktop.service."))
			_, busy, e := a.svcState()
			if e != "" && !busy {
				return nil, errors.New(e)
			}
			return map[string]any{"accepted": busy}, nil
		case "desktop.update.check", "desktop.update.install", "desktop.update.cancel", "desktop.update.restart", "desktop.update.hide", "desktop.update.show":
			if a.softwareUpdates == nil {
				return nil, errors.New("application updater unavailable")
			}
			switch method {
			case "desktop.update.hide":
				a.softwareUpdates.Hide()
			case "desktop.update.show":
				a.softwareUpdates.Show()
			case "desktop.update.check":
				a.softwareUpdates.Check(true)
			case "desktop.update.install":
				a.softwareUpdates.Install()
			case "desktop.update.restart":
				if a.softwareUpdates.Snapshot().Phase != "ready" {
					return nil, errors.New("application update is not ready")
				}
				a.requestQuit(true)
			default:
				a.softwareUpdates.Cancel()
			}
			return nativeJSON(a.softwareUpdates.Snapshot()), nil
		case "desktop.diagnostics.start":
			a.diagURL = nativeString(p, "url")
			if a.diagURL == "" {
				a.diagURL = a.Prefs().TestURL
			}
			a.startDiagnostics()
			if a.diagError != "" {
				return nil, errors.New(a.diagError)
			}
			return map[string]any{"accepted": true}, nil
		case "desktop.diagnostics.report":
			steps, _ := a.diagnosticSnapshot()
			return map[string]any{"text": diagnosticReport(steps)}, nil
		case "desktop.diagnostics.cancel":
			a.cancelDiagnostics()
			return nil, nil
		case "desktop.backup.export":
			if !a.beginProfileWork() {
				return nil, errors.New(tr("订阅正在更新，请稍后重试"))
			}
			defer a.endProfileWork()
			s, err := a.backupSnapshot()
			if err != nil {
				return nil, err
			}
			return nil, backup.Write(nativeString(p, "path"), s)
		case "desktop.backup.inspect", "desktop.backup.restore":
			if busyNetwork(a) {
				return nil, errors.New(tr("网络操作进行中，请稍后重试"))
			}
			s, err := backup.Read(nativeString(p, "path"))
			if err != nil {
				return nil, err
			}
			_, _, count, err := a.prepareRestore(s)
			if err != nil {
				return nil, err
			}
			if method == "desktop.backup.inspect" {
				var index struct {
					Profiles []profile.Profile `json:"profiles"`
				}
				_ = json.Unmarshal(s.Files["profiles.json"], &index)
				items := []any{}
				for _, item := range index.Profiles {
					items = append(items, map[string]any{"id": item.ID, "name": item.Name, "kind": item.Kind})
				}
				return map[string]any{"version": s.Version, "created": s.Created, "profiles": items, "count": count}, nil
			}
			return nil, a.restoreBackup(s)
		default:
			return nil, fmt.Errorf("unsupported desktop method: %s", method)
		}
	})
}

func (a *App) nativeSaveOverrides(ctx context.Context, method string, p map[string]json.RawMessage) (any, error) {
	result := make(chan error, 1)
	_, err := nativeMain(ctx, func() (any, error) {
		id := nativeString(p, "id")
		next, local := a.ov, a.profileOverrides(nativeString(p, "id"))
		if id != "" {
			if a.store == nil {
				return nil, errors.New("profile store unavailable")
			}
			if _, err := a.store.Get(id); err != nil {
				return nil, err
			}
		}
		switch method {
		case "desktop.rules.save":
			if id == "" {
				if err := nativeMerge(p, &next, "extraRules", "mergeYaml", "appRules", "templateIds"); err != nil {
					return nil, err
				}
			} else {
				if _, ok := p["appRules"]; ok {
					return nil, errors.New("application rules have global scope")
				}
				if err := nativeMerge(p, &local, "extraRules", "mergeYaml", "templateIds"); err != nil {
					return nil, err
				}
			}
		case "desktop.proxies.save":
			if id == "" {
				return nil, errors.New("choose a profile before editing its proxies")
			}
			if err := json.Unmarshal(p["edits"], &local.ProxyEdits); err != nil {
				return nil, err
			}
		default:
			if method == "desktop.features.policy" {
				key := "dnsPolicy"
				if nativeString(p, "kind") == "sniffer" {
					key = "snifferPolicy"
				} else if nativeString(p, "kind") != "dns" {
					return nil, errors.New("unknown feature")
				}
				p[key] = p["enabled"]
			}
			if id != "" {
				if _, ok := p["sniffer"]; ok {
					return nil, errors.New("sniffer settings have global scope")
				}
				if _, ok := p["snifferPolicy"]; ok {
					return nil, errors.New("sniffer policy has global scope")
				}
			}
			if id == "" {
				if err := nativeMerge(p, &next, "dns", "sniffer"); err != nil {
					return nil, err
				}
			} else {
				if err := nativeMerge(p, &local, "dns"); err != nil {
					return nil, err
				}
			}
			if _, ok := p["dns"]; ok {
				if _, explicit := p["dnsPolicy"]; !explicit {
					if id == "" {
						next.DNSEnable = nil
					} else {
						local.DNSEnable = nil
					}
				}
			}
			if _, ok := p["sniffer"]; ok {
				if _, explicit := p["snifferPolicy"]; !explicit {
					next.SnifferEnable = nil
				}
			}

			if raw, ok := p["dnsPolicy"]; ok {
				var policy *bool
				if err := json.Unmarshal(raw, &policy); err != nil {
					return nil, err
				}
				if id == "" {
					next.DNSEnable = policy
				} else {
					local.DNSEnable = policy
				}
			}
			if raw, ok := p["snifferPolicy"]; ok {
				if err := json.Unmarshal(raw, &next.SnifferEnable); err != nil {
					return nil, err
				}
			}
			if next.DNS != nil {
				if err := next.DNS.Validate(); err != nil {
					return nil, err
				}
			}
			if local.DNS != nil {
				if err := local.DNS.Validate(); err != nil {
					return nil, err
				}
			}
			if next.Sniffer != nil {
				if err := next.Sniffer.Validate(); err != nil {
					return nil, err
				}
			}
		}
		a.requestOverrideChange(id, next, local, func(_ bool, e error) { result <- e })
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	select {
	case err := <-result:
		return map[string]any{"saved": err == nil}, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (a *App) nativeSaveNetwork(ctx context.Context, p map[string]json.RawMessage) (any, error) {
	var work networkSettingsChange
	_, err := nativeMain(ctx, func() (any, error) {
		if !a.beginOverrideChange() {
			return nil, errors.New(tr("网络操作进行中，请稍后重试"))
		}
		next := a.ov
		if err := nativeMerge(p, &next, "mixedPort", "allowLan", "ipv6", "unified", "tunStack", "tunAutoRoute"); err != nil {
			a.endOverrideChange()
			return nil, err
		}
		a.resetNetworkDraft()
		a.mixedPortDraft, a.lanOn, a.ipv6On, a.unifiedOn, a.tunStackPick = strconv.Itoa(next.MixedPort), next.AllowLAN, next.IPv6, next.Unified, next.TunStack
		var err error
		work, err = a.prepareNetworkSettings()
		if err != nil {
			a.endOverrideChange()
			return nil, err
		}
		work.next.TunAutoRout = next.TunAutoRout
		work.nextRaw, err = a.configBytesFor(work.next)
		if err != nil {
			a.endOverrideChange()
			return nil, err
		}
		a.networkGeneration.Add(1)
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	r := work.run()
	return nativeMain(context.WithoutCancel(ctx), func() (any, error) {
		defer a.endOverrideChange()
		a.commitNetworkSettings(work, r)
		return map[string]any{"saved": r.err == nil}, r.err
	})
}

// Build a worker model from immutable snapshots. It shares only the locked
// profile store and kernel client; private editor/cache fields stay isolated.
// The real model's busy gate excludes imports, auto refreshes and switches for
// the whole transaction. The final cache/preference publication runs on main.
func (a *App) nativeModelJob(ctx context.Context, fn func(*App) (any, error), publish func(*App)) (any, error) {
	var worker *App
	_, err := nativeMain(ctx, func() (any, error) {
		if a.store == nil {
			return nil, errors.New("profile store unavailable")
		}
		if !a.beginOverrideChange() {
			return nil, errors.New(tr("网络操作进行中，请稍后重试"))
		}
		a.networkGeneration.Add(1)
		worker = &App{k: a.k, mgr: a.mgr, store: a.store, ov: a.runtimeOverrides(), ovPath: a.ovPath, cfgText: a.cfgText, cfgPath: a.cfgPath, dir: a.dir, modeToken: a.modeToken}
		worker.meshClient = a.meshClient
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	value, workErr := fn(worker)
	_, err = nativeMain(context.WithoutCancel(ctx), func() (any, error) {
		defer a.endOverrideChange()
		a.featureCacheAt = time.Time{}
		a.cancelDelayTest()
		a.proxyGroup = ""
		if workErr == nil {
			if publish != nil {
				publish(worker)
			}
			a.notice = ""
		} else {
			a.setSubErr(workErr.Error())
		}
		return nil, nil
	})
	return value, errors.Join(workErr, err)
}

func (a *App) nativeProfileCall(ctx context.Context, method string, p map[string]json.RawMessage) (any, error) {
	id := nativeString(p, "id")
	if method == "desktop.profiles.aggregatePreview" {
		var spec profile.AggregateSpec
		if err := json.Unmarshal(p["spec"], &spec); err != nil {
			return nil, err
		}
		return nativeMain(ctx, func() (any, error) {
			if a.store == nil {
				return nil, errors.New("profile store unavailable")
			}
			raw, stats, err := a.store.BuildAggregate(spec)
			return map[string]any{"yaml": string(raw), "stats": nativeJSON(stats)}, err
		})
	}
	return a.nativeModelJob(ctx, func(w *App) (any, error) {
		switch method {
		case "desktop.profiles.import":
			if address := nativeString(p, "url"); address != "" {
				if err := profile.ValidateSettings(profile.Profile{Name: "import", URL: address, UserAgent: nativeString(p, "userAgent")}); err != nil {
					return nil, err
				}
			}
			item, err := w.importManagedProfileContext(ctx, func(accept func(profile.Profile, []byte) error) (profile.Profile, error) {
				switch {
				case nativeString(p, "url") != "":
					return workflow.Import(ctx, w.store, workflow.ImportSource{Name: nativeString(p, "name"), URL: nativeString(p, "url")}, accept)
				case nativeString(p, "path") != "":
					return workflow.Import(ctx, w.store, workflow.ImportSource{Name: nativeString(p, "name"), Path: nativeString(p, "path")}, accept)
				case p["content"] != nil:
					return workflow.Import(ctx, w.store, workflow.ImportSource{Name: nativeString(p, "name"), YAML: nativeString(p, "content")}, accept)
				default:
					return profile.Profile{}, errors.New("provide subscription URL, local path or YAML content")
				}
			})
			// Source metadata, like the existing subscription editor, applies
			// to the next refresh. A metadata write failure must not roll the
			// accepted core back behind an already committed source cache.
			if err == nil && nativeString(p, "url") != "" && (p["userAgent"] != nil || p["useProxy"] != nil) {
				if p["userAgent"] != nil {
					item.UserAgent = nativeString(p, "userAgent")
				}
				if p["useProxy"] != nil {
					item.UseProxy = nativeBool(p, "useProxy")
				}
				err = w.store.Edit(item)
			}
			return item, err

		case "desktop.profiles.update":
			return nil, w.refreshManagedProfileContext(ctx, id)
		case "desktop.profiles.updateAll":
			var failures []error
			for _, item := range w.store.List() {
				if !item.IsAggregate() {
					if err := w.refreshManagedProfileContext(ctx, item.ID); err != nil {
						failures = append(failures, fmt.Errorf("%s: %w", item.Name, err))
					}
				}
			}
			return nil, errors.Join(failures...)
		case "desktop.profiles.edit":
			var item profile.Profile
			if err := json.Unmarshal(p["profile"], &item); err != nil {
				return nil, err
			}
			return nil, w.store.Edit(item)
		case "desktop.profiles.delete":
			if w.store.ActiveID() == id {
				return nil, errors.New(tr("正在使用的订阅不能直接删除，请先启用其他配置"))
			}
			return nil, w.store.RemoveContext(ctx, id)
		case "desktop.profiles.activate":
			return nil, w.activateManagedProfile(ctx, id)
		case "desktop.profiles.aggregate":
			var spec profile.AggregateSpec
			if err := json.Unmarshal(p["spec"], &spec); err != nil {
				return nil, err
			}
			active := id != "" && w.store.ActiveID() == id
			old, err := w.lastAcceptedConfig()
			if err != nil && active {
				return nil, err
			}
			running := active && w.k != nil && w.k.RunningForChange()
			attempted := false
			item, stats, err := w.store.SaveAggregate(id, nativeString(p, "name"), spec, func(raw []byte) error {
				candidate, err := patch.ApplyLayers(raw, w.runtimeOverrides(), w.profileOverrides(id))
				if err != nil {
					return err
				}
				if running {
					attempted = true
					return w.k.Start(candidate)
				}
				return nil
			})
			if err != nil && attempted {
				err = errors.Join(err, w.k.Start(old))
			}
			return map[string]any{"profile": item, "stats": nativeJSON(stats)}, err
		default:
			return nil, errors.New("unknown profile operation")
		}
	}, nil)
}

func (a *App) nativeProxyEditor(id string) (any, error) {
	if a.store == nil {
		return nil, errors.New("profile store unavailable")
	}
	if id == "" {
		id = a.store.ActiveID()
	}
	if _, err := a.store.Get(id); err != nil {
		return nil, err
	}
	w := &App{store: a.store, ov: a.ov, cfgText: a.cfgText}
	w.openProfileProxyEditor(id)
	if !w.showProxyEditor {
		return nil, errors.New("cannot load proxy editor: " + w.notice)
	}
	nodes, groups := []any{}, []any{}
	options := []string{"DIRECT", "REJECT"}
	for _, key := range w.proxyEditorNodeOrder {
		f := w.proxyEditorNodes[key]
		opts, err := f.options()
		if err != nil {
			return nil, err
		}
		port, _ := strconv.Atoi(f.Port)
		base := w.proxyEditorBaseNodes[f.Source]
		nodes = append(nodes, map[string]any{"source": f.Source, "localId": f.LocalID, "name": f.Name, "type": f.Type, "server": f.Server, "port": port, "options": opts, "missing": f.Missing, "base": nativeJSON(base)})
		options = append(options, f.Name)
	}
	for _, key := range w.proxyEditorGroupOrder {
		f := w.proxyEditorGroups[key]
		base := w.proxyEditorBaseGroups[f.Source]
		groups = append(groups, map[string]any{"source": f.Source, "name": f.Name, "type": f.Type, "members": f.Members, "missing": f.Missing, "deleted": f.Deleted, "base": nativeJSON(base)})
		if !f.Deleted {
			options = append(options, f.Name)
		}
	}
	return map[string]any{"id": id, "nodes": nodes, "groups": groups, "edits": a.profileOverrides(id).ProxyEdits, "options": options}, nil
}
