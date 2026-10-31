package profile

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
	"gopurr/internal/localize"
)

const (
	AggregateSelect = "Gopurr Select"
	AggregateAuto   = "Gopurr Auto"
)

type AggregateSource struct {
	ID     string `json:"id"`
	Prefix string `json:"prefix,omitempty"`
}

// AggregateSpec only references locally cached sources. Rebuilding an
// aggregate never silently changes its sources or downloads other files.
type AggregateSpec struct {
	Sources []AggregateSource `json:"sources"`
	Include string            `json:"include,omitempty"`
	Exclude string            `json:"exclude,omitempty"`
	Dedupe  bool              `json:"dedupe"`
}

type AggregateStats struct {
	Input      int
	Output     int
	Duplicates int
	Names      []string
}

func ValidateAggregateSpec(spec AggregateSpec) error {
	if len(spec.Sources) == 0 {
		return errors.New(localize.Text("请至少选择一个聚合来源"))
	}
	if len(spec.Sources) > 64 {
		return errors.New(localize.Text("聚合来源不能超过 64 个"))
	}
	seen := map[string]bool{}
	for _, source := range spec.Sources {
		if source.ID == "" || seen[source.ID] {
			return errors.New(localize.Text("聚合来源为空或重复"))
		}
		if len(source.Prefix) > 256 || strings.ContainsAny(source.Prefix, "\r\n\t") {
			return errors.New(localize.Text("来源前缀不能包含换行，且不能超过 256 字节"))
		}
		seen[source.ID] = true
	}
	for _, expression := range []string{spec.Include, spec.Exclude} {
		if len(expression) > 2048 {
			return errors.New(localize.Text("筛选表达式不能超过 2048 字节"))
		}
		if _, err := regexp.Compile(expression); err != nil {
			return fmt.Errorf(localize.Text("筛选表达式无效：%w"), err)
		}
	}
	return nil
}

func (s *Store) DependentAggregates(sourceID string) []Profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Profile
	for _, p := range s.Items {
		if p.Aggregate == nil {
			continue
		}
		for _, source := range p.Aggregate.Sources {
			if source.ID == sourceID {
				out = append(out, cloneProfile(p))
				break
			}
		}
	}
	return out
}

type aggregateInput struct {
	source  AggregateSource
	profile Profile
	raw     []byte
}

func (s *Store) aggregateInputs(spec AggregateSpec) ([]aggregateInput, error) {
	if err := ValidateAggregateSpec(spec); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []aggregateInput
	for _, source := range spec.Sources {
		var found *Profile
		for _, p := range s.Items {
			if p.ID == source.ID {
				copy := p
				found = &copy
				break
			}
		}
		if found == nil {
			return nil, errors.New(localize.Text("聚合来源已不存在，请重新选择"))
		}
		if found.IsAggregate() {
			return nil, errors.New(localize.Text("聚合订阅不能作为另一个聚合的来源"))
		}
		raw, err := os.ReadFile(filepath.Join(s.dir, "profiles", found.File))
		if err != nil {
			return nil, fmt.Errorf(localize.Text("无法读取聚合来源 %q：%w"), found.Name, err)
		}
		out = append(out, aggregateInput{source, *found, raw})
	}
	return out, nil
}

func (s *Store) BuildAggregate(spec AggregateSpec) ([]byte, AggregateStats, error) {
	inputs, err := s.aggregateInputs(spec)
	if err != nil {
		return nil, AggregateStats{}, err
	}
	return buildAggregate(spec, inputs)
}

type aggregateNode struct {
	sourceID, sourceName, original string
	fields                         map[string]any
}

