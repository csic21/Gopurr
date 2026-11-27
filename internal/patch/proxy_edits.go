package patch

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	RC "github.com/metacubex/mihomo/rules/common"
	"go.yaml.in/yaml/v3"
	"gopurr/internal/controllerconfig"
	"gopurr/internal/localize"
)

// ProxyEdits stores only fields the user changed. Subscription refreshes keep
// new credentials, protocol options and newly added nodes instead of restoring
// an editor's old snapshot over the whole proxies/proxy-groups arrays.
type ProxyEdits struct {
	Nodes  []NodeEdit  `json:"nodes,omitempty"`
	Groups []GroupEdit `json:"groups,omitempty"`
}

type NodeEdit struct {
	Source  string         `json:"source"`
	LocalID string         `json:"localId,omitempty"` // stable identity for locally added nodes
	Aliases []string       `json:"aliases,omitempty"`
	Name    *string        `json:"name,omitempty"`
	Server  *string        `json:"server,omitempty"`
	Port    *int           `json:"port,omitempty"`
	Options map[string]any `json:"options,omitempty"` // recursive merge patch; null removes a key
	Delete  bool           `json:"delete,omitempty"`
}

type GroupEdit struct {
	Source        string    `json:"source,omitempty"` // empty for a locally added group
	Aliases       []string  `json:"aliases,omitempty"`
	Name          *string   `json:"name,omitempty"`
	Type          *string   `json:"type,omitempty"`
	Members       *[]string `json:"members,omitempty"`
	AppendMembers []string  `json:"appendMembers,omitempty"` // keep inherited members following subscription updates
	Delete        bool      `json:"delete,omitempty"`
}

type ProxyNode struct {
	Name    string         `json:"name"`
	Type    string         `json:"type"`
	Server  string         `json:"server"`
	Port    int            `json:"port"`
	Options map[string]any `json:"options,omitempty"`
}
type ProxyGroup struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Members []string `json:"members"`
}

// ProxyEditorBase builds the intermediate source identities for the visual
// editor. Rules in a template may intentionally refer to names that saved
// ProxyEdits create later, so this stage validates structure, not policy
// references. It never applies edits, app rules or managed runtime settings.
func ProxyEditorBase(raw []byte, ov Overrides, local ProfileOverrides) ([]byte, error) {
	doc, err := readConfigMap(raw, localize.Text("订阅配置"))
	if err != nil {
		return nil, err
	}
	controllerconfig.Strip(doc)
	if err := ValidateTemplateLibrary(ov.Library); err != nil {
		return nil, err
	}
	if _, err := applyTemplateYAML(doc, ov.Library, ov.TemplateIDs); err != nil {
		return nil, err
	}
	if _, err := mergeUserYAML(doc, ov.MergeYAML); err != nil {
		return nil, err
	}
	if err := applyNetworkFeatures(doc, Overrides{DNS: ov.DNS, Sniffer: ov.Sniffer}); err != nil {
		return nil, err
	}
	if _, err := applyTemplateYAML(doc, ov.Library, local.TemplateIDs); err != nil {
		return nil, fmt.Errorf(localize.Text("订阅覆写：%w"), err)
	}
	if _, err := mergeUserYAML(doc, local.MergeYAML); err != nil {
		return nil, fmt.Errorf(localize.Text("订阅覆写：%w"), err)
	}
	if err := applyNetworkFeatures(doc, Overrides{DNS: local.DNS}); err != nil {
		return nil, fmt.Errorf(localize.Text("订阅 DNS：%w"), err)
	}
	if err := validateConfigStructure(doc); err != nil {
		return nil, err
	}
	return yaml.Marshal(doc)
}

func CloneProxyEdits(edits *ProxyEdits) *ProxyEdits {
	if edits == nil {
		return nil
	}
	body, _ := json.Marshal(edits)
	var out ProxyEdits
	_ = json.Unmarshal(body, &out)
	return &out
}

