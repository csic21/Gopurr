package patch

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopurr/internal/localize"
)

const MaxTemplateYAML = 256 << 10

// OverrideTemplate is a reusable YAML layer. References use the immutable ID;
// changing a display name never changes which subscriptions use the layer.
type OverrideTemplate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	YAML string `json:"yaml"`
}

func ValidateTemplateLibrary(library []OverrideTemplate) error {
	if len(library) > 128 {
		return fmt.Errorf("%s", localize.Text("覆写库最多保存 128 个模板"))
	}
	seen := map[string]bool{}
	total := 0
	for _, item := range library {
		if item.ID == "" || len(item.ID) > 64 || strings.IndexFunc(item.ID, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
		}) >= 0 || seen[item.ID] {
			return fmt.Errorf("%s", localize.Text("覆写模板 ID 无效或重复"))
		}
		seen[item.ID] = true
		if strings.TrimSpace(item.Name) == "" || utf8.RuneCountInString(item.Name) > 128 || strings.IndexFunc(item.Name, unicode.IsControl) >= 0 || !utf8.ValidString(item.Name) {
			return fmt.Errorf("%s", localize.Text("模板名称须为 1–128 个字符，不能包含换行"))
		}
		total += len(item.YAML)
		if len(item.YAML) > MaxTemplateYAML || total > 4<<20 || !utf8.ValidString(item.YAML) {
			return fmt.Errorf("%s", localize.Text("单个覆写模板不能超过 256 KiB，模板库总计不能超过 4 MiB"))
		}
		doc := map[string]any{}
		if _, err := mergeUserYAML(doc, item.YAML); err != nil {
			return fmt.Errorf(localize.Text("覆写模板 %q 无效：%w"), item.Name, err)
		}
		if err := validateConfigStructure(doc); err != nil {
			return fmt.Errorf(localize.Text("覆写模板 %q 无效：%w"), item.Name, err)
		}
	}
	return nil
}

func ValidateTemplateIDs(library []OverrideTemplate, ids []string) error {
	index := make(map[string]bool, len(library))
	for _, item := range library {
		index[item.ID] = true
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !index[id] {
			return fmt.Errorf("%s", localize.Text("所选覆写模板已不存在，请重新选择"))
		}
		if seen[id] {
			return fmt.Errorf("%s", localize.Text("同一作用范围不能重复应用同一个覆写模板"))
		}
		seen[id] = true
	}
	return nil
}

// ResolveTemplateYAML preserves the caller's explicit attachment order.
func ResolveTemplateYAML(library []OverrideTemplate, ids []string) ([]string, error) {
	if err := ValidateTemplateLibrary(library); err != nil {
		return nil, err
	}
	if err := ValidateTemplateIDs(library, ids); err != nil {
		return nil, err
	}
	index := make(map[string]string, len(library))
	for _, item := range library {
		index[item.ID] = item.YAML
	}
	layers := make([]string, len(ids))
	for i, id := range ids {
		layers[i] = index[id]
	}
	return layers, nil
}

func applyTemplateYAML(doc map[string]any, library []OverrideTemplate, ids []string) (bool, error) {
	// ApplyLayers validates definitions once before invoking the two scopes.
	if err := ValidateTemplateIDs(library, ids); err != nil {
		return false, err
	}
	index := make(map[string]string, len(library))
	for _, item := range library {
		index[item.ID] = item.YAML
	}
	replaced := false
	for _, id := range ids {
		raw := index[id]
		rules, err := mergeUserYAML(doc, raw)
		if err != nil {
			return false, err
		}
		replaced = replaced || rules
	}
	return replaced, nil
}
