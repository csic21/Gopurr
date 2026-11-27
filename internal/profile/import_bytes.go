package profile

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"gopurr/internal/localize"
	"gopurr/internal/patch"
)

// AddBytesChecked imports a document supplied by a native file picker or editor.
// The cache is its source: no desktop path needs to survive a mobile sandbox.
func (s *Store) AddBytesChecked(name string, body []byte, accept func(Profile, []byte) error) (Profile, error) {
	return s.AddBytesCheckedContext(context.Background(), name, body, accept)
}
func (s *Store) AddBytesCheckedContext(ctx context.Context, name string, body []byte, accept func(Profile, []byte) error) (Profile, error) {
	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	if err := validate(body); err != nil {
		return Profile{}, err
	}
	sum := sha256.Sum256(body)
	id := "local-" + hex.EncodeToString(sum[:])[:16]
	p := Profile{ID: id, Name: firstNonEmpty(strings.TrimSpace(name), "配置"), Kind: KindFile, File: id + ".yaml", Updated: time.Now()}
	return s.importCheckedContext(ctx, p, body, name != "", accept)
}

// AddDownloadedChecked commits an already downloaded HTTP subscription. Hosts
// own downloading so their lifecycle context can cancel it before any commit.
func (s *Store) AddDownloadedChecked(address, name string, body []byte, headers http.Header, accept func(Profile, []byte) error) (Profile, error) {
	return s.AddDownloadedCheckedContext(context.Background(), address, name, body, headers, accept)
}
func (s *Store) AddDownloadedCheckedContext(ctx context.Context, address, name string, body []byte, headers http.Header, accept func(Profile, []byte) error) (Profile, error) {
	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	if err := validate(body); err != nil {
		return Profile{}, err
	}
	id := idOf(address)
	p := Profile{ID: id, Name: firstNonEmpty(strings.TrimSpace(name), decodeTitle(headers.Get("profile-title")), hostOf(address)), Kind: KindURL, URL: address, File: id + ".yaml", Updated: time.Now(), Info: parseUserinfo(headers.Get("subscription-userinfo"))}
	return s.importCheckedContext(ctx, p, body, name != "", accept)
}

// BuildLocalYAML synthesizes a minimal valid Clash/mihomo YAML document for a
// pure local profile with Gopurr Select, Gopurr Auto, and MATCH,Gopurr Select.
func BuildLocalYAML(nodes []patch.ProxyNode) ([]byte, error) {
	usedNames := map[string]bool{
		AggregateSelect: true, AggregateAuto: true,
		"DIRECT": true, "REJECT": true, "REJECT-DROP": true,
		"PASS": true, "COMPATIBLE": true, "PASS-RULE": true, "GLOBAL": true,
	}
	proxies := make([]any, 0, len(nodes))
	names := make([]string, 0, len(nodes))
	for _, n := range nodes {
		typ := strings.TrimSpace(n.Type)
		if typ == "" && n.Options != nil {
			if s, ok := n.Options["type"].(string); ok {
				typ = strings.TrimSpace(s)
			}
		}
		if typ == "" {
			return nil, fmt.Errorf("%s", localize.Text("请选择节点协议"))
		}
		server := strings.TrimSpace(n.Server)
		if server == "" {
			return nil, fmt.Errorf("%s", localize.Text("服务器地址不能为空或包含空白"))
		}
		if n.Port < 1 || n.Port > 65535 {
			return nil, fmt.Errorf("%s", localize.Text("节点端口须为 1–65535"))
		}
		name := uniqueProxyNodeName(usedNames, n.Name, typ, server, n.Port)
		entry := patch.MergeProxyOptions(nil, n.Options)
		entry["name"] = name
		entry["type"] = typ
		entry["server"] = server
		entry["port"] = n.Port
		proxies = append(proxies, entry)
		names = append(names, name)
	}
	selectMembers := make([]string, 0, len(names)+2)
	selectMembers = append(selectMembers, AggregateAuto)
	selectMembers = append(selectMembers, names...)
	autoMembers := names
	if len(names) == 0 {
		selectMembers = append(selectMembers, "DIRECT")
		autoMembers = []string{"DIRECT"}
	}
	doc := map[string]any{
		"proxies": proxies,
		"proxy-groups": []any{
			map[string]any{"name": AggregateSelect, "type": "select", "proxies": selectMembers},
			map[string]any{"name": AggregateAuto, "type": "url-test", "proxies": autoMembers, "url": "https://www.gstatic.com/generate_204", "interval": 300},
		},
		"rules": []string{"MATCH," + AggregateSelect},
	}
	return yaml.Marshal(doc)
}

// CreateLocal creates a pure local (KindFile, Path == "") profile with optional
// initial nodes so users can manage nodes without any remote subscription.
func (s *Store) CreateLocal(ctx context.Context, name string, nodes []patch.ProxyNode, accept func(Profile, []byte) error) (Profile, error) {
	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	body, err := BuildLocalYAML(nodes)
	if err != nil {
		return Profile{}, err
	}
	if err := validate(body); err != nil {
		return Profile{}, err
	}
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Profile{}, err
	}
	id := "local-" + hex.EncodeToString(random[:])
	title := firstNonEmpty(strings.TrimSpace(name), localize.Text("本地节点"))
	p := Profile{
		ID:      id,
		Name:    title,
		Kind:    KindFile,
		File:    id + ".yaml",
		Updated: time.Now(),
	}
	return s.importCheckedContext(ctx, p, body, true, accept)
}

