package patch

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"github.com/metacubex/mihomo/component/trie"
	"github.com/metacubex/mihomo/config"
	"github.com/metacubex/mihomo/dns"

	"gopurr/internal/localize"
)

type DNSSettings struct {
	Enable            bool     `json:"enable" yaml:"enable"`
	EnhancedMode      string   `json:"enhancedMode" yaml:"enhanced-mode"`
	IPv6              bool     `json:"ipv6" yaml:"ipv6"`
	Nameserver        []string `json:"nameserver" yaml:"nameserver"`
	DefaultNameserver []string `json:"defaultNameserver" yaml:"default-nameserver"`
	Fallback          []string `json:"fallback" yaml:"fallback"`
	FakeIPFilter      []string `json:"fakeIPFilter" yaml:"fake-ip-filter"`
}

type SnifferSettings struct {
	Enable              bool     `json:"enable" yaml:"enable"`
	OverrideDestination bool     `json:"overrideDestination" yaml:"override-destination"`
	ForceDNSMapping     bool     `json:"forceDNSMapping" yaml:"force-dns-mapping"`
	ParsePureIP         bool     `json:"parsePureIP" yaml:"parse-pure-ip"`
	HTTPPorts           []string `json:"httpPorts" yaml:"-"`
	TLSPorts            []string `json:"tlsPorts" yaml:"-"`
	QUICPorts           []string `json:"quicPorts" yaml:"-"`
	SkipDomain          []string `json:"skipDomain" yaml:"skip-domain"`
	ForceDomain         []string `json:"forceDomain" yaml:"force-domain"`
	SkipSrcAddress      []string `json:"skipSrcAddress" yaml:"skip-src-address"`
	SkipDstAddress      []string `json:"skipDstAddress" yaml:"skip-dst-address"`
}

func DefaultDNSSettings() DNSSettings {
	return DNSSettings{Enable: true, EnhancedMode: "fake-ip",
		Nameserver: []string{"1.1.1.1", "223.5.5.5"}, DefaultNameserver: []string{"1.1.1.1", "223.5.5.5"}}
}

func DefaultSnifferSettings() SnifferSettings {
	return SnifferSettings{Enable: true, ForceDNSMapping: true, ParsePureIP: true,
		HTTPPorts: []string{"80", "8080-8880"}, TLSPorts: []string{"443", "8443"}, QUICPorts: []string{"443"}}
}

// FeatureSettings reads the composed configuration for display and for seeding
// a custom draft. Defaults fill omitted form fields, without enabling a feature
// absent from the configuration. No resolver, socket or external source is used.
func FeatureSettings(raw []byte) (DNSSettings, SnifferSettings, error) {
	cfg, err := config.UnmarshalRawConfig(raw)
	if err != nil {
		return DefaultDNSSettings(), DefaultSnifferSettings(), fmt.Errorf(localize.Text("读取 DNS 与嗅探配置失败：%w"), err)
	}
	d, s := cfg.DNS, cfg.Sniffer
	dnsSettings := DNSSettings{Enable: d.Enable, EnhancedMode: d.EnhancedMode.String(), IPv6: d.IPv6,
		Nameserver: d.NameServer, DefaultNameserver: d.DefaultNameserver, Fallback: d.Fallback, FakeIPFilter: d.FakeIPFilter}
	sniffer := SnifferSettings{Enable: s.Enable, OverrideDestination: s.OverrideDest, ForceDNSMapping: s.ForceDnsMapping,
		ParsePureIP: s.ParsePureIp, SkipDomain: s.SkipDomain, ForceDomain: s.ForceDomain,
		SkipSrcAddress: s.SkipSrcAddress, SkipDstAddress: s.SkipDstAddress}
	setPorts := func(name string, ports []string) {
		// An empty port list in mihomo matches every port.
		if len(ports) == 0 {
			ports = []string{"1-65535"}
		}
		switch strings.ToUpper(name) {
		case "HTTP":
			sniffer.HTTPPorts = ports
		case "TLS":
			sniffer.TLSPorts = ports
		case "QUIC":
			sniffer.QUICPorts = ports
		}
	}
	if len(s.Sniff) > 0 {
		for name, protocol := range s.Sniff {
			setPorts(name, protocol.Ports)
		}
	} else if len(s.Sniffing) > 0 {
		for _, name := range s.Sniffing {
			setPorts(name, s.Ports)
		}
	} else {
		// Offer a usable starting point when enabling a previously unconfigured sniffer.
		defaults := DefaultSnifferSettings()
		sniffer.HTTPPorts, sniffer.TLSPorts, sniffer.QUICPorts = defaults.HTTPPorts, defaults.TLSPorts, defaults.QUICPorts
	}
	return dnsSettings, sniffer, nil
}

