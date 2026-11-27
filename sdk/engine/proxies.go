package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	C "github.com/metacubex/mihomo/constant"
	"go.yaml.in/yaml/v3"

	"gopurr/internal/patch"
	"gopurr/internal/profile"
)

func (e *Engine) parseProxiesCall(params json.RawMessage) (any, error) {
	var request struct {
		Text string `json:"text"`
		Mode string `json:"mode"`
	}
	if err := decodeParams(params, &request); err != nil {
		return nil, err
	}
	if len(request.Text) > MaxDocumentBytes {
		return nil, failure("invalid_params", "输入内容超过 16 MiB")
	}
	return profile.ParseProxyInputsWithMode(request.Text, request.Mode), nil
}

func (e *Engine) proxyEditorCallLocked(params json.RawMessage) (any, error) {
	var request struct {
		ID string `json:"id"`
	}
	if err := decodeParams(params, &request); err != nil {
		return nil, err
	}
	id := request.ID
	if id == "" {
		id = e.store.ActiveID()
	}
	if id == "" {
		return nil, failure("no_profile", "请先导入并选择配置")
	}
	item, err := e.store.Get(id)
	if err != nil {
		return nil, err
	}
	raw, err := e.store.Content(id)
	if err != nil {
		return nil, err
	}
	local := patch.ProfileOverrides{}
	if item.Overrides != nil {
		local = *item.Overrides
	}
	edits := patch.CloneProxyEdits(local.ProxyEdits)
	baseLocal := local
	baseLocal.ProxyEdits = nil
	baseLocal.ExtraRules = nil

	baseRaw, err := patch.ProxyEditorBase(raw, patch.Default(), baseLocal)
	if err != nil {
		return nil, failure("invalid_config", err.Error())
	}
	baseNodes, baseGroups, err := patch.ReadProxyEditor(baseRaw)
	if err != nil {
		return nil, failure("invalid_config", err.Error())
	}

	var baseDoc struct {
		Groups    []previewGroup             `yaml:"proxy-groups"`
		Providers map[string]previewProvider `yaml:"proxy-providers"`
	}
	_ = yaml.Unmarshal(baseRaw, &baseDoc)
	baseGroupMeta := map[string]previewGroup{}
	for _, g := range baseDoc.Groups {
		baseGroupMeta[g.Name] = g
	}
	providerNodes := []ProviderNodeInfo{}
	var providerOrder []string
	for name := range baseDoc.Providers {
		providerOrder = append(providerOrder, name)
	}
	sort.Strings(providerOrder)
	for _, provName := range providerOrder {
		prov := baseDoc.Providers[provName]
		pNodes := prov.Payload
		if prov.Type != "inline" {
			path := prov.Path
			if path == "" && prov.Type == "http" {
				path = C.Path.GetPathByHash("proxies", prov.URL)
			}
			if cached, ok := e.readPreviewProvider(path); ok {
				pNodes = cached
			}
		}
		for _, pn := range pNodes {
			providerNodes = append(providerNodes, ProviderNodeInfo{
				Provider: provName,
				Name:     prov.Override.Prefix + pn.Name + prov.Override.Suffix,
				Type:     pn.Type,
			})
		}
	}

	nodeMap := map[string]*ProxyEditorNode{}
	nodeOrder := make([]string, 0, len(baseNodes))
	baseNodeMap := map[string]patch.ProxyNode{}
	for _, n := range baseNodes {
		copyNode := n
		baseNodeMap[n.Name] = copyNode
		nodeOrder = append(nodeOrder, n.Name)
		nodeMap[n.Name] = &ProxyEditorNode{
			Source:  n.Name,
			Name:    n.Name,
			Type:    n.Type,
			Server:  n.Server,
			Port:    n.Port,
			Options: patch.MergeProxyOptions(nil, n.Options),
			Base:    &copyNode,
		}
	}

	groupMap := map[string]*ProxyEditorGroup{}
	groupOrder := make([]string, 0, len(baseGroups))
	baseGroupMap := map[string]patch.ProxyGroup{}
	for _, g := range baseGroups {
		copyGroup := g
		baseGroupMap[g.Name] = copyGroup
		groupOrder = append(groupOrder, g.Name)
		inherited := make([]string, len(g.Members))
		for i, m := range g.Members {
			inherited[i] = edits.Target(m)
		}
		meta := baseGroupMeta[g.Name]
		use := slices.Clone(meta.Use)
		if use == nil {
			use = []string{}
		}
		groupMap[g.Name] = &ProxyEditorGroup{
			Source:           g.Name,
			Name:             g.Name,
			Type:             g.Type,
			Members:          slices.Clone(inherited),
			InheritedMembers: slices.Clone(inherited),
			AppendMembers:    []string{},
			Use:              use,
			IncludeAll:       meta.IncludeAll || meta.IncludeAllProxies || meta.IncludeAllProviders,
			Base:             &copyGroup,
		}
	}

	newGroupSeq := 0
	if edits != nil {
		for _, ne := range edits.Nodes {
			key := ne.Source
			if key == "" {
				key = "local:" + ne.LocalID
			}
			row := nodeMap[key]
			if row == nil {
				row = &ProxyEditorNode{
					Source:  ne.Source,
					LocalID: ne.LocalID,
					Name:    ne.Source,
					Options: map[string]any{},
					Missing: ne.Source != "",
				}
				nodeMap[key] = row
				nodeOrder = append(nodeOrder, key)
			}
			row.Edited = true
			mergedOpts := patch.MergeProxyOptions(baseNodeMap[ne.Source].Options, ne.Options)
			row.Options = mergedOpts
			if typ, ok := mergedOpts["type"].(string); ok {
				row.Type = typ
			}
			if ne.Name != nil {
				row.Name = *ne.Name
			}
			if ne.Server != nil {
				row.Server = *ne.Server
			}
			if ne.Port != nil {
				row.Port = *ne.Port
			}
		}
		for _, ge := range edits.Groups {
			key := ge.Source
			if key == "" {
				newGroupSeq++
				key = fmt.Sprintf("new:%d", newGroupSeq)
			}
			row := groupMap[key]
			if row == nil {
				row = &ProxyEditorGroup{
					Source:           ge.Source,
					Name:             ge.Source,
					Type:             "select",
					Members:          []string{},
					InheritedMembers: []string{},
					AppendMembers:    []string{},
					Explicit:         ge.Source == "",
					ExplicitMembers:  ge.Source == "",
					Missing:          ge.Source != "",
					Use:              []string{},
				}
				groupMap[key] = row
				groupOrder = append(groupOrder, key)
			}
			row.Edited = true
			if ge.Name != nil {
				row.Name = *ge.Name
			}
			if ge.Type != nil {
				row.Type = *ge.Type
			}
			if ge.Members != nil {
				row.Members = slices.Clone(*ge.Members)
				row.Explicit = true
				row.ExplicitMembers = true
			}
			if len(ge.AppendMembers) > 0 {
				row.AppendMembers = slices.Clone(ge.AppendMembers)
				if !row.Explicit {
					seen := map[string]bool{}
					combined := make([]string, 0, len(row.InheritedMembers)+len(ge.AppendMembers))
					for _, m := range row.InheritedMembers {
						seen[m] = true
						combined = append(combined, m)
					}
					for _, m := range ge.AppendMembers {
						resolved := edits.Target(m)
						if !seen[resolved] {
							seen[resolved] = true
							combined = append(combined, resolved)
						}
					}
					row.Members = combined
				}
			}
			row.Deleted = ge.Delete
		}
	}

	nodes := make([]ProxyEditorNode, 0, len(nodeOrder))
	availableNames := []string{"DIRECT", "REJECT"}
	for _, key := range nodeOrder {
		row := nodeMap[key]
		if row.Options == nil {
			row.Options = map[string]any{}
		}
		nodes = append(nodes, *row)
		if !row.Missing && row.Name != "" && !slices.Contains(availableNames, row.Name) {
			availableNames = append(availableNames, row.Name)
		}
	}
	groups := make([]ProxyEditorGroup, 0, len(groupOrder))
	for _, key := range groupOrder {
		row := groupMap[key]
		if row.Members == nil {
			row.Members = []string{}
		}
		if row.InheritedMembers == nil {
			row.InheritedMembers = []string{}
		}
		if row.AppendMembers == nil {
			row.AppendMembers = []string{}
		}
		if row.Use == nil {
			row.Use = []string{}
		}
		groups = append(groups, *row)
		if !row.Deleted && !row.Missing && row.Name != "" && !slices.Contains(availableNames, row.Name) {
			availableNames = append(availableNames, row.Name)
		}
	}

	return ProxyEditorPayload{
		ID:             item.ID,
		ProfileID:      item.ID,
		ProfileName:    item.Name,
		ProfileKind:    item.Kind,
		IsPureLocal:    item.IsPureLocal(),
		Nodes:          nodes,
		Groups:         groups,
		Edits:          edits,
		Options:        availableNames,
		AvailableNames: availableNames,
		ProviderNodes:  providerNodes,
	}, nil
}