func ReadProxyEditor(raw []byte) ([]ProxyNode, []ProxyGroup, error) {
	doc, err := readConfigMap(raw, localize.Text("订阅配置"))
	if err != nil {
		return nil, nil, err
	}
	if err := validateConfigStructure(doc); err != nil {
		return nil, nil, err
	}
	var nodes []ProxyNode
	var groups []ProxyGroup
	for _, entry := range proxyEntries(doc, "proxies") {
		m := entry.(map[string]any)
		port, _ := strconv.Atoi(fmt.Sprint(m["port"]))
		server, _ := m["server"].(string)
		options := MergeProxyOptions(nil, m)
		delete(options, "name")
		delete(options, "server")
		delete(options, "port")
		nodes = append(nodes, ProxyNode{Name: m["name"].(string), Type: m["type"].(string), Server: server, Port: port, Options: options})
	}
	for _, entry := range proxyEntries(doc, "proxy-groups") {
		m := entry.(map[string]any)
		groups = append(groups, ProxyGroup{Name: m["name"].(string), Type: m["type"].(string), Members: proxyMembers(m)})
	}
	return nodes, groups, nil
}

func PreviewProxyEdits(raw []byte, edits *ProxyEdits) ([]byte, error) {
	doc, err := readConfigMap(raw, localize.Text("订阅配置"))
	if err != nil {
		return nil, err
	}
	if err := applyProxyEdits(doc, edits); err != nil {
		return nil, err
	}
	return yaml.Marshal(doc)
}