func buildAggregate(spec AggregateSpec, inputs []aggregateInput) ([]byte, AggregateStats, error) {
	var stats AggregateStats
	include, _ := regexp.Compile(spec.Include)
	exclude, _ := regexp.Compile(spec.Exclude)
	var nodes []aggregateNode
	usedNames := map[string]bool{AggregateSelect: true, AggregateAuto: true, "DIRECT": true, "REJECT": true, "GLOBAL": true, "PASS": true, "COMPATIBLE": true, "REJECT-DROP": true}
	byOrigin := map[string]string{}
	identities := map[[32]byte]string{}
	for _, input := range inputs {
		doc, err := aggregateMapping(input.raw)
		if err != nil {
			return nil, stats, fmt.Errorf(localize.Text("聚合来源 %q 无效：%w"), input.profile.Name, err)
		}
		if value, exists := doc["proxy-providers"]; exists && value != nil {
			providers, valid := value.(map[string]any)
			if !valid || len(providers) > 0 {
				return nil, stats, fmt.Errorf(localize.Text("来源 %q 含有代理集合；当前聚合仅支持直接列出 proxies 节点的配置"), input.profile.Name)
			}
		}
		proxies, ok := doc["proxies"].([]any)
		if !ok || len(proxies) == 0 {
			return nil, stats, fmt.Errorf(localize.Text("来源 %q 没有可聚合的 proxies 节点"), input.profile.Name)
		}
		seenSource := map[string]bool{}
		for _, value := range proxies {
			fields, ok := value.(map[string]any)
			if !ok {
				return nil, stats, fmt.Errorf(localize.Text("来源 %q 中的节点必须为 YAML 映射"), input.profile.Name)
			}
			name, _ := fields["name"].(string)
			typ, _ := fields["type"].(string)
			if strings.TrimSpace(name) == "" || strings.TrimSpace(typ) == "" || seenSource[name] {
				return nil, stats, fmt.Errorf(localize.Text("来源 %q 中的节点名称或类型为空，或名称重复"), input.profile.Name)
			}
			seenSource[name], stats.Input = true, stats.Input+1
			if spec.Include != "" && !include.MatchString(name) || spec.Exclude != "" && exclude.MatchString(name) {
				continue
			}
			if len(nodes) >= 20000 {
				return nil, stats, errors.New(localize.Text("聚合结果不能超过 20000 个节点"))
			}
			origin := input.source.ID + "\x00" + name
			// Dependent nodes are kept distinct: identical dialer names from two
			// sources can refer to entirely different upstream nodes.
			_, dependent := fields["dialer-proxy"]
			identityFields := map[string]any{}
			for key, value := range fields {
				if key != "name" {
					identityFields[key] = value
				}
			}
			encoded, err := json.Marshal(identityFields)
			if err != nil {
				return nil, stats, fmt.Errorf(localize.Text("来源 %q 中的节点字段无法序列化"), input.profile.Name)
			}
			identity := sha256.Sum256(encoded)
			if existing, ok := identities[identity]; spec.Dedupe && !dependent && ok {
				byOrigin[origin], stats.Duplicates = existing, stats.Duplicates+1
				continue
			}
			base := input.source.Prefix + name
			unique := base
			for suffix := 2; usedNames[unique]; suffix++ {
				unique = fmt.Sprintf("%s (%d)", base, suffix)
			}
			usedNames[unique], byOrigin[origin] = true, unique
			if !dependent {
				identities[identity] = unique
			}
			fields["name"] = unique
			nodes = append(nodes, aggregateNode{input.source.ID, input.profile.Name, name, fields})
		}
	}
	if len(nodes) == 0 {
		return nil, stats, errors.New(localize.Text("筛选后没有节点，请调整聚合来源或筛选条件"))
	}
	var proxies []any
	var names []string
	for _, node := range nodes {
		if value, exists := node.fields["dialer-proxy"]; exists {
			dialer, ok := value.(string)
			if !ok || dialer == "" {
				return nil, stats, fmt.Errorf(localize.Text("来源 %q 的节点 %q 的 dialer-proxy 必须为非空字符串"), node.sourceName, node.original)
			}
			if replacement, ok := byOrigin[node.sourceID+"\x00"+dialer]; ok {
				node.fields["dialer-proxy"] = replacement
			} else if dialer != "DIRECT" {
				return nil, stats, fmt.Errorf(localize.Text("来源 %q 的节点 %q 依赖未包含的节点或策略组 %q"), node.sourceName, node.original, dialer)
			}
		}
		proxies = append(proxies, node.fields)
		names = append(names, node.fields["name"].(string))
	}
	if err := aggregateDependencies(proxies); err != nil {
		return nil, stats, err
	}
	result := map[string]any{
		"proxies": proxies,
		"proxy-groups": []any{
			map[string]any{"name": AggregateSelect, "type": "select", "proxies": append([]string{AggregateAuto}, names...)},
			map[string]any{"name": AggregateAuto, "type": "url-test", "proxies": names, "url": "https://www.gstatic.com/generate_204", "interval": 300},
		},
		"rules": []string{"MATCH," + AggregateSelect},
	}
	out, err := yaml.Marshal(result)
	stats.Output, stats.Names = len(nodes), names
	return out, stats, err
}

