package patch

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"go.yaml.in/yaml/v3"

	"gopurr/internal/localize"
)

// Decode one mapping document; yaml.Unmarshal alone silently ignores any later
// documents. An empty editor is a no-op, but explicit null/scalar/list documents
// are not configs.
func readConfigMap(raw []byte, label string) (map[string]any, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	var node yaml.Node
	if err := decoder.Decode(&node); err != nil {
		if err == io.EOF {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf(localize.Text("%s不是合法 YAML: %w"), label, err)
	}
	if len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf(localize.Text("%s必须是 YAML 键值映射（map），不能是列表、标量或 null"), label)
	}
	var doc map[string]any
	if err := node.Decode(&doc); err != nil {
		return nil, fmt.Errorf(localize.Text("%s不是合法 YAML: %w"), label, err)
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf(localize.Text("%s不是合法 YAML: %w"), label, err)
		}
		return nil, fmt.Errorf(localize.Text("%s只能包含一个 YAML 文档"), label)
	}
	return doc, nil
}

func mergeUserYAML(doc map[string]any, raw string) (rulesReplaced bool, err error) {
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}
	addition, err := readConfigMap([]byte(raw), localize.Text("扩展 YAML"))
	if err != nil {
		return false, err
	}
	for key := range addition {
		// The daemon installs its own socket and secret after Apply. Reject all
		// controller variants, including pipe/TLS, rather than accepting a
		// setting the app cannot honor or one that opens another controller.
		if key == "secret" || key == "external-controller" || strings.HasPrefix(key, "external-controller-") {
			return false, fmt.Errorf(localize.Text("扩展 YAML 不能修改 %q：controller 连接与密钥由应用管理"), key)
		}
	}
	mergeMaps(doc, addition)
	_, rulesReplaced = addition["rules"]
	return rulesReplaced, nil
}

func mergeMaps(dst, src map[string]any) {
	for key, value := range src {
		if incoming, ok := value.(map[string]any); ok {
			if current, ok := dst[key].(map[string]any); ok {
				mergeMaps(current, incoming)
				continue
			}
		}
		// Sequences and scalars replace the existing value, including an empty
		// sequence or string. No list concatenation or script execution occurs.
		dst[key] = value
	}
}