func applyNetworkFeatures(doc map[string]any, ov Overrides) error {
	if d := ov.DNS; d != nil {
		if err := d.Validate(); err != nil {
			return err
		}
		if ov.TunEnable && !d.Enable {
			return fmt.Errorf("%s", localize.Text("TUN 模式需要内置 DNS，请先关闭 TUN，再关闭 DNS"))
		}
		current := configSection(doc, "dns")
		current["enable"], current["enhanced-mode"], current["ipv6"] = d.Enable, d.EnhancedMode, d.IPv6
		current["nameserver"], current["default-nameserver"] = nonNilStrings(d.Nameserver), nonNilStrings(d.DefaultNameserver)
		current["fallback"], current["fake-ip-filter"] = nonNilStrings(d.Fallback), nonNilStrings(d.FakeIPFilter)
	}
	if s := ov.Sniffer; s != nil {
		if err := s.Validate(); err != nil {
			return err
		}
		current := configSection(doc, "sniffer")
		current["enable"], current["override-destination"] = s.Enable, s.OverrideDestination
		current["force-dns-mapping"], current["parse-pure-ip"] = s.ForceDNSMapping, s.ParsePureIP
		current["skip-domain"], current["force-domain"] = nonNilStrings(s.SkipDomain), nonNilStrings(s.ForceDomain)
		current["skip-src-address"], current["skip-dst-address"] = sniffAddressRanges(s.SkipSrcAddress), sniffAddressRanges(s.SkipDstAddress)
		sniff := map[string]any{}
		for name, ports := range map[string][]string{"HTTP": s.HTTPPorts, "TLS": s.TLSPorts, "QUIC": s.QUICPorts} {
			if len(ports) > 0 {
				// Set a per-protocol value too: inherited protocol overrides must
				// not silently defeat the visible destination switch.
				sniff[name] = map[string]any{"ports": ports, "override-destination": s.OverrideDestination}
			}
		}
		current["sniff"] = sniff
		delete(current, "sniffing")
		delete(current, "port-whitelist")
	}
	return nil
}

func configSection(doc map[string]any, key string) map[string]any {
	section, _ := doc[key].(map[string]any)
	if section == nil {
		section = map[string]any{}
		doc[key] = section
	}
	return section
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func sniffAddressRanges(values []string) []string {
	ranges := make([]string, 0, len(values))
	for _, value := range values {
		if addr, err := netip.ParseAddr(value); err == nil {
			value = netip.PrefixFrom(addr, addr.BitLen()).String()
		}
		ranges = append(ranges, value)
	}
	return ranges
}

func (d DNSSettings) Validate() error {
	if d.EnhancedMode != "fake-ip" && d.EnhancedMode != "redir-host" {
		return fmt.Errorf("%s", localize.Text("DNS 模式须为 fake-ip 或 redir-host"))
	}
	if d.Enable && len(d.Nameserver) == 0 {
		return fmt.Errorf("%s", localize.Text("请至少填写一个 DNS 解析服务器"))
	}
	if d.Enable && len(d.DefaultNameserver) == 0 {
		return fmt.Errorf("%s", localize.Text("请至少填写一个引导 DNS"))
	}
	for _, field := range []struct {
		name      string
		values    []string
		bootstrap bool
	}{
		{localize.Text("解析服务器"), d.Nameserver, false}, {localize.Text("引导 DNS"), d.DefaultNameserver, true}, {localize.Text("备用 DNS"), d.Fallback, false},
	} {
		for _, value := range field.values {
			if err := validateDNSServer(value, field.bootstrap); err != nil {
				return fmt.Errorf("%s：%w", field.name, err)
			}
		}
	}
	return nil
}

func validateDNSServer(value string, bootstrap bool) error {
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, " \t\r\n") || value == "" {
		return fmt.Errorf(localize.Text("无效的服务器地址 %q"), value)
	}
	// Use the kernel's parser to accept the same schemes and # parameters.
	// Parsing constructs descriptors only; it does not send DNS requests.
	servers, err := dns.ParseNameServer([]string{value})
	if err != nil || len(servers) != 1 {
		return fmt.Errorf(localize.Text("无效的 DNS 地址 %q：%v"), value, err)
	}
	server := servers[0]
	_, port, splitErr := net.SplitHostPort(server.Addr)
	if splitErr != nil && server.Net == "https" {
		if u, err := url.Parse(server.Addr); err == nil {
			port = u.Port()
		}
	}
	if port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf(localize.Text("DNS 端口须为 1–65535：%s"), value)
		}
	}
	if bootstrap && server.Net != "system" {
		host, _, err := net.SplitHostPort(server.Addr)
		if err != nil {
			if u, err := url.Parse(server.Addr); err == nil {
				host = u.Hostname()
			}
		}
		if _, err := netip.ParseAddr(host); err != nil {
			return fmt.Errorf(localize.Text("引导 DNS 的服务器必须使用 IP 地址或 system：%s"), value)
		}
	}
	return nil
}

