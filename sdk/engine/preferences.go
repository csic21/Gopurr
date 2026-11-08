package engine

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"gopurr/internal/securefs"
)

const preferencesFile = "mobile-runtime.json"

type runtimePreferences struct {
	Version    int                          `json:"version"`
	Mode       string                       `json:"mode"`
	Selections map[string]map[string]string `json:"selections,omitempty"`
}

func validMode(mode string) bool { return mode == "rule" || mode == "global" || mode == "direct" }

func (e *Engine) loadPreferences() error {
	path := filepath.Join(e.home, preferencesFile)
	if err := securefs.PrivateFile(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil // Upgrade from builds which kept choices only in memory.
		}
		return failure("preferences_failed", "无法读取手机连接偏好，请检查应用数据")
	}
	f, err := os.Open(path)
	if err != nil {
		return failure("preferences_failed", "无法读取手机连接偏好，请检查应用数据")
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, MaxDocumentBytes+1))
	var saved runtimePreferences
	if err != nil || len(raw) > MaxDocumentBytes || json.Unmarshal(raw, &saved) != nil || saved.Version != 1 || !validMode(saved.Mode) {
		return failure("preferences_failed", "手机连接偏好损坏或版本不兼容，原文件已保留")
	}
	// A crash between profile deletion and preference cleanup cannot revive a
	// removed subscription's choices. Profile IDs, not names, are the boundary.
	for id := range saved.Selections {
		if _, err := e.store.Get(id); err != nil {
			delete(saved.Selections, id)
		}
	}
	e.mode, e.manualSelections = saved.Mode, saved.Selections
	return nil
}

// Save before acknowledging a change; never report a transient choice as saved.
// securefs atomically replaces a private file without following destination links.
func (e *Engine) savePreferencesLocked(mode string, selections map[string]map[string]string) error {
	raw, err := json.Marshal(runtimePreferences{Version: 1, Mode: mode, Selections: selections})
	if err != nil || len(raw) > MaxDocumentBytes {
		return failure("preferences_failed", "手机连接偏好过大，未保存修改")
	}
	if err := securefs.WriteFile(filepath.Join(e.home, preferencesFile), raw, 0600); err != nil {
		return failure("preferences_failed", "无法保存手机连接偏好，未更改当前选择")
	}
	return nil
}

func cloneSelections(source map[string]map[string]string) map[string]map[string]string {
	result := make(map[string]map[string]string, len(source))
	for id, groups := range source {
		result[id] = make(map[string]string, len(groups))
		for group, member := range groups {
			result[id][group] = member
		}
	}
	return result
}

// Cleanup follows an already committed profile/runtime change. Failure must not
// undo a successful connection or resurrect a deleted profile; surface a warning
// in Status instead. The next successful preference write retries the cleanup.
func (e *Engine) savePrunedPreferencesLocked() {
	if err := e.savePreferencesLocked(e.mode, e.manualSelections); err != nil {
		e.lastError = "连接偏好清理未能保存；当前连接不受影响，请检查应用存储空间"
	}
}
