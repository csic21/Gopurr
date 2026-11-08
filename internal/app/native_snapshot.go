package app

// The desktop bridge deliberately lives beside the original model. Native
// clients share its accepted configuration, transactional writes and recovery
// policy rather than maintaining a second desktop engine or preference store.
import (
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"unicode"

	"gopurr/internal/buildinfo"
	"gopurr/internal/patch"
	"gopurr/internal/profile"
	"gopurr/internal/remote"
)

// nativeMain completes a model operation on the main thread. The completion
// channel also supports asynchronous dispatchers used by isolated bridge tests.
func nativeMain(ctx context.Context, fn func() (any, error)) (any, error) {
	type response struct {
		value any
		err   error
	}
	done := make(chan response, 1)
	runOnMain(func() {
		if err := ctx.Err(); err != nil {
			done <- response{err: err}
			return
		}
		v, e := fn()
		done <- response{v, e}
	})
	// Once dispatch committed its result, a simultaneous deadline must not
	// discard it: callers may have reserved a transaction's busy gate.
	select {
	case r := <-done:
		return r.value, r.err
	default:
	}
	select {
	case r := <-done:
		return r.value, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// nativeJSON normalizes structs lacking JSON tags (mesh, update, diagnostic,
// core), while preserving already tagged keys and arbitrary map keys.
func nativeJSON(v any) any { return nativeValue(reflect.ValueOf(v)) }
func nativeValue(v reflect.Value) any {
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return nativeValue(v.Elem())
	}
	if v.CanInterface() {
		if _, ok := v.Interface().(json.Marshaler); ok {
			return v.Interface()
		}
		// encoding/json also uses MarshalText for values such as netip.Addr.
		// Reflecting their private fields would turn device IPs into {}.
		if _, ok := v.Interface().(encoding.TextMarshaler); ok {
			return v.Interface()
		}
	}
	switch v.Kind() {
	case reflect.Struct:
		out := map[string]any{}
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "-" {
				continue
			}
			if tag == "" {
				tag = nativeKey(f.Name)
			}
			out[tag] = nativeValue(v.Field(i))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return nil
		}
		out := map[string]any{}
		it := v.MapRange()
		for it.Next() {
			if it.Key().Kind() == reflect.String {
				out[it.Key().String()] = nativeValue(it.Value())
			}
		}
		return out
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil
		}
		out := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			out[i] = nativeValue(v.Index(i))
		}
		return out
	default:
		if v.CanInterface() {
			return v.Interface()
		}
		return nil
	}
}
func nativeKey(s string) string {
	if s == "" || !unicode.IsUpper(rune(s[0])) {
		return s
	}
	for _, a := range []string{"DNS", "URL", "SSH", "RDP", "VNC", "TCP", "UDP", "IPv6", "IPs", "IP", "OS", "ID"} {
		if strings.HasPrefix(s, a) {
			return strings.ToLower(a) + s[len(a):]
		}
	}
	rs := []rune(s)
	n := 1
	for n < len(rs) && unicode.IsUpper(rs[n]) {
		if n+1 < len(rs) && unicode.IsLower(rs[n+1]) {
			break
		}
		n++
	}
	return strings.ToLower(string(rs[:n])) + string(rs[n:])
}
func nativeError(err error) string {
	if err != nil {
		return err.Error()
	}
	return ""
}

