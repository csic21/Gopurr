package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"gopurr/internal/patch"
	"gopurr/internal/workflow"
)

// A transaction owns immutable inputs. Only its completion touches UI state;
// validation, kernel reload, persistence and rollback run off the event loop.
type overrideChange struct {
	raw                []byte
	previous, next     patch.Overrides
	local              patch.ProfileOverrides
	previousLocal      patch.ProfileOverrides
	path, acceptedPath string
	active             bool
	running            func() bool
	start              func([]byte) error
	persist            func() error
}

func (w overrideChange) run() (bool, error) {
	candidate, err := patch.ApplyLayers(w.raw, w.next, w.local)
	if err != nil {
		return false, fmt.Errorf(tr("配置验证失败：%w"), err)
	}
	running := w.active && w.running != nil && w.running()
	var previous []byte
	if running {
		if w.acceptedPath != "" {
			previous, err = os.ReadFile(w.acceptedPath)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return false, err
			}
		}
		if len(previous) == 0 {
			previous, err = patch.ApplyLayers(w.raw, w.previous, w.previousLocal)
			if err != nil {
				return false, err
			}
		}
	}
	change := workflow.ConfigChange{Candidate: candidate, Commit: func(context.Context) error {
		if err := w.persist(); err != nil {
			return fmt.Errorf(tr("保存失败，已保留原配置：%w"), err)
		}
		return nil
	}}
	if running {
		change.Apply = func(_ context.Context, raw []byte) error {
			if err := w.start(raw); err != nil {
				return fmt.Errorf(tr("内核未接受配置，未保存：%s"), err)
			}
			return nil
		}
		change.Restore = func(context.Context) error {
			if err := w.start(previous); err != nil {
				return fmt.Errorf(tr("恢复原配置失败：%w"), err)
			}
			return nil
		}
	}
	result, err := workflow.RunConfigChange(context.Background(), change)
	return result.Applied, err
}

func (a *App) prepareOverrideChange(id string, global patch.Overrides, local patch.ProfileOverrides) (overrideChange, error) {
	w := overrideChange{raw: []byte(a.cfgText), previous: a.runtimeOverrides(), next: global, local: local, path: a.ovPath, active: true}
	w.next.MeshIPs, w.next.MeshProxyAddress, w.next.MeshProxyPassword = w.previous.MeshIPs, w.previous.MeshProxyAddress, w.previous.MeshProxyPassword
	activeID := ""
	if a.store != nil {
		activeID = a.store.ActiveID()
	}
	if id != "" && a.store == nil {
		return w, errors.New(tr("请先选择订阅"))
	}
	sourceID := id
	if sourceID == "" {
		sourceID = activeID
	}
	if sourceID != "" {
		var err error
		w.raw, err = a.store.Content(sourceID)
		if err != nil {
			return w, err
		}
		w.previousLocal = a.profileOverrides(sourceID)
	}
	if id == "" {
		w.local = w.previousLocal
		w.persist = func() error { return writeSettingsFile(w.path, global) }
	} else {
		store := a.store
		if store == nil {
			return w, errors.New(tr("请先选择订阅"))
		}
		w.active = activeID == id
		if !w.active {
			w.next.ExtraRules, w.next.AppRules, w.next.TunEnable = nil, nil, false
		}
		w.persist = func() error { return store.SetOverrides(id, local) }
	}
	if a.k != nil {
		w.running, w.start = a.k.RunningForChange, a.k.Start
	}
	if a.mgr != nil {
		w.acceptedPath = a.mgr.ConfigPath()
	}
	return w, nil
}

// Reserve the same busy gates used by proxy/network work and profile writes.
// This prevents a late save from overwriting an intervening switch or import.
func (a *App) beginOverrideChange() bool {
	a.bmu.Lock()
	defer a.bmu.Unlock()
	if a.subBusy || a.sysBusy || a.kernelBusy || a.svcBusy || a.tunChanging || a.proxySelecting || a.proxySelectionActive.Load() > 0 || a.shutdownPending.Load() {
		return false
	}
	a.subBusy, a.sysBusy = true, true
	return true
}
func (a *App) endOverrideChange() {
	a.bmu.Lock()
	a.subBusy, a.sysBusy = false, false
	a.bmu.Unlock()
}
func (a *App) finishOverrideChange(id string, next patch.Overrides, applied bool) {
	if id == "" {
		a.ov = next
	}
	a.featureCacheAt = time.Time{}
	if applied {
		a.cancelDelayTest()
		a.proxyGroup = ""
	}
}
func (a *App) requestOverrideChange(id string, next patch.Overrides, local patch.ProfileOverrides, done func(bool, error)) {
	if !a.beginOverrideChange() {
		done(false, errors.New(tr("网络操作进行中，请稍后重试")))
		return
	}
	w, err := a.prepareOverrideChange(id, next, local)
	if err != nil {
		a.endOverrideChange()
		done(false, err)
		return
	}
	a.networkGeneration.Add(1)
	a.invalidate()
	go func() {
		applied, err := w.run()
		runOnMain(func() {
			if err == nil {
				a.finishOverrideChange(id, next, applied)
			}
			done(applied, err)
			a.endOverrideChange()
			a.invalidate()
		})
	}()
}

// Synchronous entry for callers without an event loop and transaction tests.
func (a *App) commitOverrideChange(id string, next patch.Overrides, local patch.ProfileOverrides) (bool, error) {
	if !a.beginOverrideChange() {
		return false, errors.New(tr("网络操作进行中，请稍后重试"))
	}
	defer a.endOverrideChange()
	w, err := a.prepareOverrideChange(id, next, local)
	if err != nil {
		return false, err
	}
	applied, err := w.run()
	if err == nil {
		a.finishOverrideChange(id, next, applied)
	}
	return applied, err
}

// Scope and draft are captured at submission, not read from a worker.
func (a *App) requestExtensionDraft(yaml bool) {
	id, draft := "", a.extraRulesDraft
	if a.extensionScope == 1 {
		id = a.extensionProfileID
	}
	next, local := a.ov, a.profileOverrides(id)
	if yaml {
		draft = a.mergeYAMLDraft
		next.MergeYAML, local.MergeYAML = draft, draft
		a.mergeYAMLError, a.mergeYAMLStatus = "", ""
	} else {
		next.ExtraRules, local.ExtraRules = extraRuleLines(draft), extraRuleLines(draft)
		a.extraRulesError, a.extraRulesStatus = "", ""
	}
	// A scoped transaction must not publish the candidate global copy.
	if id != "" {
		next = a.ov
	}
	a.requestOverrideChange(id, next, local, func(applied bool, err error) {
		if id != a.extensionDraftKey {
			return
		}
		if yaml {
			if err != nil {
				a.mergeYAMLError = err.Error()
				return
			}
			a.mergeYAMLSaved, a.mergeYAMLStatus = draft, a.scopedSavedStatus(applied)
		} else {
			if err != nil {
				a.extraRulesError = err.Error()
				return
			}
			a.extraRulesSaved, a.extraRulesStatus = draft, a.scopedSavedStatus(applied)
		}
	})
}