func proxyEntries(doc map[string]any, key string) []any { list, _ := doc[key].([]any); return list }
func proxyMembers(m map[string]any) []string {
	var out []string
	for _, v := range proxyEntries(m, "proxies") {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func validProxyName(name string) bool {
	return name != "" && len(name) <= 512 && strings.TrimSpace(name) == name && strings.IndexFunc(name, unicode.IsControl) < 0 && !strings.Contains(name, ",") && !proxyBuiltins[name]
}

var proxyBuiltins = map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true, "PASS": true, "COMPATIBLE": true, "PASS-RULE": true, "GLOBAL": true}

// Target maps a policy name in external app rules through simultaneous edits.
func (edits *ProxyEdits) Target(name string) string {
	if edits == nil {
		return name
	}
	for _, e := range edits.Nodes {
		if e.Source != "" && e.Source == name && !e.Delete {
			if e.Name != nil {
				return *e.Name
			}
			return e.Source
		}
	}
	for _, e := range edits.Groups {
		if e.Source == name && !e.Delete {
			if e.Name != nil {
				return *e.Name
			}
			return e.Source
		}
	}
	return edits.aliasTarget(name)
}

func (edits *ProxyEdits) aliasTarget(name string) string {
	if edits == nil {
		return name
	}
	for _, e := range edits.Nodes {
		if !e.Delete && slices.Contains(e.Aliases, name) {
			if e.Name != nil {
				return *e.Name
			}
			return e.Source
		}
	}
	for _, e := range edits.Groups {
		if !e.Delete && slices.Contains(e.Aliases, name) {
			if e.Name != nil {
				return *e.Name
			}
			return e.Source
		}
	}
	return name
}

// PruneDeletedLocalNodeRefs cleans references to removed local nodes (Source == "")
// out of GroupEdit.Members and GroupEdit.AppendMembers, returning the pruned
// ProxyEdits along with the list of deleted local node names so callers can
// validate that no dialer-proxy or rule references broke.
func PruneDeletedLocalNodeRefs(previous, next *ProxyEdits) (*ProxyEdits, []string) {
	out := CloneProxyEdits(next)
	survivingLocalIDs := map[string]bool{}
	survivingNames := map[string]bool{}
	var deletedCandidates []string

	if out != nil {
		keptNodes := make([]NodeEdit, 0, len(out.Nodes))
		for _, e := range out.Nodes {
			if e.Source == "" && e.Delete {
				if e.Name != nil && *e.Name != "" {
					deletedCandidates = append(deletedCandidates, *e.Name)
				}
				deletedCandidates = append(deletedCandidates, e.Aliases...)
				continue
			}
			keptNodes = append(keptNodes, e)
			if e.Source == "" && e.LocalID != "" {
				survivingLocalIDs[e.LocalID] = true
			}
			if !e.Delete {
				if e.Name != nil && *e.Name != "" {
					survivingNames[*e.Name] = true
				} else if e.Source != "" {
					survivingNames[e.Source] = true
				}
			}
		}
		out.Nodes = keptNodes
	}
	if previous != nil {
		for _, e := range previous.Nodes {
			if e.Source != "" {
				continue
			}
			if e.LocalID != "" && survivingLocalIDs[e.LocalID] {
				continue
			}
			if e.Name != nil && *e.Name != "" {
				deletedCandidates = append(deletedCandidates, *e.Name)
			}
			deletedCandidates = append(deletedCandidates, e.Aliases...)
		}
	}
	deletedSet := map[string]bool{}
	var deletedNames []string
	for _, name := range deletedCandidates {
		if name == "" || survivingNames[name] || deletedSet[name] {
			continue
		}
		deletedSet[name] = true
		deletedNames = append(deletedNames, name)
	}
	if out == nil {
		return nil, deletedNames
	}
	if len(deletedSet) > 0 {
		keptGroups := make([]GroupEdit, 0, len(out.Groups))
		for _, g := range out.Groups {
			if g.Members != nil {
				filtered := make([]string, 0, len(*g.Members))
				for _, m := range *g.Members {
					if !deletedSet[m] {
						filtered = append(filtered, m)
					}
				}
				g.Members = &filtered
			}
			if len(g.AppendMembers) > 0 {
				filtered := make([]string, 0, len(g.AppendMembers))
				for _, m := range g.AppendMembers {
					if !deletedSet[m] {
						filtered = append(filtered, m)
					}
				}
				g.AppendMembers = filtered
			}
			if g.Source != "" && !g.Delete && g.Name == nil && g.Type == nil && g.Members == nil && len(g.AppendMembers) == 0 && len(g.Aliases) == 0 {
				continue
			}
			keptGroups = append(keptGroups, g)
		}
		out.Groups = keptGroups
	}
	if len(out.Nodes) == 0 && len(out.Groups) == 0 {
		return nil, deletedNames
	}
	return out, deletedNames
}

// ValidateDeletedNodeRefs checks that a compiled configuration no longer
// references any deleted local node via dialer-proxy, listeners, tunnels, or rules.
func ValidateDeletedNodeRefs(raw []byte, deletedNames []string) error {
	if len(deletedNames) == 0 {
		return nil
	}
	doc, err := readConfigMap(raw, localize.Text("订阅配置"))
	if err != nil {
		return err
	}
	existing := ruleTargets(doc)
	deleted := map[string]bool{}
	for _, name := range deletedNames {
		if name != "" && !existing[name] {
			deleted[name] = true
		}
	}
	if len(deleted) == 0 {
		return nil
	}
	var checkDialers func(any) error
	checkDialers = func(value any) error {
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				if key == "dialer-proxy" {
					if target, ok := item.(string); ok && deleted[target] {
						return fmt.Errorf(localize.Text("节点 %q 仍被 dialer-proxy 使用，请先修改链式代理"), target)
					}
				}
				if err := checkDialers(item); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range v {
				if err := checkDialers(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := checkDialers(doc); err != nil {
		return err
	}
	for _, section := range []string{"listeners", "tunnels"} {
		for _, entry := range proxyEntries(doc, section) {
			switch item := entry.(type) {
			case map[string]any:
				if target, ok := item["proxy"].(string); ok && deleted[target] {
					return fmt.Errorf(localize.Text("节点 %q 仍被入站或端口转发使用，请先修改对应出口"), target)
				}
			case string:
				if section != "tunnels" {
					continue
				}
				parts := strings.Split(item, ",")
				if len(parts) == 4 && deleted[strings.TrimSpace(parts[3])] {
					return fmt.Errorf(localize.Text("节点 %q 仍被入站或端口转发使用，请先修改对应出口"), strings.TrimSpace(parts[3]))
				}
			}
		}
	}
	if rules, ok := doc["rules"].([]any); ok {
		for _, v := range rules {
			rule, _ := v.(string)
			kind, _, target, _ := RC.ParseRulePayload(rule, true)
			if kind != "SUB-RULE" && deleted[target] {
				return fmt.Errorf(localize.Text("节点 %q 仍被规则使用，请先修改规则"), target)
			}
		}
	}
	if subs, ok := doc["sub-rules"].(map[string]any); ok {
		for _, v := range subs {
			if rules, ok := v.([]any); ok {
				for _, item := range rules {
					rule, _ := item.(string)
					kind, _, target, _ := RC.ParseRulePayload(rule, true)
					if kind != "SUB-RULE" && deleted[target] {
						return fmt.Errorf(localize.Text("节点 %q 仍被规则使用，请先修改规则"), target)
					}
				}
			}
		}
	}
	return nil
}

func RewriteProxyRuleTargets(rules []string, edits *ProxyEdits) []string {
	out := slices.Clone(rules)
	for i, rule := range out {
		out[i] = rewriteProxyRule(rule, edits)
	}
	return out
}

func rewriteProxyRule(rule string, edits *ProxyEdits) string {
	kind, payload, target, params := RC.ParseRulePayload(rule, true)
	if target == "" || kind == "SUB-RULE" {
		return rule
	}
	next := edits.Target(target)
	if next == target {
		return rule
	}
	if kind == "MATCH" {
		return "MATCH," + next
	}
	parts := []string{kind, payload, next}
	parts = append(parts, params...)
	return strings.Join(parts, ",")
}

func applyProxyEdits(doc map[string]any, edits *ProxyEdits) error {
	if edits == nil || len(edits.Nodes)+len(edits.Groups) == 0 {
		return nil
	}
	if err := validateConfigStructure(doc); err != nil {
		return err
	}
	nodes, groups := proxyEntries(doc, "proxies"), proxyEntries(doc, "proxy-groups")
	nodeSource, groupSource := map[string]map[string]any{}, map[string]map[string]any{}
	nodeIndex, groupIndex := map[string]int{}, map[string]int{}
	sourceOwner := map[string]string{}
	for i, v := range nodes {
		m := v.(map[string]any)
		name := m["name"].(string)
		nodeSource[name] = m
		nodeIndex[name] = i
		sourceOwner[name] = "node:" + name
	}
	for i, v := range groups {
		m := v.(map[string]any)
		name := m["name"].(string)
		groupSource[name] = m
		groupIndex[name] = i
		sourceOwner[name] = "group:" + name
	}
	seen := map[string]bool{}
	deletedNodes := map[string]bool{}
	deletedNodeIndices := map[int]bool{}
	for _, e := range edits.Nodes {
		identity := e.Source
		if e.Source == "" {
			if e.Delete {
				return fmt.Errorf("%s", localize.Text("新增节点不能标记为删除"))
			}
			identity = "local:" + e.LocalID
			if e.LocalID == "" {
				return fmt.Errorf("%s", localize.Text("本地节点缺少标识，请重新添加"))
			}
		}
		if seen["node:"+identity] {
			return fmt.Errorf(localize.Text("节点 %q 有重复修改"), e.Source)
		}
		seen["node:"+identity] = true
		m := nodeSource[e.Source]
		if e.Source == "" {
			m = map[string]any{}
			nodes = append(nodes, m)
		} else if m == nil {
			return fmt.Errorf(localize.Text("原节点 %q 已不存在，请移除其本地修改后重试"), e.Source)
		}
		if e.Delete {
			deletedNodes[e.Source] = true
			for _, alias := range e.Aliases {
				deletedNodes[alias] = true
			}
			deletedNodeIndices[nodeIndex[e.Source]] = true
			continue
		}
		for _, key := range []string{"name", "server", "port"} {
			if _, ok := e.Options[key]; ok {
				return fmt.Errorf(localize.Text("请在表单中填写 %s，不要放入高级参数"), key)
			}
		}
		options := MergeProxyOptions(m, e.Options)
		clear(m)
		for key, value := range options {
			m[key] = value
		}
		if e.Name != nil {
			if !validProxyName(*e.Name) {
				return fmt.Errorf("%s", localize.Text("名称不能为空、内置策略名或包含逗号和换行"))
			}
			m["name"] = *e.Name
		}
		if e.Server != nil {
			if strings.TrimSpace(*e.Server) == "" || strings.IndexFunc(*e.Server, unicode.IsSpace) >= 0 || strings.IndexFunc(*e.Server, unicode.IsControl) >= 0 {
				return fmt.Errorf("%s", localize.Text("服务器地址不能为空或包含空白"))
			}
			m["server"] = *e.Server
		}
		if e.Port != nil {
			if *e.Port < 1 || *e.Port > 65535 {
				return fmt.Errorf("%s", localize.Text("节点端口须为 1–65535"))
			}
			m["port"] = *e.Port
		}
		if e.Source == "" && (e.Name == nil || e.Server == nil || e.Port == nil) {
			return fmt.Errorf("%s", localize.Text("新增节点须填写名称、服务器地址和端口"))
		}
		if kind, _ := m["type"].(string); strings.TrimSpace(kind) == "" {
			return fmt.Errorf("%s", localize.Text("请选择节点协议"))
		}
	}
	if len(deletedNodeIndices) > 0 {
		keptNodes := make([]any, 0, len(nodes))
		for i, v := range nodes {
			if !deletedNodeIndices[i] {
				keptNodes = append(keptNodes, v)
			}
		}
		nodes = keptNodes
	}
	deleted := map[string]bool{}
	deletedIndices := map[int]bool{}
	changedGroups := map[string]bool{}
	explicitMembers := map[string]bool{}
	appendedMembers := map[string][]string{}
	for _, e := range edits.Groups {
		if e.Source != "" && seen["group:"+e.Source] {
			return fmt.Errorf(localize.Text("策略组 %q 有重复修改"), e.Source)
		}
		seen["group:"+e.Source] = true
		var m map[string]any
		if e.Source == "" {
			if e.Delete {
				return fmt.Errorf("%s", localize.Text("新增策略组不能标记为删除"))
			}
			m = map[string]any{"type": "select"}
			groups = append(groups, m)
		} else {
			m = groupSource[e.Source]
			if m == nil {
				return fmt.Errorf(localize.Text("原策略组 %q 已不存在，请移除其本地修改后重试"), e.Source)
			}
		}
		if e.Delete {
			deleted[e.Source] = true
			for _, alias := range e.Aliases {
				deleted[alias] = true
			}
			deletedIndices[groupIndex[e.Source]] = true
			continue
		}
		if e.Name != nil {
			m["name"] = *e.Name
		}
		name, _ := m["name"].(string)
		if !validProxyName(name) {
			return fmt.Errorf("%s", localize.Text("名称不能为空、内置策略名或包含逗号和换行"))
		}
		if e.Type != nil {
			if !slices.Contains([]string{"select", "url-test", "fallback", "load-balance"}, *e.Type) {
				return fmt.Errorf("%s", localize.Text("请选择手动、自动测速、故障转移或负载均衡策略"))
			}
			m["type"] = *e.Type
		}
		if e.Members != nil {
			explicitMembers[name] = true
			values := make([]any, len(*e.Members))
			for i, v := range *e.Members {
				values[i] = v
			}
			m["proxies"] = values
		}
		appendedMembers[name] = e.AppendMembers
		if m["type"] != "select" {
			if m["url"] == nil {
				m["url"] = "https://www.gstatic.com/generate_204"
			}
			if m["interval"] == nil {
				m["interval"] = 300
			}
		}
		changedGroups[name] = true
	}
	var kept []any
	for i, v := range groups {
		if !deletedIndices[i] {
			kept = append(kept, v)
		}
	}
	groups = kept
	all := map[string]bool{}
	finalOwner := map[string]string{}
	for name := range proxyBuiltins {
		all[name] = true
	}
	for _, entries := range [][]any{nodes, groups} {
		for _, v := range entries {
			name := v.(map[string]any)["name"].(string)
			if all[name] {
				return fmt.Errorf(localize.Text("节点或策略组名称 %q 重复"), name)
			}
			all[name] = true
		}
	}
	for source, m := range nodeSource {
		if !deletedNodes[source] {
			finalOwner[m["name"].(string)] = "node:" + source
		}
	}
	for _, e := range edits.Nodes {
		if e.Source == "" && !e.Delete {
			finalOwner[*e.Name] = "local-node:" + e.LocalID
		}
	}
	for source, m := range groupSource {
		if !deleted[source] {
			finalOwner[m["name"].(string)] = "group:" + source
		}
	}
	for _, v := range groups {
		name := v.(map[string]any)["name"].(string)
		if finalOwner[name] == "" {
			finalOwner[name] = "new:" + name
		}
	}
	aliasOwner := map[string]string{}
	checkAliases := func(source string, aliases []string, owner string) error {
		if len(aliases) > 32 || source == "" && len(aliases) > 0 {
			return fmt.Errorf("%s", localize.Text("历史名称映射过多或不属于原有节点，请恢复所选项后重试"))
		}
		seen := map[string]bool{}
		for _, alias := range aliases {
			if !validProxyName(alias) || seen[alias] {
				return fmt.Errorf("%s", localize.Text("历史名称映射无效或重复，请恢复所选项后重试"))
			}
			seen[alias] = true
			if other := sourceOwner[alias]; other != "" && other != owner {
				return fmt.Errorf(localize.Text("历史名称 %q 与当前节点或策略组冲突，请恢复所选项后重试"), alias)
			}
			if other := finalOwner[alias]; other != "" && other != owner {
				return fmt.Errorf(localize.Text("历史名称 %q 与当前节点或策略组冲突，请恢复所选项后重试"), alias)
			}
			if other := aliasOwner[alias]; other != "" && other != owner {
				return fmt.Errorf(localize.Text("历史名称 %q 与其他本地修改冲突，请恢复所选项后重试"), alias)
			}
			aliasOwner[alias] = owner
		}
		return nil
	}
	for _, e := range edits.Nodes {
		source, owner := e.Source, "node:"+e.Source
		if source == "" {
			source, owner = e.LocalID, "local-node:"+e.LocalID
		}
		if err := checkAliases(source, e.Aliases, owner); err != nil {
			return err
		}
	}
	for _, e := range edits.Groups {
		if err := checkAliases(e.Source, e.Aliases, "group:"+e.Source); err != nil {
			return err
		}
	}
	// dialer-proxy is also a policy reference: inline proxy chains, provider
	// overrides and NTP may use it. Keep those references aligned with the
	// renamed identity, including historical local names.
	var rewriteDialers func(any) error
	rewriteDialers = func(value any) error {
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				if key == "dialer-proxy" {
					if target, ok := item.(string); ok {
						next := edits.Target(target)
						if deleted[next] {
							return fmt.Errorf(localize.Text("策略组 %q 仍被 dialer-proxy 使用，请先修改链式代理"), next)
						}
						if deletedNodes[next] {
							return fmt.Errorf(localize.Text("节点 %q 仍被 dialer-proxy 使用，请先修改链式代理"), next)
						}
						v[key] = next
					}
				}
				if err := rewriteDialers(item); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range v {
				if err := rewriteDialers(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	// Include newly appended nodes when rewriting chain references.
	doc["proxies"] = nodes
	if err := rewriteDialers(doc); err != nil {
		return err
	}
	// Only these known top-level sections use `proxy` as an outbound policy.
	// Unrelated protocol/plugin dictionaries may use the same key for data.
	for _, section := range []string{"listeners", "tunnels"} {
		entries := proxyEntries(doc, section)
		for i, entry := range entries {
			switch item := entry.(type) {
			case map[string]any:
				if target, ok := item["proxy"].(string); ok {
					next := edits.Target(target)
					if deleted[next] {
						return fmt.Errorf(localize.Text("策略组 %q 仍被入站或端口转发使用，请先修改对应出口"), next)
					}
					if deletedNodes[next] {
						return fmt.Errorf(localize.Text("节点 %q 仍被入站或端口转发使用，请先修改对应出口"), next)
					}
					item["proxy"] = next
				}
			case string:
				// Mihomo also accepts tunnels as network,address,target,proxy.
				if section != "tunnels" {
					continue
				}
				parts := strings.Split(item, ",")
				if len(parts) != 4 {
					continue
				}
				target := strings.TrimSpace(parts[3])
				next := edits.Target(target)
				if deleted[next] {
					return fmt.Errorf(localize.Text("策略组 %q 仍被入站或端口转发使用，请先修改对应出口"), next)
				}
				if deletedNodes[next] {
					return fmt.Errorf(localize.Text("节点 %q 仍被入站或端口转发使用，请先修改对应出口"), next)
				}
				if next != target {
					parts[3] = next
					entries[i] = strings.Join(parts, ",")
				}
			}
		}
	}
	graph := map[string][]string{}
	for _, v := range groups {
		m := v.(map[string]any)
		name := m["name"].(string)
		rawInherited := proxyMembers(m)
		rawAppended := appendedMembers[name]
		members := make([]string, 0, len(rawInherited)+len(rawAppended))
		nodeDeletedFromGroup := false

		if !explicitMembers[name] {
			seenInherited := map[string]bool{}
			for _, member := range rawInherited {
				resolved := edits.Target(member)
				if deletedNodes[resolved] {
					nodeDeletedFromGroup = true
					continue
				}
				if deleted[resolved] {
					return fmt.Errorf(localize.Text("策略组 %q 仍被 %q 使用，请先修改成员"), resolved, name)
				}
				if changedGroups[name] && !all[resolved] {
					return fmt.Errorf(localize.Text("策略组 %q 的成员 %q 不存在"), name, resolved)
				}
				if seenInherited[resolved] {
					return fmt.Errorf(localize.Text("策略组 %q 的成员重复"), name)
				}
				seenInherited[resolved] = true
				members = append(members, resolved)
			}
			seenAppended := map[string]bool{}
			for _, member := range rawAppended {
				resolved := edits.aliasTarget(member)
				if deletedNodes[resolved] {
					nodeDeletedFromGroup = true
					continue
				}
				if deleted[resolved] {
					return fmt.Errorf(localize.Text("策略组 %q 仍被 %q 使用，请先修改成员"), resolved, name)
				}
				if changedGroups[name] && !all[resolved] {
					return fmt.Errorf(localize.Text("策略组 %q 的成员 %q 不存在"), name, resolved)
				}
				if seenAppended[resolved] {
					return fmt.Errorf(localize.Text("策略组 %q 的成员重复"), name)
				}
				seenAppended[resolved] = true
				// Auto-deduplicate AppendMembers against inherited members so an
				// upstream refresh introducing a same-named member never breaks.
				if seenInherited[resolved] {
					continue
				}
				members = append(members, resolved)
			}
		} else {
			seenMembers := map[string]bool{}
			combined := make([]string, 0, len(rawInherited)+len(rawAppended))
			combined = append(combined, rawInherited...)
			combined = append(combined, rawAppended...)
			for _, member := range combined {
				resolved := edits.aliasTarget(member)
				if deletedNodes[resolved] {
					nodeDeletedFromGroup = true
					continue
				}
				if deleted[resolved] {
					return fmt.Errorf(localize.Text("策略组 %q 仍被 %q 使用，请先修改成员"), resolved, name)
				}
				if changedGroups[name] && !all[resolved] {
					return fmt.Errorf(localize.Text("策略组 %q 的成员 %q 不存在"), name, resolved)
				}
				if seenMembers[resolved] {
					return fmt.Errorf(localize.Text("策略组 %q 的成员重复"), name)
				}
				seenMembers[resolved] = true
				members = append(members, resolved)
			}
		}
		if (changedGroups[name] || nodeDeletedFromGroup) && len(members) == 0 && len(proxyEntries(m, "use")) == 0 && m["include-all"] != true && m["include-all-proxies"] != true && m["include-all-providers"] != true {
			return fmt.Errorf(localize.Text("策略组 %q 至少需要一个成员"), name)
		}
		values := make([]any, len(members))
		for i, s := range members {
			values[i] = s
		}
		m["proxies"] = values
		graph[name] = members
	}
	state := map[string]int{}
	var visit func(string) error
	visit = func(name string) error {
		if state[name] == 1 {
			return fmt.Errorf(localize.Text("策略组 %q 存在循环引用"), name)
		}
		if state[name] == 2 {
			return nil
		}
		state[name] = 1
		for _, member := range graph[name] {
			if _, ok := graph[member]; ok {
				if err := visit(member); err != nil {
					return err
				}
			}
		}
		state[name] = 2
		return nil
	}
	for name := range graph {
		if err := visit(name); err != nil {
			return err
		}
	}
	for _, section := range []string{"rules"} {
		if rules, ok := doc[section].([]any); ok {
			for i, v := range rules {
				rule, _ := v.(string)
				next := rewriteProxyRule(rule, edits)
				kind, _, target, _ := RC.ParseRulePayload(next, true)
				if kind != "SUB-RULE" && deleted[target] {
					return fmt.Errorf(localize.Text("策略组 %q 仍被规则使用，请先修改规则"), target)
				}
				if kind != "SUB-RULE" && deletedNodes[target] {
					return fmt.Errorf(localize.Text("节点 %q 仍被规则使用，请先修改规则"), target)
				}
				rules[i] = next
			}
		}
	}
	if subs, ok := doc["sub-rules"].(map[string]any); ok {
		for _, v := range subs {
			if rules, ok := v.([]any); ok {
				for i, item := range rules {
					rule, _ := item.(string)
					next := rewriteProxyRule(rule, edits)
					kind, _, target, _ := RC.ParseRulePayload(next, true)
					if kind != "SUB-RULE" && deleted[target] {
						return fmt.Errorf(localize.Text("策略组 %q 仍被规则使用，请先修改规则"), target)
					}
					if kind != "SUB-RULE" && deletedNodes[target] {
						return fmt.Errorf(localize.Text("节点 %q 仍被规则使用，请先修改规则"), target)
					}
					rules[i] = next
				}
			}
		}
	}
	doc["proxies"], doc["proxy-groups"] = nodes, groups
	return nil
}
