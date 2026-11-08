package engine

import (
	"context"
	"encoding/json"
	"strings"

	"gopurr/internal/profile"
	"gopurr/internal/workflow"
)

// profileNetworkCall reserves only the profile writer while HTTP runs. Status,
// traffic and disconnect do not wait for a server to deliver its response.
func (e *Engine) profileNetworkCall(ctx context.Context, method string, params json.RawMessage) (any, error) {
	var request struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		URL  string `json:"url"`
		YAML string `json:"yaml"`
	}
	if err := decodeParams(params, &request); err != nil {
		return nil, err
	}
	if method == "profiles.import" {
		if (request.URL == "") == (request.YAML == "") {
			return nil, failure("invalid_params", "请提供订阅链接或 YAML 内容其中一项")
		}
		if len(request.Name) > 256 || len(request.YAML) > MaxDocumentBytes {
			return nil, failure("invalid_params", "配置名称过长或 YAML 超过 16 MiB")
		}
		if request.URL != "" {
			if _, err := httpURL(request.URL); err != nil {
				return nil, err
			}
		}
	} else if request.ID == "" {
		return nil, failure("invalid_params", "请选择需要更新的配置")
	}
	if !e.profileMu.TryLock() {
		return nil, failure("busy", "订阅正在更新，请稍后重试")
	}
	defer e.profileMu.Unlock()
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil, failure("closed", "内核已关闭")
	}
	if err := ctx.Err(); err != nil {
		e.mu.Unlock()
		return nil, err
	}
	e.operationID++
	operationID := e.operationID
	e.operation = &Operation{Method: method, ProfileID: request.ID, Phase: "downloading"}
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		if e.operationID == operationID {
			e.operation = nil
		}
		e.mu.Unlock()
	}()

	var revision string
	prepare := func(ctx context.Context, item profile.Profile, raw []byte) (workflow.ProfilePlan, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.closed {
			return workflow.ProfilePlan{}, failure("closed", "内核已关闭")
		}
		if err := ctx.Err(); err != nil {
			return workflow.ProfilePlan{}, err
		}
		if e.operationID != operationID {
			return workflow.ProfilePlan{}, context.Canceled
		}
		e.operation.ProfileID = item.ID
		e.operation.Phase = "validating"
		managed, err := e.prepareLocked(raw, item)
		if err != nil {
			return workflow.ProfilePlan{}, err
		}
		revision = profileRevision(item, raw)
		e.operation.Phase = "saving"
		return workflow.ProfilePlan{Config: managed, Apply: false}, nil
	}
	change := workflow.ProfileChange{Prepare: prepare}
	if method == "profiles.import" {
		change.Persist = func(accept func(profile.Profile, []byte) error) (profile.Profile, error) {
			return workflow.Import(ctx, e.store, workflow.ImportSource{Name: request.Name, URL: request.URL, YAML: request.YAML}, accept)
		}
	} else {
		change.Persist = func(accept func(profile.Profile, []byte) error) (profile.Profile, error) {
			item, err := e.store.Get(request.ID)
			if err != nil {
				return profile.Profile{}, err
			}
			err = workflow.Refresh(ctx, e.store, request.ID, func(raw []byte) error { return accept(item, raw) })
			if err != nil {
				return profile.Profile{}, err
			}
			return e.store.Get(request.ID)
		}
	}
	result, err := workflow.RunProfileChange(ctx, change)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if result.Profile.ID == e.store.ActiveID() {
		e.pendingPlan = nil
		if e.core != nil && e.core.Running() {
			e.reconnectRequired = e.runtimeRevision != "" && e.runtimeRevision != revision
		}
	}
	summary := profileSummary(result.Profile, e.store.ActiveID())
	summary.ReconnectRequired = e.reconnectRequired && summary.Active && e.core != nil && e.core.Running()
	if method == "profiles.import" {
		return summary, nil
	} // Preserve API v1's direct profile response.
	status := e.statusLocked()
	status.Busy = false
	status.Operation = nil
	return ProfileMutation{Profile: &summary, Saved: true, Applied: result.Applied, ReconnectRequired: summary.ReconnectRequired, Status: status}, nil
}

func (e *Engine) profileLocalCallLocked(ctx context.Context, method string, params json.RawMessage) (any, error) {
	var request struct {
		ID   string  `json:"id"`
		Name *string `json:"name"`
		URL  *string `json:"url"`
	}
	if err := decodeParams(params, &request); err != nil {
		return nil, err
	}
	if request.ID == "" {
		return nil, failure("invalid_params", "请选择配置")
	}
	item, err := e.store.Get(request.ID)
	if err != nil {
		return nil, err
	}
	if method == "profiles.editor" {
		return ProfileEditor{ID: item.ID, Name: item.Name, URL: item.URL, Kind: item.Kind}, nil
	}
	if !e.profileMu.TryLock() {
		return nil, failure("busy", "订阅正在更新，请稍后重试")
	}
	defer e.profileMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if method == "profiles.delete" {
		if item.ID == e.store.ActiveID() && e.core != nil && e.core.Running() {
			return nil, failure("profile_in_use", "请先断开连接，再删除正在使用的配置")
		}
		if err := e.store.RemoveContext(ctx, item.ID); err != nil {
			return nil, err
		}
		if _, remembered := e.manualSelections[item.ID]; remembered {
			delete(e.manualSelections, item.ID)
			e.savePrunedPreferencesLocked()
		}
		e.pendingPlan = nil
		return ProfileMutation{Saved: true, Applied: false, Status: e.statusLocked()}, nil
	}
	if request.Name != nil {
		if len(*request.Name) > 256 {
			return nil, failure("invalid_params", "配置名称过长")
		}
		item.Name = strings.TrimSpace(*request.Name)
	}
	if request.URL != nil {
		if item.IsFile() && *request.URL != "" {
			return nil, failure("invalid_params", "本地配置不能修改订阅链接")
		}
		if !item.IsFile() {
			if _, err := httpURL(*request.URL); err != nil {
				return nil, err
			}
			item.URL = *request.URL
		}
	}
	if err := e.store.Edit(item); err != nil {
		return nil, err
	}
	summary := profileSummary(item, e.store.ActiveID())
	summary.ReconnectRequired = summary.Active && e.reconnectRequired && e.core != nil && e.core.Running()
	return ProfileMutation{Profile: &summary, Saved: true, Applied: false, ReconnectRequired: summary.ReconnectRequired, Status: e.statusLocked()}, nil
}
