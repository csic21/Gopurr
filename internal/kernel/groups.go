package kernel

import (
	"sort"

	"go.yaml.in/yaml/v3"

	"gopurr/internal/core"
)

func configuredGroupOrder(raw []byte) []string {
	var cfg struct {
		Groups []struct {
			Name string `yaml:"name"`
		} `yaml:"proxy-groups"`
	}
	if yaml.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	names := make([]string, 0, len(cfg.Groups))
	for _, group := range cfg.Groups {
		names = append(names, group.Name)
	}
	return names
}

// The controller returns a map. Restore config order, with a stable fallback
// for built-in groups or groups missing from the available order.
func sortGroups(groups []core.Group, names []string) {
	order := make(map[string]int, len(names))
	for i, name := range names {
		if _, exists := order[name]; !exists {
			order[name] = i
		}
	}
	sort.SliceStable(groups, func(i, j int) bool {
		iPos, iKnown := order[groups[i].Name]
		jPos, jKnown := order[groups[j].Name]
		if iKnown != jKnown {
			return iKnown
		}
		if iKnown {
			return iPos < jPos
		}
		return groups[i].Name < groups[j].Name
	})
}
