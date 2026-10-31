package app

import (
	"errors"
	"os"

	"gopurr/internal/patch"
)

// configForProfile is shared by editors so previews and validated saves use
// the same layers and ephemeral mesh routes as the running configuration.
func (a *App) configForProfile(id string, local patch.ProfileOverrides) ([]byte, error) {
	raw := []byte(a.cfgText)
	if id != "" {
		if a.store == nil {
			return nil, errors.New(tr("订阅目录不可用"))
		}
		var err error
		raw, err = a.store.Content(id)
		if err != nil {
			return nil, err
		}
	}
	if len(raw) == 0 {
		return nil, errors.New(tr("没有可用的配置"))
	}
	return patch.ApplyLayers(raw, a.runtimeOverrides(), local)
}

// A saved override can become stale after an external file change. Repairs
// must roll back to what the kernel last accepted, not reconstruct that now
// invalid source with the stale override being repaired.
func (a *App) lastAcceptedConfig() ([]byte, error) {
	if a.mgr != nil {
		body, err := os.ReadFile(a.mgr.ConfigPath())
		if err == nil && len(body) > 0 {
			return body, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return a.configBytes()
}

// The visual editor needs the subscription's original identities, even when
// saved app policies already refer to renamed or locally added groups.
func (a *App) proxyEditorBaseConfig(id string) ([]byte, error) {
	if a.store == nil {
		return nil, errors.New(tr("订阅目录不可用"))
	}
	raw, err := a.store.Content(id)
	if err != nil {
		return nil, err
	}
	global := a.runtimeOverrides()
	global.AppRules, global.ExtraRules = nil, nil
	if id != a.store.ActiveID() {
		global.TunEnable = false
	}
	local := a.profileOverrides(id)
	local.ProxyEdits, local.ExtraRules = nil, nil
	return patch.ProxyEditorBase(raw, global, local)
}
