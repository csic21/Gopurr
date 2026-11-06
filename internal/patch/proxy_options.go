package patch

import (
	"bytes"
	"encoding/json"
)

// MergeProxyOptions applies a recursive merge patch without aliasing the source.
// Deletions are explicit, so an unedited credential or nested transport option
// continues to follow subscription refreshes.
func MergeProxyOptions(base, changes map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(changes))
	for key, value := range base {
		out[key] = cloneProxyOption(value)
	}
	for key, value := range changes {
		if value == nil {
			delete(out, key)
		} else if nested, ok := value.(map[string]any); ok {
			previous, _ := out[key].(map[string]any)
			out[key] = MergeProxyOptions(previous, nested)
		} else {
			out[key] = cloneProxyOption(value)
		}
	}
	return out
}

func cloneProxyOption(value any) any {
	switch v := value.(type) {
	case map[string]any:
		return MergeProxyOptions(v, nil)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = cloneProxyOption(v[i])
		}
		return out
	default:
		return v
	}
}

func DiffProxyOptions(base, next map[string]any) map[string]any {
	out := map[string]any{}
	for key := range base {
		if _, ok := next[key]; !ok {
			out[key] = nil
		}
	}
	for key, value := range next {
		old, exists := base[key]
		left, _ := json.Marshal(old)
		right, _ := json.Marshal(value)
		if exists && bytes.Equal(left, right) {
			continue
		}
		oldMap, wasMap := old.(map[string]any)
		if newMap, ok := value.(map[string]any); ok && wasMap {
			out[key] = DiffProxyOptions(oldMap, newMap)
		} else {
			out[key] = cloneProxyOption(value)
		}
	}
	return out
}
