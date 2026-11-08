package engine

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopurr/internal/patch"

	"github.com/dlclark/regexp2"
	C "github.com/metacubex/mihomo/constant"
	"go.yaml.in/yaml/v3"
)

type previewNode struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
}

type previewGroup struct {
	Name                string   `yaml:"name"`
	Type                string   `yaml:"type"`
	Proxies             []string `yaml:"proxies"`
	Use                 []string `yaml:"use"`
	IncludeAll          bool     `yaml:"include-all"`
	IncludeAllProxies   bool     `yaml:"include-all-proxies"`
	IncludeAllProviders bool     `yaml:"include-all-providers"`
	Hidden              bool     `yaml:"hidden"`
	Filter              string   `yaml:"filter"`
	ExcludeFilter       string   `yaml:"exclude-filter"`
	ExcludeType         string   `yaml:"exclude-type"`
}

type previewProvider struct {
	Type          string        `yaml:"type"`
	Path          string        `yaml:"path"`
	URL           string        `yaml:"url"`
	Payload       []previewNode `yaml:"payload"`
	Filter        string        `yaml:"filter"`
	ExcludeFilter string        `yaml:"exclude-filter"`
	ExcludeType   string        `yaml:"exclude-type"`
	Override      struct {
		Prefix       string `yaml:"additional-prefix"`
		Suffix       string `yaml:"additional-suffix"`
		Replacements []any  `yaml:"proxy-name"`
		Expressions  []any  `yaml:"override-expr"`
	} `yaml:"override"`
}

