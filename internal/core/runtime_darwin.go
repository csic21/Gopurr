package core

import (
	"fmt"
	"net"
	"net/netip"
	"syscall"

	"golang.org/x/net/route"
)

func checkTunRoutes(index int, expected []netip.Prefix) error {
	rib, err := route.FetchRIB(syscall.AF_UNSPEC, route.RIBTypeRoute, 0)
	if err != nil {
		return fmt.Errorf("读取 TUN 路由失败: %w", err)
	}
	messages, err := route.ParseRIB(route.RIBTypeRoute, rib)
	if err != nil {
		return fmt.Errorf("读取 TUN 路由失败: %w", err)
	}
	return checkRouteCoverage(expected, darwinRoutePrefixes(messages, index))
}

func darwinRoutePrefixes(messages []route.Message, index int) []netip.Prefix {
	var prefixes []netip.Prefix
	for _, message := range messages {
		r, ok := message.(*route.RouteMessage)
		if !ok || r.Index != index || r.Flags&syscall.RTF_UP == 0 || r.Flags&(syscall.RTF_REJECT|syscall.RTF_BLACKHOLE) != 0 || len(r.Addrs) <= syscall.RTAX_NETMASK {
			continue
		}
		var addr netip.Addr
		var mask net.IPMask
		switch a := r.Addrs[syscall.RTAX_DST].(type) {
		case *route.Inet4Addr:
			addr = netip.AddrFrom4(a.IP)
		case *route.Inet6Addr:
			addr = netip.AddrFrom16(a.IP)
		default:
			continue
		}
		bits := 0
		if r.Flags&syscall.RTF_HOST != 0 {
			bits = addr.BitLen()
		} else {
			switch m := r.Addrs[syscall.RTAX_NETMASK].(type) {
			case *route.Inet4Addr:
				mask = net.IPMask(m.IP[:])
			case *route.Inet6Addr:
				mask = net.IPMask(m.IP[:])
			case nil:
				mask = make(net.IPMask, addr.BitLen()/8)
			default:
				continue
			}
			var size int
			bits, size = mask.Size()
			if size != addr.BitLen() {
				continue
			}
		}
		prefixes = append(prefixes, netip.PrefixFrom(addr, bits).Masked())
	}
	return prefixes
}
