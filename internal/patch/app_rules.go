package patch

import "gopurr/internal/localize"

import (
	"fmt"
	"regexp"
	"strings"

	"gopurr/internal/applications"
)

type AppRule struct {
	Application    applications.Application `json:"application"`
	Target         string                   `json:"target"`
	IncludeHelpers bool                     `json:"includeHelpers"`
}

// AppRuleText uses escaped, anchored paths. A name such as "Electron Helper"
// would also match unrelated apps. Bundle containment includes versioned
// Frameworks, nested Helper.app executables, XPC services and login items.
func AppRuleText(route AppRule) (string, error) {
	app := route.Application
	if !absoluteAppPath(app.Path) || !absoluteAppPath(app.Executable) || strings.ContainsAny(app.Path+app.Executable, "\r\n\x00") {
		return "", fmt.Errorf("%s", localize.Text("应用路径必须是完整路径，且不能包含换行"))
	}
	if route.Target == "" || strings.TrimSpace(route.Target) != route.Target || strings.ContainsAny(route.Target, ",\r\n\x00") {
		return "", fmt.Errorf("%s", localize.Text("请选择有效的直连、拦截或代理组策略"))
	}
	quote := func(path string) string {
		// Commas delimit mihomo rules even inside regex payloads.
		return strings.ReplaceAll(regexp.QuoteMeta(path), ",", `\x2c`)
	}
	pattern := quote(app.Executable)
	if route.IncludeHelpers {
		if !app.IsBundle() {
			return "", fmt.Errorf("%s", localize.Text("仅 .app 应用包支持自动包含 Helper；其他程序请单独添加 Helper"))
		}
		pattern = "(?:" + pattern + "|" + quote(app.Path+"/Contents/") + ".+)"
	}
	return `PROCESS-PATH-REGEX,\A` + pattern + `\z,` + route.Target, nil
}

func absoluteAppPath(path string) bool {
	// Keep backups portable without interpreting a Windows path using the
	// macOS host's filepath implementation (or vice versa).
	return strings.HasPrefix(path, "/") ||
		(len(path) > 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && path[2] == '\\') ||
		strings.HasPrefix(path, `\\`)
}

func prependAppRules(doc map[string]any, routes []AppRule) error {
	if len(routes) == 0 {
		return nil
	}
	rules := make([]any, 0, len(routes))
	seen := map[string]bool{}
	for i, route := range routes {
		key := strings.ToLower(route.Application.Path)
		if seen[key] {
			return fmt.Errorf(localize.Text("应用 %q 重复，请为每个应用设置一个策略"), route.Application.Name)
		}
		seen[key] = true
		rule, err := AppRuleText(route)
		if err == nil {
			err = validateRule(doc, rule)
		}
		if err != nil {
			return fmt.Errorf(localize.Text("第 %d 条应用分流（%s）无效：%w"), i+1, route.Application.Name, err)
		}
		rules = append(rules, rule)
	}
	existing, _ := doc["rules"].([]any)
	doc["rules"] = append(rules, existing...)
	// A subscription with process lookup disabled must not silently defeat
	// the user's application policies. Removing all policies restores it.
	doc["find-process-mode"] = "always"
	return nil
}
