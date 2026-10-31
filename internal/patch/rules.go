package patch

import (
	"fmt"
	"strconv"
	"strings"

	C "github.com/metacubex/mihomo/constant"
	R "github.com/metacubex/mihomo/rules"
	RC "github.com/metacubex/mihomo/rules/common"
	"github.com/metacubex/mihomo/rules/logic"

	"gopurr/internal/localize"
)

func prependExtraRules(doc map[string]any, extra []string) error {
	var rules []any
	for i, text := range extra {
		rule := strings.TrimSpace(text)
		if rule == "" {
			continue // blank editor lines also allow clearing all additions
		}
		if err := validateRule(doc, rule); err != nil {
			return fmt.Errorf(localize.Text("第 %d 条额外规则无效: %w"), i+1, err)
		}
		rules = append(rules, rule)
	}
	if len(rules) == 0 {
		return nil
	}
	if existing, _ := doc["rules"].([]any); existing != nil {
		rules = append(rules, existing...)
	}
	doc["rules"] = rules
	return nil
}

func validateRule(doc map[string]any, rule string) error {
	if strings.ContainsAny(rule, "\r\n") {
		return fmt.Errorf("%s", localize.Text("每条规则只能占一行"))
	}
	kind, payload, target, params := RC.ParseRulePayload(rule, true)
	count := strings.Count(rule, ",")
	if (kind == "MATCH" && count != 1) || (kind != "MATCH" && count < 2) || target == "" {
		return fmt.Errorf("%s", localize.Text("规则格式应为 类型,匹配内容,目标策略（MATCH,目标策略）"))
	}
	parsed, err := parseRuleOffline(kind, payload, target, params, nil)
	if err != nil {
		return fmt.Errorf(localize.Text("%s 规则参数无效: %w"), kind, err)
	}
	if kind == "SUB-RULE" {
		// SUB-RULE's final field names a sub-rule, not a proxy policy.
		subRules, _ := doc["sub-rules"].(map[string]any)
		if _, ok := subRules[target]; !ok {
			return fmt.Errorf(localize.Text("子规则 %q 不存在"), target)
		}
		if err := stringList(subRules[target], localize.Text("sub-rules 中的 ")+target); err != nil {
			return err
		}
	} else if !ruleTargets(doc)[target] {
		return fmt.Errorf(localize.Text("目标策略 %q 不存在，请使用内置策略或当前配置中的策略组、代理名称"), target)
	}
	providers, _ := doc["rule-providers"].(map[string]any)
	for _, name := range parsed.ProviderNames() {
		if _, ok := providers[name]; !ok {
			return fmt.Errorf(localize.Text("规则集 %q 不存在，请先在 rule-providers 中定义"), name)
		}
	}
	return nil
}

func ruleTargets(doc map[string]any) map[string]bool {
	targets := map[string]bool{
		"DIRECT": true, "REJECT": true, "REJECT-DROP": true,
		"PASS": true, "COMPATIBLE": true, "PASS-RULE": true, "GLOBAL": true,
	}
	for _, key := range []string{"proxy-groups", "proxies"} {
		entries, _ := doc[key].([]any)
		for _, entry := range entries {
			if named, ok := entry.(map[string]any); ok {
				if name, ok := named["name"].(string); ok {
					targets[name] = true
				}
			}
		}
	}
	return targets
}

// Reuse mihomo's parameter validators and regex dialect. Geo constructors can
// download data, so replace only those with inert rules while validating their
// syntax. Logical rules recurse through this function as well, never through
// the data-loading parser. This does not call config.Parse or alter core state.
func parseRuleOffline(kind, payload, target string, params []string, subRules map[string][]C.Rule) (C.Rule, error) {
	if kind != "MATCH" && payload == "" {
		return nil, fmt.Errorf("%s", localize.Text("缺少匹配内容"))
	}
	for _, param := range params {
		if param != "no-resolve" && param != "src" {
			return nil, fmt.Errorf(localize.Text("未知规则选项 %q，可用选项为 no-resolve、src"), param)
		}
	}
	// mihomo's generic range parser narrows integers after parsing them. Catch
	// overflow here so an accidental 65536 cannot silently become port zero.
	bits := 0
	switch kind {
	case "SRC-PORT", "DST-PORT", "IN-PORT":
		bits = 16
	case "DSCP":
		bits = 6
	case "UID":
		bits = 32
	}
	if bits != 0 && payload != "*" {
		for _, segment := range strings.Split(payload, "/") {
			if strings.TrimSpace(segment) == "" {
				continue
			}
			for _, number := range strings.Split(segment, "-") {
				if _, err := strconv.ParseUint(strings.Trim(number, "[ ]"), 10, bits); err != nil {
					return nil, fmt.Errorf(localize.Text("%s 数值或范围 %q 无效或超出范围"), kind, segment)
				}
			}
		}
	}
	switch kind {
	case "GEOSITE", "GEOIP", "SRC-GEOIP":
		return RC.NewMatch(target), nil
	case "IP-ASN", "SRC-IP-ASN":
		asn, err := strconv.ParseUint(payload, 10, 32)
		if err != nil || asn == 0 {
			return nil, fmt.Errorf("%s", localize.Text("ASN 必须是有效的正整数"))
		}
		return RC.NewMatch(target), nil
	case "AND", "OR", "NOT", "SUB-RULE":
		if strings.Trim(payload, "() \t,") == "" {
			return nil, fmt.Errorf("%s", localize.Text("匹配表达式不能为空"))
		}
		if !strings.HasPrefix(payload, "(") || !strings.HasSuffix(payload, ")") {
			return nil, fmt.Errorf("%s", localize.Text("匹配表达式必须用括号包围"))
		}
		if kind != "SUB-RULE" {
			conditions := strings.TrimSpace(payload[1 : len(payload)-1])
			if !strings.HasPrefix(conditions, "(") || !strings.HasSuffix(conditions, ")") {
				return nil, fmt.Errorf("%s", localize.Text("逻辑规则中的每个条件必须用括号包围，例如 ((DOMAIN,example.com))"))
			}
		}
		switch kind {
		case "AND":
			return logic.NewAND(payload, target, parseRuleOffline)
		case "OR":
			return logic.NewOR(payload, target, parseRuleOffline)
		case "NOT":
			return logic.NewNOT(payload, target, parseRuleOffline)
		default:
			return logic.NewSubRule(payload, target, subRules, parseRuleOffline)
		}
	default:
		return R.ParseRule(kind, payload, target, params, subRules)
	}
}
