package patch

import "gopurr/internal/localize"

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

const meshDirect = "__GOPURR_TAILNET__"

// Exact node routes enter the embedded authenticated AEAD bridge. They must
// remain inside Gopurr TUN; excluding them would send traffic onto the LAN.
func applyMeshRoutes(doc map[string]any, ov Overrides) error {
	if len(ov.MeshIPs) == 0 || ov.MeshProxyAddress == "" {
		return nil
	}
	var rules []any
	seen := map[netip.Addr]bool{}
	for _, raw := range ov.MeshIPs {
		ip, err := netip.ParseAddr(raw)
		if err != nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || seen[ip] {
			continue
		}
		// Only accept official Tailscale address ranges. fd7a is ULA and is
		// checked separately below because netip reports it as private.
		if !netip.MustParsePrefix("100.64.0.0/10").Contains(ip) {
			continue
		}
		seen[ip] = true
		prefix := netip.PrefixFrom(ip, 32).String()
		rules = append(rules, "IP-CIDR,"+prefix+","+meshDirect+",no-resolve")
	}
	for _, raw := range ov.MeshIPs {
		ip, err := netip.ParseAddr(raw)
		if err != nil || seen[ip] || !netip.MustParsePrefix("fd7a:115c:a1e0::/48").Contains(ip) {
			continue
		}
		seen[ip] = true
		prefix := netip.PrefixFrom(ip, 128).String()
		rules = append(rules, "IP-CIDR6,"+prefix+","+meshDirect+",no-resolve")
	}
	if len(rules) == 0 {
		return nil
	}
	proxies, _ := doc["proxies"].([]any)
	for _, item := range proxies {
		if p, ok := item.(map[string]any); ok && p["name"] == meshDirect {
			return fmt.Errorf(localize.Text("订阅使用了组网保留的出口名称 %s，请重命名"), meshDirect)
		}
	}
	host, portText, err := net.SplitHostPort(ov.MeshProxyAddress)
	port, portErr := strconv.Atoi(portText)
	if err != nil || portErr != nil || host != "127.0.0.1" || port < 1 || port > 65535 || len(ov.MeshProxyPassword) < 32 {
		return fmt.Errorf("%s", localize.Text("内置组网桥接地址或凭据无效"))
	}
	direct := map[string]any{"name": meshDirect, "type": "ss", "server": host, "port": port,
		"cipher": "chacha20-ietf-poly1305", "password": ov.MeshProxyPassword, "udp": true}
	doc["proxies"] = append(proxies, direct)
	existing, _ := doc["rules"].([]any)
	doc["rules"] = append(rules, existing...)
	return nil
}