// proxySaveCallLocked persists proxy/group edits with strict Android VPN safety:
// it NEVER hot-reloads over a live TUN FD. If the active profile is running,
// edits are saved to disk and reconnectRequired is set to true.
func (e *Engine) proxySaveCallLocked(ctx context.Context, params json.RawMessage) (any, error) {
	var request struct {
		ID    string            `json:"id"`
		Edits *patch.ProxyEdits `json:"edits"`
	}
	if err := decodeParams(params, &request); err != nil {
		return nil, err
	}
	id := request.ID
	if id == "" {
		id = e.store.ActiveID()
	}
	if id == "" {
		return nil, failure("no_profile", "请先导入并选择配置")
	}
	if !e.profileMu.TryLock() {
		return nil, failure("busy", "订阅正在更新，请稍后重试")
	}
	defer e.profileMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	item, err := e.store.Get(id)
	if err != nil {
		return nil, err
	}
	raw, err := e.store.Content(id)
	if err != nil {
		return nil, err
	}
	local := patch.ProfileOverrides{}
	if item.Overrides != nil {
		local = *item.Overrides
	}
	pruned, deletedLocal := patch.PruneDeletedLocalNodeRefs(local.ProxyEdits, request.Edits)

	var nextRevision string
	var updatedItem profile.Profile
	if item.IsPureLocal() {
		nextRaw := raw
		if pruned != nil {
			nextRaw, err = patch.PreviewProxyEdits(raw, pruned)
			if err != nil {
				return nil, failure("invalid_config", err.Error())
			}
		}
		if err := patch.ValidateDeletedNodeRefs(nextRaw, deletedLocal); err != nil {
			return nil, failure("invalid_config", err.Error())
		}
		nextLocal := local
		nextLocal.ProxyEdits = nil
		candidate := item
		candidate.Overrides = &nextLocal
		managed, err := e.prepareLocked(nextRaw, candidate)
		if err != nil {
			return nil, err
		}
		if err := patch.ValidateDeletedNodeRefs(managed, deletedLocal); err != nil {
			return nil, failure("invalid_config", err.Error())
		}
		updatedItem, err = e.store.UpdateContentContext(ctx, id, nextRaw, &nextLocal, nil)
		if err != nil {
			return nil, err
		}
		nextRevision = profileRevision(updatedItem, nextRaw)
	} else {
		nextLocal := local
		nextLocal.ProxyEdits = pruned
		candidate := item
		candidate.Overrides = &nextLocal
		managed, err := e.prepareLocked(raw, candidate)
		if err != nil {
			return nil, err
		}
		if err := patch.ValidateDeletedNodeRefs(managed, deletedLocal); err != nil {
			return nil, failure("invalid_config", err.Error())
		}
		if err := e.store.SetOverrides(id, nextLocal); err != nil {
			return nil, err
		}
		updatedItem, err = e.store.Get(id)
		if err != nil {
			return nil, err
		}
		nextRevision = profileRevision(updatedItem, raw)
	}

	if id == e.store.ActiveID() {
		e.pendingPlan = nil
		if e.proxyRunningLocked() {
			e.reconnectRequired = e.runtimeRevision != "" && e.runtimeRevision != nextRevision
		}
	}
	summary := e.profileSummaryLocked(updatedItem, e.store.ActiveID())
	summary.ReconnectRequired = summary.Active && e.reconnectRequired && e.proxyRunningLocked()
	return ProfileMutation{
		Profile:           &summary,
		Saved:             true,
		Applied:           false,
		ReconnectRequired: summary.ReconnectRequired,
		Status:            e.statusLocked(),
	}, nil
}