// ParseSniffPorts accepts a comma-separated set of ports or inclusive ranges.
// An empty list disables that protocol, rather than selecting every port.
func ParseSniffPorts(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var result []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		bounds := strings.Split(part, "-")
		if len(bounds) > 2 {
			return nil, fmt.Errorf("%s", localize.Text("端口须为 1–65535 的整数或范围，例如 443、8080-8880"))
		}
		first, last := 0, 0
		for i, bound := range bounds {
			for _, c := range bound {
				if c < '0' || c > '9' {
					return nil, fmt.Errorf(localize.Text("端口格式无效：%s"), part)
				}
			}
			port, err := strconv.Atoi(bound)
			if err != nil || port < 1 || port > 65535 {
				return nil, fmt.Errorf(localize.Text("端口范围应为 1–65535：%s"), part)
			}
			if i == 0 {
				first = port
			}
			last = port
		}
		if first > last {
			return nil, fmt.Errorf(localize.Text("端口范围起点不能大于终点：%s"), part)
		}
		result = append(result, part)
	}
	return result, nil
}

func (s SnifferSettings) Validate() error {
	if s.Enable && len(s.HTTPPorts)+len(s.TLSPorts)+len(s.QUICPorts) == 0 {
		return fmt.Errorf("%s", localize.Text("请至少为一种嗅探协议填写端口"))
	}
	for _, field := range []struct {
		name  string
		ports []string
	}{{"HTTP", s.HTTPPorts}, {"TLS", s.TLSPorts}, {"QUIC", s.QUICPorts}} {
		if _, err := ParseSniffPorts(strings.Join(field.ports, ",")); err != nil {
			return fmt.Errorf("%s：%w", field.name, err)
		}
	}
	for _, field := range []struct {
		name   string
		values []string
	}{{localize.Text("跳过来源地址"), s.SkipSrcAddress}, {localize.Text("跳过目标地址"), s.SkipDstAddress}} {
		for _, value := range field.values {
			if strings.HasPrefix(strings.ToLower(value), "geoip:") || strings.HasPrefix(strings.ToLower(value), "rule-set:") {
				_, reference, _ := strings.Cut(value, ":")
				if strings.TrimSpace(reference) == "" {
					return fmt.Errorf(localize.Text("%s的规则引用不能为空"), field.name)
				}
				continue
			}
			if _, err := netip.ParsePrefix(value); err != nil {
				if addr, err := netip.ParseAddr(value); err != nil || addr.Zone() != "" {
					return fmt.Errorf(localize.Text("%s须为 IP 或 CIDR 网段：%s"), field.name, value)
				}
			}
		}
	}
	for _, field := range []struct {
		name   string
		values []string
	}{{localize.Text("跳过域名嗅探"), s.SkipDomain}, {localize.Text("强制域名嗅探"), s.ForceDomain}} {
		builder := &trie.DomainSetBuilder{}
		for _, value := range field.values {
			if strings.HasPrefix(strings.ToLower(value), "geosite:") || strings.HasPrefix(strings.ToLower(value), "rule-set:") {
				_, reference, _ := strings.Cut(value, ":")
				if strings.TrimSpace(reference) == "" {
					return fmt.Errorf(localize.Text("%s的规则引用不能为空"), field.name)
				}
				continue
			}
			if err := builder.Insert(value); err != nil {
				return fmt.Errorf("%s：%w", field.name, err)
			}
		}
	}
	return nil
}
