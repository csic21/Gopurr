// Package configyaml updates configuration maps without reordering existing
// YAML keys or rewriting untouched scalar values.
package configyaml

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"sort"

	"go.yaml.in/yaml/v3"
)

// Marshal applies values to the original document. Overlays provide the key
// order and scalar spelling for recursively merged user YAML.
func Marshal(raw []byte, values map[string]any, overlays ...[]byte) ([]byte, error) {
	root, err := parse(raw)
	if err != nil {
		return nil, err
	}
	for _, overlay := range overlays {
		addition, err := parse(overlay)
		if err != nil {
			return nil, err
		}
		merge(root, addition)
	}
	root, err = reconcile(root, values)
	if err != nil {
		return nil, err
	}
	return yaml.Marshal(root)
}

func parse(raw []byte) (*yaml.Node, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, nil
	}
	d := yaml.NewDecoder(bytes.NewReader(raw))
	var doc yaml.Node
	if err := d.Decode(&doc); err == io.EOF {
		return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}, nil
	} else if err != nil {
		return nil, err
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("配置必须是 YAML 键值映射")
	}
	var values map[string]any
	if err := doc.Decode(&values); err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := d.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("配置只能包含一个 YAML 文档")
	}
	return expand(doc.Content[0]), nil
}

// Resolve aliases and merge keys before editing. Keeping a merge key could
// otherwise reintroduce a management endpoint deliberately removed by the app.
func expand(node *yaml.Node) *yaml.Node {
	if node.Kind == yaml.AliasNode {
		return expand(node.Alias)
	}
	out := *node
	out.Anchor, out.Alias, out.Content = "", nil, nil
	if node.Kind == yaml.MappingNode {
		for i := 0; i < len(node.Content); i += 2 {
			if node.Content[i].Tag != "!!merge" {
				continue
			}
			merged := expand(node.Content[i+1])
			bases := []*yaml.Node{merged}
			if merged.Kind == yaml.SequenceNode {
				bases = merged.Content
			}
			for _, base := range bases {
				for j := 0; j < len(base.Content); j += 2 {
					if index(&out, base.Content[j].Value) < 0 {
						out.Content = append(out.Content, base.Content[j], base.Content[j+1])
					}
				}
			}
		}
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if key.Tag == "!!merge" {
				continue
			}
			if at := index(&out, key.Value); at >= 0 {
				out.Content[at+1] = expand(value)
			} else {
				out.Content = append(out.Content, expand(key), expand(value))
			}
		}
	} else {
		for _, child := range node.Content {
			out.Content = append(out.Content, expand(child))
		}
	}
	return &out
}

func index(node *yaml.Node, key string) int {
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return i
		}
	}
	return -1
}

func merge(dst, src *yaml.Node) {
	for i := 0; i < len(src.Content); i += 2 {
		key, value := src.Content[i], src.Content[i+1]
		if at := index(dst, key.Value); at >= 0 {
			current := dst.Content[at+1]
			if current.Kind == yaml.MappingNode && value.Kind == yaml.MappingNode {
				merge(current, value)
			} else {
				dst.Content[at+1] = value
			}
		} else {
			dst.Content = append(dst.Content, key, value)
		}
	}
}

func reconcile(original *yaml.Node, value any) (*yaml.Node, error) {
	var old any
	if err := original.Decode(&old); err == nil && reflect.DeepEqual(old, value) {
		return original, nil
	}
	if values, ok := value.(map[string]any); ok && original.Kind == yaml.MappingNode {
		out := *original
		out.Content = nil
		seen := make(map[string]bool)
		for i := 0; i < len(original.Content); i += 2 {
			key := original.Content[i]
			if next, ok := values[key.Value]; ok {
				child, err := reconcile(original.Content[i+1], next)
				if err != nil {
					return nil, err
				}
				out.Content = append(out.Content, key, child)
				seen[key.Value] = true
			}
		}
		var added []string
		for key := range values {
			if !seen[key] {
				added = append(added, key)
			}
		}
		sort.Strings(added)
		for _, key := range added {
			var k, v yaml.Node
			if err := k.Encode(key); err != nil {
				return nil, err
			}
			if err := v.Encode(values[key]); err != nil {
				return nil, err
			}
			out.Content = append(out.Content, &k, &v)
		}
		return &out, nil
	}
	var out yaml.Node
	if err := out.Encode(value); err != nil {
		return nil, err
	}
	return &out, nil
}
