package patch

import (
	"fmt"
	"strings"

	"gopurr/internal/localize"
)

// Check common editor mistakes without parsing proxy/provider implementations,
// fetching remote files, or imposing a schema on unrelated subscription data.
func validateConfigStructure(doc map[string]any) error {
	if value := doc["rules"]; value != nil {
		if err := stringList(value, "rules"); err != nil {
			return err
		}
	}
	for _, key := range []string{"proxy-groups", "proxies"} {
		value := doc[key]
		if value == nil {
			continue
		}
		list, ok := value.([]any)
		if !ok {
			return fmt.Errorf(localize.Text("配置中的 %s 必须是列表"), key)
		}
		for i, entry := range list {
			path := fmt.Sprintf(localize.Text("%s 第 %d 项"), key, i+1)
			named, ok := entry.(map[string]any)
			if !ok {
				return fmt.Errorf(localize.Text("配置中的 %s 必须是 YAML 键值映射"), path)
			}
			for _, field := range []string{"name", "type"} {
				text, ok := named[field].(string)
				if !ok || strings.TrimSpace(text) == "" {
					return fmt.Errorf(localize.Text("配置中的 %s 的 %s 必须是非空字符串"), path, field)
				}
			}
			if key == "proxy-groups" {
				for _, field := range []string{"proxies", "use"} {
					if values := named[field]; values != nil {
						if err := stringList(values, path+localize.Text(" 的 ")+field); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

func stringList(value any, path string) error {
	list, ok := value.([]any)
	if !ok {
		return fmt.Errorf(localize.Text("配置中的 %s 必须是字符串列表"), path)
	}
	for i, value := range list {
		text, ok := value.(string)
		if !ok || strings.TrimSpace(text) == "" {
			return fmt.Errorf(localize.Text("配置中的 %s 第 %d 项必须是非空字符串"), path, i+1)
		}
	}
	return nil
}