// NativeSnapshot returns only the heavyweight collection for the visible page.
// Common navigation, network controls and preferences are present on every page.
func (a *App) NativeSnapshot(ctx context.Context, page, scopeID string) (map[string]any, error) {
	if a == nil {
		return nil, errors.New("desktop model is unavailable")
	}
	var featureWork bool
	var signature featureSignature
	var featureGlobal patch.Overrides
	var featureLocal patch.ProfileOverrides
	var featureSource []byte
	var featureStore *profile.Store
	var featureID string
	var cacheFeatures bool
	v, err := nativeMain(ctx, func() (any, error) {
		status := remote.Status{Version: buildinfo.Version, Mode: a.modeToken, SystemProxy: a.sysApplied, Tun: a.tunOn, Notice: a.notice, Busy: busyNetwork(a), SystemProxyAvailable: systemProxyAvailable()}
		status.Error = a.sysErrText()
		if status.Error == "" {
			status.Error = a.tunErrText()
		}
		if status.Error == "" {
			status.Error = a.initErr
		}
		installed, busy, serviceErr := a.svcState()
		status.TunInstalled = installed
		out := map[string]any{"apiVersion": 2, "desktop": true, "page": a.page, "status": status, "prefs": a.Prefs(), "network": a.ov, "service": map[string]any{"installed": installed, "busy": busy, "error": serviceErr, "needsUpdate": false}, "capabilities": map[string]any{"desktop": true, "systemProxy": systemProxyAvailable(), "tun": a.k != nil && a.k.IsRoot()}}
		if a.mgr != nil {
			out["service"].(map[string]any)["needsUpdate"] = a.mgr.ServiceNeedsUpdate() || a.mgr.ServiceVersionSkew()
		}
		if a.softwareUpdates != nil {
			out["update"] = nativeJSON(a.softwareUpdates.Snapshot())
		}
		profiles := []any{}
		active := ""
		if a.store != nil {
			active = a.store.ActiveID()
			for _, p := range a.store.List() {
				source := p.Kind
				if p.IsFile() && p.Path != "" {
					source = filepath.Base(p.Path)
				}
				if u, e := url.Parse(p.URL); e == nil && u.Hostname() != "" {
					source = u.Hostname()
				}
				profiles = append(profiles, map[string]any{"id": p.ID, "name": p.Name, "kind": p.Kind, "source": source, "updated": p.Updated, "info": p.Info, "updateHours": p.UpdateHours, "useProxy": p.UseProxy, "active": p.ID == active})
			}
		}
		out["profiles"], out["activeProfile"] = profiles, active
		signature = a.featureSignature()
		dns, sniff, featureErr := a.featureDNS, a.featureSniffer, a.featureCacheError
		featureWork = a.featureCacheAt.IsZero() || signature != a.featureKey
		cacheFeatures = scopeID == "" || scopeID == active
		if !cacheFeatures {
			featureWork = true
		}
		featureID = active
		if scopeID != "" {
			featureID = scopeID
		}
		if featureWork {
			featureGlobal = a.runtimeOverrides()
			featureLocal = a.profileOverrides(featureID)
			featureSource = []byte(a.cfgText)
			featureStore = a.store
			if featureID != "" && featureID != active {
				featureGlobal.ExtraRules, featureGlobal.AppRules, featureGlobal.TunEnable = nil, nil, false
			}
		}

		dnsPolicy := a.ov.DNSEnable
		if featureID != "" {
			if local := a.profileOverrides(featureID); local.DNSEnable != nil {
				dnsPolicy = local.DNSEnable
			}
		}
		out["features"] = map[string]any{"dns": dns, "sniffer": sniff, "dnsPolicy": dnsPolicy, "snifferPolicy": a.ov.SnifferEnable, "error": nativeError(featureErr)}
		_, hist := a.snapshot()
		history := []any{}
		for _, s := range hist {
			history = append(history, map[string]any{"up": s.up, "down": s.down})
		}
		out["history"] = history
		out["health"] = map[string]any{"runtime": nativeJSON(a.networkRuntime), "checked": a.networkChecked, "error": a.networkErr, "proxyError": a.proxyReadErr, "proxyChanged": a.proxyChanged}
		if page == "rules" || page == "settings" || page == "proxies" || page == "" {
			scope := map[string]any{"id": scopeID, "extraRules": a.ov.ExtraRules, "mergeYaml": a.ov.MergeYAML, "dns": a.ov.DNS, "dnsEnable": a.ov.DNSEnable, "templateIds": a.ov.TemplateIDs, "proxyEdits": nil}
			if scopeID != "" {
				if a.store == nil {
					return nil, errors.New("profile store is unavailable")
				}
				if _, e := a.store.Get(scopeID); e != nil {
					return nil, e
				}
				l := a.profileOverrides(scopeID)
				scope = map[string]any{"id": scopeID, "extraRules": l.ExtraRules, "mergeYaml": l.MergeYAML, "dns": l.DNS, "dnsEnable": l.DNSEnable, "templateIds": l.TemplateIDs, "proxyEdits": l.ProxyEdits}
			}
			out["scope"], out["library"], out["appRules"] = scope, a.ov.Library, a.ov.AppRules
		}
		if page == "mesh" || page == "" {
			if a.meshClient != nil {
				s := a.meshClient.Snapshot()
				s.Status.ProxyPassword = ""
				s.Status.ProxyAddress = ""
				out["mesh"] = nativeJSON(s)
			}
			a.loadMeshShortcuts()
			out["meshShortcuts"] = nativeJSON(a.meshShortcuts)
			out["meshAccess"] = map[string]any{"ready": a.meshAccessReady(), "error": a.meshRouteError, "preparing": a.meshPreparePending, "prepareError": a.meshPrepareError}
		}
		if page == "settings" || page == "" {
			steps, running := a.diagnosticSnapshot()
			out["diagnostics"] = map[string]any{"steps": nativeJSON(steps), "running": running, "url": a.diagURL, "error": a.diagError}
			cfg := a.remoteConfigSnapshot()
			cfg.Token = ""
			out["remote"] = map[string]any{"enabled": cfg.Enabled, "bind": cfg.Bind, "port": cfg.Port, "busy": a.remoteBusy, "error": a.remoteError, "urls": a.remoteAccessURLs(), "hasToken": a.remoteConfigSnapshot().Token != ""}
		}
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	out := v.(map[string]any)
	if featureWork {
		var compileErr error
		if featureID != "" {
			if featureStore == nil {
				compileErr = errors.New("profile store unavailable")
			} else {
				featureSource, compileErr = featureStore.Content(featureID)
			}
		}
		if compileErr == nil {
			featureSource, compileErr = patch.ApplyLayers(featureSource, featureGlobal, featureLocal)
		}
		dns, sniff := patch.DefaultDNSSettings(), patch.DefaultSnifferSettings()
		if compileErr == nil {
			dns, sniff, compileErr = patch.FeatureSettings(featureSource)
		}
		features := out["features"].(map[string]any)
		features["dns"], features["sniffer"], features["error"] = dns, sniff, nativeError(compileErr)
		if cacheFeatures {
			_, err = nativeMain(ctx, func() (any, error) {
				if signature == a.featureSignature() {
					a.cacheFeatures(signature, dns, sniff, compileErr)
				}
				return nil, nil
			})
			if err != nil {
				return nil, err
			}
		}
	}
	status := out["status"].(remote.Status)
	if a.k != nil {
		status.Kernel = a.k.Running()
		status.TunAvailable = status.Kernel && a.k.IsRoot()
		s := a.k.Sample()
		status.Traffic = remote.Traffic{Up: s.UpNow, Down: s.DownNow, UpTotal: s.UpTotal, DownTotal: s.DownTotal, Stale: s.TrafficStale}
		if !s.TrafficAt.IsZero() {
			status.Traffic.At = s.TrafficAt.UnixMilli()
		}
		status.Connections = s.Conns
		status.ConnectionsStale = s.ConnectionsStale
		if status.Kernel {
			status.Proxies, status.Rules, _ = a.k.Counts()
		}
	}
	out["status"] = status
	h := webHost{a}
	if page == "overview" || page == "proxies" || page == "rules" || page == "" {
		groups, e := h.Groups(ctx)
		if e != nil {
			return nil, e
		}
		out["groups"] = groups
	}
	if page == "conns" || page == "" {
		conns, e := h.Connections(ctx)
		out["connections"] = conns
		out["connectionsError"] = nativeError(e)
	}
	if page == "logs" || page == "" {
		logs, e := h.Logs(ctx)
		out["logs"] = logs
		out["logsError"] = nativeError(e)
	}
	if (page == "rules" || page == "") && a.k != nil {
		out["effectiveRules"] = nativeJSON(a.k.Rules())
	}
	// Freeze references before a queued main-thread mutation can modify them.
	body, e := json.Marshal(out)
	if e != nil {
		return nil, e
	}
	var detached map[string]any
	e = json.Unmarshal(body, &detached)
	return detached, e
}
