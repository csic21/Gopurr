// Package patch applies this app's runtime settings on top of a subscription
// config, so a provider's file never has to be edited by hand.
package patch

import (
	"fmt"

	"gopurr/internal/controllerconfig"

	"go.yaml.in/yaml/v3"
)

// Overrides combines app-owned runtime settings with optional user additions.
// Empty ExtraRules and MergeYAML clear those additions on the next Apply.
type Overrides struct {
	MixedPort int    `json:"mixedPort"`
	AllowLAN  bool   `json:"allowLan"`
	IPv6      bool   `json:"ipv6"`
	Unified   bool   `json:"unified"` // unified-delay
	Mode      string `json:"mode"`

	TunEnable   bool   `json:"tunEnable"`
	TunStack    string `json:"tunStack"`
	TunAutoRout bool   `json:"tunAutoRoute"`

	ExtraRules []string `json:"extraRules,omitempty"`
	MergeYAML  string   `json:"mergeYaml,omitempty"`

	// nil follows the subscription and YAML overrides; a non-nil value owns
	// only the fields exposed by the corresponding settings page.
	DNS     *DNSSettings     `json:"dns,omitempty"`
	Sniffer *SnifferSettings `json:"sniffer,omitempty"`
}

// Default is what a fresh install runs with: mixed port 7890, rule mode, TUN
// off until the user asks for it.
func Default() Overrides {
	return Overrides{
		MixedPort:   7890,
		AllowLAN:    false,
		IPv6:        false,
		Unified:     true,
		Mode:        "rule",
		TunEnable:   false,
		TunStack:    "mixed",
		TunAutoRout: true,
	}
}

// Apply builds a runtime config without changing the subscription bytes. User
// YAML is merged first, extra rules are prepended next, and app settings win last.
func Apply(raw []byte, ov Overrides) ([]byte, error) {
	doc, err := readConfigMap(raw, "订阅配置")
	if err != nil {
		return nil, err
	}
	controllerconfig.Strip(doc)
	rulesReplaced, err := mergeUserYAML(doc, ov.MergeYAML)
	if err != nil {
		return nil, err
	}
	if err := validateConfigStructure(doc); err != nil {
		return nil, err
	}
	if rulesReplaced {
		rules, _ := doc["rules"].([]any)
		for i, rule := range rules {
			if err := validateRule(doc, rule.(string)); err != nil {
				return nil, fmt.Errorf("扩展 YAML 中第 %d 条 rules 规则无效: %w", i+1, err)
			}
		}
	}
	if err := prependExtraRules(doc, ov.ExtraRules); err != nil {
		return nil, err
	}

	if ov.MixedPort > 0 {
		doc["mixed-port"] = ov.MixedPort
		// The mixed listener serves both HTTP and SOCKS. Subscription files
		// often put their HTTP listener on this same port, which would claim
		// it before mihomo can start the mixed listener.
		delete(doc, "port")
		delete(doc, "socks-port")
	}
	doc["allow-lan"] = ov.AllowLAN
	doc["ipv6"] = ov.IPv6
	doc["unified-delay"] = ov.Unified
	if ov.Mode != "" {
		doc["mode"] = ov.Mode
	}

	tun, _ := doc["tun"].(map[string]any)
	if tun == nil {
		tun = map[string]any{}
	}
	tun["enable"] = ov.TunEnable
	if ov.TunEnable {
		if ov.TunStack != "" {
			tun["stack"] = ov.TunStack
		}
		tun["auto-detect-interface"] = true
		tun["auto-route"] = ov.TunAutoRout
		if _, ok := tun["dns-hijack"]; !ok {
			tun["dns-hijack"] = []any{"any:53"}
		}
	}
	doc["tun"] = tun
	if err := applyNetworkFeatures(doc, ov); err != nil {
		return nil, err
	}

	// A TUN device needs a DNS the kernel answers locally.
	if ov.TunEnable {
		dns, _ := doc["dns"].(map[string]any)
		if dns == nil {
			dns = map[string]any{}
		}
		if _, ok := dns["enable"]; !ok {
			dns["enable"] = true
		}
		if _, ok := dns["enhanced-mode"]; !ok {
			dns["enhanced-mode"] = "fake-ip"
		}
		if _, ok := dns["fake-ip-range"]; !ok {
			dns["fake-ip-range"] = "198.18.0.1/16"
		}
		doc["dns"] = dns
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("生成运行配置失败: %w", err)
	}
	return out, nil
}

// MixedPort reads the mixed port a config would listen on, for the system
// proxy step.
func MixedPort(raw []byte) int {
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return 0
	}
	switch v := doc["mixed-port"].(type) {
	case int:
		return v
	case uint64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}
