package core

import (
	"fmt"
	"net/netip"

	LC "github.com/metacubex/mihomo/listener/config"
	tun "github.com/metacubex/sing-tun"
	"go4.org/netipx"
)

// Use the route planner that creates mihomo's routes, including its macOS
// subranges, IPv6, legacy keys and explicit exclusions. A connected subnet or
// just one half of a default route cannot establish complete coverage.
func expectedTunRoutes(c LC.Tun) ([]netip.Prefix, error) {
	o := tun.Options{AutoRoute: true, Inet4Address: c.Inet4Address, Inet6Address: c.Inet6Address}
	for _, prefixes := range [][]netip.Prefix{c.RouteAddress, c.Inet4RouteAddress, c.Inet6RouteAddress} {
		for _, p := range prefixes {
			if p.Addr().Is4() {
				o.Inet4RouteAddress = append(o.Inet4RouteAddress, p)
			} else {
				o.Inet6RouteAddress = append(o.Inet6RouteAddress, p)
			}
		}
	}
	for _, prefixes := range [][]netip.Prefix{c.RouteExcludeAddress, c.Inet4RouteExcludeAddress, c.Inet6RouteExcludeAddress} {
		for _, p := range prefixes {
			if p.Addr().Is4() {
				o.Inet4RouteExcludeAddress = append(o.Inet4RouteExcludeAddress, p)
			} else {
				o.Inet6RouteExcludeAddress = append(o.Inet6RouteExcludeAddress, p)
			}
		}
	}
	return o.BuildAutoRouteRanges(false)
}

func checkRouteCoverage(expected, observed []netip.Prefix) error {
	if len(expected) == 0 {
		return fmt.Errorf("TUN 没有可接管的路由范围")
	}
	var builder netipx.IPSetBuilder
	for _, prefix := range observed {
		builder.AddPrefix(prefix)
	}
	covered, err := builder.IPSet()
	if err != nil {
		return err
	}
	for _, prefix := range expected {
		if !covered.ContainsPrefix(prefix) {
			return fmt.Errorf("TUN 自动路由未完整就绪，缺少 %s", prefix)
		}
	}
	return nil
}