func aggregateMapping(raw []byte) (map[string]any, error) {
	d := yaml.NewDecoder(bytes.NewReader(raw))
	var node yaml.Node
	if err := d.Decode(&node); err != nil {
		return nil, err
	}
	if len(node.Content) != 1 || node.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New(localize.Text("配置必须为单个 YAML 映射"))
	}
	var doc map[string]any
	if err := node.Decode(&doc); err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := d.Decode(&extra); err != io.EOF {
		return nil, errors.New(localize.Text("配置必须为单个 YAML 映射"))
	}
	return doc, nil
}

func aggregateDependencies(proxies []any) error {
	links := map[string]string{}
	for _, value := range proxies {
		p := value.(map[string]any)
		name := p["name"].(string)
		if dep, ok := p["dialer-proxy"].(string); ok && dep != "DIRECT" {
			links[name] = dep
		}
	}
	visited := map[string]uint8{}
	for start := range links {
		if visited[start] == 2 {
			continue
		}
		var path []string
		for node := start; node != "" && visited[node] != 2; node = links[node] {
			if visited[node] == 1 {
				return fmt.Errorf(localize.Text("聚合节点 %q 的链式代理存在循环"), start)
			}
			visited[node] = 1
			path = append(path, node)
		}
		for _, node := range path {
			visited[node] = 2
		}
	}
	return nil
}

// SaveAggregate validates the candidate before touching its stored snapshot.
// The caller can apply it to a running kernel in accept, rolling that kernel
// back if this method later reports a persistence failure.
func (s *Store) SaveAggregate(id, name string, spec AggregateSpec, accept func([]byte) error) (Profile, AggregateStats, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Profile{}, AggregateStats{}, errors.New(localize.Text("请填写配置名称"))
	}
	inputs, err := s.aggregateInputs(spec)
	if err != nil {
		return Profile{}, AggregateStats{}, err
	}
	body, stats, err := buildAggregate(spec, inputs)
	if err != nil {
		return Profile{}, stats, err
	}
	if accept != nil {
		if err := accept(body); err != nil {
			return Profile{}, stats, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, input := range inputs {
		current, err := os.ReadFile(filepath.Join(s.dir, "profiles", input.profile.File))
		if err != nil || !bytes.Equal(current, input.raw) {
			return Profile{}, stats, errors.New(localize.Text("聚合来源已变化，请重新预览后保存"))
		}
	}
	index := -1
	var old Profile
	if id != "" {
		for i, p := range s.Items {
			if p.ID == id {
				index, old = i, p
				break
			}
		}
		if index < 0 || !old.IsAggregate() {
			return Profile{}, stats, errors.New(localize.Text("聚合订阅不存在，请重新选择"))
		}
	} else {
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return Profile{}, stats, err
		}
		id = "aggregate-" + hex.EncodeToString(random[:])
	}
	p := old
	p.ID, p.Name, p.Kind, p.File, p.Updated = id, name, KindAggregate, id+".yaml", time.Now()
	p.Aggregate = &AggregateSpec{Sources: append([]AggregateSource(nil), spec.Sources...), Include: spec.Include, Exclude: spec.Exclude, Dedupe: spec.Dedupe}
	path := filepath.Join(s.dir, "profiles", p.File)
	var previous []byte
	if index >= 0 {
		previous, err = os.ReadFile(path)
		if err != nil {
			return Profile{}, stats, err
		}
	}
	if err := writeContent(path, body); err != nil {
		return Profile{}, stats, err
	}
	if index >= 0 {
		s.Items[index] = p
	} else {
		s.Items = append(s.Items, p)
	}
	if err := s.save(); err != nil {
		if index >= 0 {
			s.Items[index] = old
			err = errors.Join(err, writeContent(path, previous))
		} else {
			s.Items = s.Items[:len(s.Items)-1]
			err = errors.Join(err, os.Remove(path))
		}
		return Profile{}, stats, err
	}
	return cloneProfile(p), stats, nil
}