// A disconnected read must not construct mihomo adapters/providers: doing so
// can create health-check tasks. Read only the saved YAML and local cache.
func (e *Engine) previewGroupsLocked() (GroupList, error) {
	result := GroupList{Groups: []Group{}, Preview: true}
	id := e.store.ActiveID()
	if id == "" {
		return result, nil
	}
	p, err := e.store.Get(id)
	if err != nil {
		return result, err
	}
	raw, err := e.store.Content(id)
	if err != nil {
		return result, err
	}
	local := patch.ProfileOverrides{}
	if p.Overrides != nil {
		local = *p.Overrides
	}
	raw, err = patch.ApplyLayers(raw, patch.Default(), local)
	if err != nil {
		return result, failure("invalid_config", "无法读取已保存配置中的节点")
	}
	var doc struct {
		Proxies   []previewNode              `yaml:"proxies"`
		Groups    []previewGroup             `yaml:"proxy-groups"`
		Providers map[string]previewProvider `yaml:"proxy-providers"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return result, failure("invalid_config", "无法读取已保存配置中的节点")
	}
	byName := map[string]string{"DIRECT": "Direct", "REJECT": "Reject", "REJECT-DROP": "RejectDrop", "PASS": "Pass", "COMPATIBLE": "Compatible"}
	var all []string
	for _, n := range doc.Proxies {
		byName[n.Name] = previewType(n.Type)
		all = append(all, n.Name)
	}
	global := []string{"DIRECT", "REJECT"}
	global = append(global, all...)
	hasGlobal := false
	for _, g := range doc.Groups {
		byName[g.Name] = previewType(g.Type)
		global = append(global, g.Name)
		hasGlobal = hasGlobal || g.Name == "GLOBAL"
	}
	var providerOrder []string
	for name := range doc.Providers {
		providerOrder = append(providerOrder, name)
	}
	sort.Strings(providerOrder)
	providerNodes := make(map[string][]previewNode)
	providerMissing := make(map[string]bool)
	for _, name := range providerOrder {
		provider := doc.Providers[name]
		nodes := provider.Payload
		if provider.Type != "inline" {
			path := provider.Path
			if path == "" && provider.Type == "http" {
				path = C.Path.GetPathByHash("proxies", provider.URL)
			}
			if cached, ok := e.readPreviewProvider(path); ok {
				nodes = cached
			} else {
				providerMissing[name] = true
			}
		}
		// Expressions can rename or synthesize nodes; do not pretend that an
		// incomplete static representation is the live provider result.
		if len(provider.Override.Replacements) > 0 || len(provider.Override.Expressions) > 0 {
			providerMissing[name] = true
			continue
		}
		matches := previewFilter(provider.Filter, provider.ExcludeFilter, provider.ExcludeType)
		for _, n := range nodes {
			if matches(n) {
				n.Name = provider.Override.Prefix + n.Name + provider.Override.Suffix
				providerNodes[name] = append(providerNodes[name], n)
			}
		}
	}
	if !hasGlobal {
		doc.Groups = append(doc.Groups, previewGroup{Name: "GLOBAL", Type: "select", Proxies: global})
	}
	for _, source := range doc.Groups {
		g := Group{Name: source.Name, Type: previewType(source.Type), Hidden: source.Hidden, Members: []Member{}}
		matches := previewFilter(source.Filter, source.ExcludeFilter, source.ExcludeType)
		explicitMatches := previewFilter("", source.ExcludeFilter, source.ExcludeType)
		seen := make(map[string]bool)
		add := func(n previewNode) {
			if n.Name != "" && !seen[n.Name] {
				seen[n.Name] = true
				g.Members = append(g.Members, Member{Name: n.Name, Type: previewType(n.Type)})
			}
		}
		for _, name := range source.Proxies {
			if typ, ok := byName[name]; ok && explicitMatches(previewNode{Name: name, Type: typ}) {
				add(previewNode{Name: name, Type: typ})
			}
		}
		if source.IncludeAll || source.IncludeAllProxies {
			for _, name := range all {
				n := previewNode{Name: name, Type: byName[name]}
				if matches(n) {
					add(n)
				}
			}
		}
		use := source.Use
		if source.IncludeAll || source.IncludeAllProviders {
			use = providerOrder
		}
		for _, name := range use {
			if _, exists := doc.Providers[name]; !exists || providerMissing[name] {
				result.Incomplete = true
			}
			for _, n := range providerNodes[name] {
				if matches(n) {
					add(n)
				}
			}
		}
		if g.Type == "Selector" && len(g.Members) > 0 {
			g.Now = g.Members[0].Name
			if saved := e.manualSelections[id][g.Name]; seen[saved] {
				g.Now = saved
			}
		}
		result.Groups = append(result.Groups, g)
	}
	return result, nil
}

func (e *Engine) readPreviewProvider(path string) ([]previewNode, bool) {
	root := filepath.Join(e.home, "mihomo")
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, false
	}
	if path == "" {
		return nil, false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, false
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, false
	}
	f, err := os.Open(resolved)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxDocumentBytes {
		return nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(f, MaxDocumentBytes+1))
	var doc struct {
		Proxies []previewNode `yaml:"proxies"`
	}
	if err != nil || len(raw) > MaxDocumentBytes || yaml.Unmarshal(raw, &doc) != nil || doc.Proxies == nil {
		return nil, false
	}
	return doc.Proxies, true
}

func previewFilter(include, exclude, excludeType string) func(previewNode) bool {
	compile := func(pattern string) []*regexp2.Regexp {
		var patterns []*regexp2.Regexp
		if pattern == "" {
			return patterns
		}
		for _, part := range strings.Split(pattern, "`") {
			r, err := regexp2.Compile(part, regexp2.None)
			if err != nil {
				continue
			}
			r.MatchTimeout = 10 * time.Millisecond
			patterns = append(patterns, r)
		}
		return patterns
	}
	includePatterns, excludePatterns := compile(include), compile(exclude)
	excludedTypes := strings.Split(excludeType, "|")
	match := func(patterns []*regexp2.Regexp, name string) bool {
		for _, r := range patterns {
			if ok, _ := r.MatchString(name); ok {
				return true
			}
		}
		return false
	}
	return func(n previewNode) bool {
		for _, typ := range excludedTypes {
			if typ != "" && previewType(typ) == previewType(n.Type) {
				return false
			}
		}
		return (include == "" || match(includePatterns, n.Name)) && (exclude == "" || !match(excludePatterns, n.Name))
	}
}

func previewType(typ string) string {
	switch strings.ToLower(typ) {
	case "select", "selector":
		return "Selector"
	case "url-test", "urltest":
		return "URLTest"
	case "load-balance", "loadbalance":
		return "LoadBalance"
	case "ss", "shadowsocks":
		return "Shadowsocks"
	case "ssr", "shadowsocksr":
		return "ShadowsocksR"
	case "direct":
		return "Direct"
	case "reject":
		return "Reject"
	case "http":
		return "Http"
	case "fallback":
		return "Fallback"
	case "relay":
		return "Relay"
	default:
		return typ
	}
}
