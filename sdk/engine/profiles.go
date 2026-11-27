package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"

	"gopurr/internal/patch"
	"gopurr/internal/profile"
	"gopurr/internal/workflow"
)

// optionalInt distinguishes an omitted JSON field from an explicit null or integer,
// allowing profiles.update to clear UpdateHours back to nil (inherit global).
type optionalInt struct {
	Set   bool
	Value *int
}

func (o *optionalInt) UnmarshalJSON(data []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		o.Value = nil
		return nil
	}
	var v int
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

// profileNetworkCall reserves only the profile writer while HTTP runs. Status,
// traffic and disconnect do not wait for a server to deliver its response.
func (e *Engine) profileNetworkCall(ctx context.Context, method string, params json.RawMessage) (any, error) {
	var request struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		URL         string `json:"url"`
		YAML        string `json:"yaml"`
		UserAgent   string `json:"userAgent"`
		UseProxy    bool   `json:"useProxy"`
		UpdateHours *int   `json:"updateHours"`
		Activate    bool   `json:"activate"`
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
			if err := profile.ValidateSettings(profile.Profile{Name: "import", URL: request.URL, UserAgent: request.UserAgent, UpdateHours: request.UpdateHours}); err != nil {
				return nil, failure("invalid_params", err.Error())
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
			return workflow.Import(ctx, e.store, workflow.ImportSource{
				Name:        strings.TrimSpace(request.Name),
				URL:         request.URL,
				YAML:        request.YAML,
				UserAgent:   strings.TrimSpace(request.UserAgent),
				UseProxy:    request.UseProxy,
				UpdateHours: request.UpdateHours,
			}, accept)
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
	if method == "profiles.import" && request.Activate && result.Profile.ID != e.store.ActiveID() {
		// Safe activation only when the proxy tunnel is not actively running.
		if !e.proxyRunningLocked() && !(e.vpn && !e.vpnMeshOnly) {
			_ = e.store.SetActiveContext(ctx, result.Profile.ID)
		}
	}
	if result.Profile.ID == e.store.ActiveID() {
		e.pendingPlan = nil
		if e.proxyRunningLocked() {
			e.reconnectRequired = e.runtimeRevision != "" && e.runtimeRevision != revision
		}
	}
	summary := e.profileSummaryLocked(result.Profile, e.store.ActiveID())
	summary.ReconnectRequired = e.reconnectRequired && summary.Active && e.proxyRunningLocked()
	if method == "profiles.import" {
		return summary, nil
	} // Preserve API v1's direct profile response.
	status := e.statusLocked()
	status.Busy = false
	status.Operation = nil
	return ProfileMutation{Profile: &summary, Saved: true, Applied: result.Applied, ReconnectRequired: summary.ReconnectRequired, Status: status}, nil
}

func (e *Engine) createLocalProfileLocked(ctx context.Context, params json.RawMessage) (any, error) {
	var request struct {
		Name     string            `json:"name"`
		Nodes    []patch.ProxyNode `json:"nodes"`
		Activate bool              `json:"activate"`
	}
	if err := decodeParams(params, &request); err != nil {
		return nil, err
	}
	if len(request.Name) > 256 {
		return nil, failure("invalid_params", "配置名称过长")
	}
	if !e.profileMu.TryLock() {
		return nil, failure("busy", "订阅正在更新，请稍后重试")
	}
	defer e.profileMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	item, err := e.store.CreateLocal(ctx, request.Name, request.Nodes, func(candidate profile.Profile, raw []byte) error {
		_, err := e.prepareLocked(raw, candidate)
		return err
	})
	if err != nil {
		return nil, err
	}
	if request.Activate && item.ID != e.store.ActiveID() && !e.proxyRunningLocked() && !(e.vpn && !e.vpnMeshOnly) {
		if err := e.store.SetActiveContext(ctx, item.ID); err != nil {
			return nil, err
		}
	}
	if item.ID == e.store.ActiveID() {
		e.pendingPlan = nil
	}
	summary := e.profileSummaryLocked(item, e.store.ActiveID())
	summary.ReconnectRequired = summary.Active && e.reconnectRequired && e.proxyRunningLocked()
	return ProfileMutation{Profile: &summary, Saved: true, Applied: false, ReconnectRequired: summary.ReconnectRequired, Status: e.statusLocked()}, nil
}

func (e *Engine) profileLocalCallLocked(ctx context.Context, method string, params json.RawMessage) (any, error) {
	var request struct {
		ID               string      `json:"id"`
		Name             *string     `json:"name"`
		URL              *string     `json:"url"`
		UserAgent        *string     `json:"userAgent"`
		UseProxy         *bool       `json:"useProxy"`
		UpdateHours      optionalInt `json:"updateHours"`
		ClearUpdateHours bool        `json:"clearUpdateHours"`
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
		inspect := e.store.InspectProfile(item)
		return ProfileEditor{
			ID:                  item.ID,
			Name:                item.Name,
			URL:                 item.URL,
			Kind:                item.Kind,
			Info:                item.Info,
			UserAgent:           item.UserAgent,
			UpdateHours:         item.UpdateHours,
			UseProxy:            item.UseProxy,
			IsAggregate:         item.IsAggregate(),
			HasPath:             item.Path != "",
			IsPureLocal:         item.IsPureLocal(),
			NodeCount:           inspect.NodeCount,
			LocalNodeCount:      inspect.LocalNodeCount,
			EditedNodeCount:     inspect.EditedNodeCount,
			EditedGroupCount:    inspect.EditedGroupCount,
			ExtraRuleCount:      inspect.ExtraRuleCount,
			TemplateCount:       inspect.TemplateCount,
			LastError:           inspect.LastError,
			DependentAggregates: inspect.DependentAggregates,
			Aggregate:           inspect.Aggregate,
		}, nil
	}
	if !e.profileMu.TryLock() {
		return nil, failure("busy", "订阅正在更新，请稍后重试")
	}
	defer e.profileMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if method == "profiles.delete" {
		if item.ID == e.store.ActiveID() && e.proxyRunningLocked() {
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
	if request.UserAgent != nil && !item.IsFile() && !item.IsAggregate() {
		item.UserAgent = strings.TrimSpace(*request.UserAgent)
	}
	if request.UseProxy != nil && !item.IsFile() && !item.IsAggregate() {
		item.UseProxy = *request.UseProxy
	}
	if !item.IsFile() && !item.IsAggregate() {
		if request.ClearUpdateHours {
			item.UpdateHours = nil
		} else if request.UpdateHours.Set {
			if request.UpdateHours.Value == nil || *request.UpdateHours.Value < 0 {
				item.UpdateHours = nil
			} else {
				v := *request.UpdateHours.Value
				item.UpdateHours = &v
			}
		}
	}
	if err := e.store.Edit(item); err != nil {
		return nil, err
	}
	summary := e.profileSummaryLocked(item, e.store.ActiveID())
	summary.ReconnectRequired = summary.Active && e.reconnectRequired && e.proxyRunningLocked()
	return ProfileMutation{Profile: &summary, Saved: true, Applied: false, ReconnectRequired: summary.ReconnectRequired, Status: e.statusLocked()}, nil
}
