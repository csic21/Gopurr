package app

import (
	"os"
	"path/filepath"
)

func (a *App) saveLocalConfig() {
	previous := a.cfgText
	a.cfgText = a.rawDraft
	if _, err := a.configBytes(); err != nil {
		a.cfgText = previous
		a.rawError = "配置验证失败: " + err.Error()
		return
	}
	running := a.k != nil && a.k.Running()
	if running && !a.startKernel() {
		a.cfgText = previous
		a.rawError = a.notice
		return
	}
	if err := writeLocalConfig(a.cfgPath, []byte(a.cfgText)); err != nil {
		a.cfgText = previous
		if running {
			a.startKernel()
		}
		a.rawError = "保存失败: " + err.Error()
		return
	}
	a.rawError, a.showRaw = "", false
	a.notice = "本地配置已保存"
	if running {
		a.notice += "并应用"
	}
}

func writeLocalConfig(path string, raw []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".local-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(raw); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
