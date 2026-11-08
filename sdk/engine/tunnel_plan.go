package engine

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"gopurr/internal/profile"
)

const TunnelPlanVersion = 1

// AppScope describes which Android packages enter the system-created VPN.
// The host always excludes its own package, including in include mode.
type AppScope struct {
	Mode     string   `json:"mode"`
	Packages []string `json:"packages"`
}

// TunnelPlan is the single versioned source for Android Builder and Go's TUN
// configuration. Hosts return its token when attaching the resulting descriptor;
// they cannot substitute arbitrary network settings through the JSON API.
type TunnelPlan struct {
	Version     int      `json:"version"`
	Token       string   `json:"token"`
	MTU         int      `json:"mtu"`
	Addresses   []string `json:"addresses"`
	Routes      []string `json:"routes"`
	DNSServers  []string `json:"dnsServers"`
	IPv6        bool     `json:"ipv6"`
	AppScope    AppScope `json:"appScope"`
	ExcludeSelf bool     `json:"excludeSelf"`
}

type pendingTunnelPlan struct {
	plan      TunnelPlan
	profileID string
	revision  string
	prepared  []byte
	mode      string
}

var packageName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)*$`)

func defaultScope() AppScope { return AppScope{Mode: "all", Packages: []string{}} }

func normalizeScope(scope AppScope) (AppScope, error) {
	if scope.Mode == "" {
		scope.Mode = "all"
	}
	if scope.Mode != "all" && scope.Mode != "include" && scope.Mode != "exclude" {
		return AppScope{}, failure("invalid_params", "应用范围无效")
	}
	if len(scope.Packages) > 256 {
		return AppScope{}, failure("invalid_params", "最多选择 256 个应用")
	}
	packages := []string{}
	for _, name := range scope.Packages {
		name = strings.TrimSpace(name)
		if len(name) > 256 || !packageName.MatchString(name) {
			return AppScope{}, failure("invalid_params", "应用包名无效")
		}
		packages = append(packages, name)
	}
	slices.Sort(packages)
	scope.Packages = slices.Compact(packages)
	if scope.Mode == "all" {
		scope.Packages = []string{}
	}
	if scope.Mode == "include" && len(scope.Packages) == 0 {
		return AppScope{}, failure("invalid_params", "请至少选择一个经过 VPN 的应用")
	}
	return scope, nil
}

func cloneScope(scope AppScope) AppScope {
	scope.Packages = append([]string{}, scope.Packages...)
	return scope
}
func clonePlan(plan TunnelPlan) TunnelPlan {
	plan.Addresses = slices.Clone(plan.Addresses)
	plan.Routes = slices.Clone(plan.Routes)
	plan.DNSServers = slices.Clone(plan.DNSServers)
	plan.AppScope = cloneScope(plan.AppScope)
	return plan
}

func newTunnelPlan(scope AppScope) (TunnelPlan, error) {
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		return TunnelPlan{}, err
	}
	return TunnelPlan{Version: TunnelPlanVersion, Token: hex.EncodeToString(random[:]), MTU: 1500,
		Addresses: []string{"198.18.0.1/30", "fdfe:dcba:9876::1/126"}, Routes: []string{"0.0.0.0/0", "::/0"}, DNSServers: []string{"198.18.0.2", "fdfe:dcba:9876::2"}, IPv6: true, AppScope: cloneScope(scope), ExcludeSelf: true}, nil
}

func profileRevision(p profile.Profile, raw []byte) string {
	hash := sha256.New()
	_, _ = hash.Write(raw)
	overrides, _ := json.Marshal(p.Overrides)
	_, _ = hash.Write(overrides)
	return hex.EncodeToString(hash.Sum(nil))
}

func (e *Engine) tunnelPlanLocked(scope AppScope) (TunnelPlan, error) {
	scope, err := normalizeScope(scope)
	if err != nil {
		return TunnelPlan{}, err
	}
	id := e.store.ActiveID()
	if id == "" {
		return TunnelPlan{}, failure("no_profile", "请先导入并选择配置")
	}
	p, err := e.store.Get(id)
	if err != nil {
		return TunnelPlan{}, err
	}
	raw, err := e.store.Content(id)
	if err != nil {
		return TunnelPlan{}, err
	}
	prepared, err := e.prepareLocked(raw, p)
	if err != nil {
		return TunnelPlan{}, err
	}
	plan, err := newTunnelPlan(scope)
	if err != nil {
		return TunnelPlan{}, err
	}
	e.pendingPlan = &pendingTunnelPlan{plan: plan, profileID: id, revision: profileRevision(p, raw), prepared: prepared, mode: e.mode}
	e.scope = scope
	return clonePlan(plan), nil
}

func (e *Engine) validatePendingPlanLocked(token string) (*pendingTunnelPlan, error) {
	if e.pendingPlan == nil || token == "" || token != e.pendingPlan.plan.Token {
		return nil, failure("stale_plan", "VPN 配置已变化，请重新准备连接")
	}
	item, err := e.store.Get(e.store.ActiveID())
	if err != nil {
		return nil, failure("stale_plan", "VPN 配置已变化，请重新准备连接")
	}
	raw, err := e.store.Content(item.ID)
	if err != nil {
		return nil, err
	}
	if e.pendingPlan.profileID != item.ID || e.pendingPlan.revision != profileRevision(item, raw) || e.pendingPlan.mode != e.mode {
		return nil, failure("stale_plan", "VPN 配置已变化，请重新准备连接")
	}
	return e.pendingPlan, nil
}