// UpdateContentContext atomically replaces the stored YAML of a profile (used
// when persisting edits directly to pure local profiles) and optionally updates
// its ProfileOverrides in the same index commit.
func (s *Store) UpdateContentContext(ctx context.Context, id string, body []byte, overrides *patch.ProfileOverrides, accept func(Profile, []byte) error) (Profile, error) {
	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	if err := validate(body); err != nil {
		return Profile{}, err
	}
	s.mu.Lock()
	var old Profile
	found := false
	for _, p := range s.Items {
		if p.ID == id {
			old, found = cloneProfile(p), true
			break
		}
	}
	if !found {
		s.mu.Unlock()
		return Profile{}, errors.New(localize.Text("没有这个订阅"))
	}
	path := filepath.Join(s.dir, "profiles", old.File)
	oldBody, err := os.ReadFile(path)
	if err != nil {
		s.mu.Unlock()
		return Profile{}, err
	}
	candidate := cloneProfile(old)
	candidate.Updated = time.Now()
	if overrides != nil {
		ov := *overrides
		candidate.Overrides = &ov
		candidate = cloneProfile(candidate)
	}
	s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	if accept != nil {
		if err := accept(cloneProfile(candidate), body); err != nil {
			return Profile{}, err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Profile{}, err
	}
	index := -1
	for i, p := range s.Items {
		if p.ID == id {
			index = i
			if !sameImportProfile(p, old) {
				return Profile{}, errors.New(localize.Text("订阅正在更新，请稍后重试"))
			}
			break
		}
	}
	if index < 0 {
		return Profile{}, errors.New(localize.Text("没有这个订阅"))
	}
	currentBody, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(oldBody, currentBody) {
		return Profile{}, errors.New(localize.Text("订阅正在更新，请稍后重试"))
	}
	if err := writeContent(path, body); err != nil {
		return Profile{}, err
	}
	if err := ctx.Err(); err != nil {
		return Profile{}, errors.Join(err, writeContent(path, oldBody))
	}
	s.Items[index] = candidate
	if err := s.save(); err != nil {
		s.Items[index] = old
		return Profile{}, errors.Join(err, writeContent(path, oldBody))
	}
	return cloneProfile(candidate), nil
}

type AggregateRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type AggregateSourceSummary struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Prefix string `json:"prefix,omitempty"`
}

type AggregateSummary struct {
	Sources []AggregateSourceSummary `json:"sources"`
	Include string                   `json:"include,omitempty"`
	Exclude string                   `json:"exclude,omitempty"`
	Dedupe  bool                     `json:"dedupe"`
}

type ProfileInspect struct {
	NodeCount           int               `json:"nodeCount"`
	LocalNodeCount      int               `json:"localNodeCount"`
	EditedNodeCount     int               `json:"editedNodeCount"`
	EditedGroupCount    int               `json:"editedGroupCount"`
	ExtraRuleCount      int               `json:"extraRuleCount"`
	TemplateCount       int               `json:"templateCount"`
	LastError           string            `json:"lastError,omitempty"`
	DependentAggregates []AggregateRef    `json:"dependentAggregates"`
	Aggregate           *AggregateSummary `json:"aggregate,omitempty"`
}

// InspectProfile computes lightweight node/overlay counts and aggregate
// relationships for UI cards without running full config compilation.
func (s *Store) InspectProfile(p Profile) ProfileInspect {
	out := ProfileInspect{
		LastError:           s.ProfileError(p.ID),
		DependentAggregates: []AggregateRef{},
	}
	baseCount := 0
	if raw, err := s.Content(p.ID); err == nil {
		var doc struct {
			Proxies []struct {
				Name string `yaml:"name"`
			} `yaml:"proxies"`
		}
		if yaml.Unmarshal(raw, &doc) == nil {
			baseCount = len(doc.Proxies)
		}
	}
	deletedBase := 0
	if p.Overrides != nil {
		out.ExtraRuleCount = len(p.Overrides.ExtraRules)
		out.TemplateCount = len(p.Overrides.TemplateIDs)
		if p.Overrides.ProxyEdits != nil {
			for _, n := range p.Overrides.ProxyEdits.Nodes {
				if n.Source == "" {
					if !n.Delete {
						out.LocalNodeCount++
					}
				} else {
					if n.Delete {
						deletedBase++
					} else {
						out.EditedNodeCount++
					}
				}
			}
			out.EditedGroupCount = len(p.Overrides.ProxyEdits.Groups)
		}
	}
	out.NodeCount = max(0, baseCount+out.LocalNodeCount-deletedBase)
	for _, dep := range s.DependentAggregates(p.ID) {
		out.DependentAggregates = append(out.DependentAggregates, AggregateRef{ID: dep.ID, Name: dep.Name})
	}
	if p.Aggregate != nil {
		summary := &AggregateSummary{
			Sources: make([]AggregateSourceSummary, 0, len(p.Aggregate.Sources)),
			Include: p.Aggregate.Include,
			Exclude: p.Aggregate.Exclude,
			Dedupe:  p.Aggregate.Dedupe,
		}
		for _, src := range p.Aggregate.Sources {
			name := src.ID
			if item, err := s.Get(src.ID); err == nil && item.Name != "" {
				name = item.Name
			}
			summary.Sources = append(summary.Sources, AggregateSourceSummary{ID: src.ID, Name: name, Prefix: src.Prefix})
		}
		out.Aggregate = summary
	}
	return out
}

