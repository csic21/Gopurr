package core

import (
	"fmt"
	"net/netip"
	"unsafe"

	"golang.org/x/sys/windows"
)

func checkTunRoutes(index int, expected []netip.Prefix) error {
	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(windows.AF_UNSPEC, &table); err != nil {
		return fmt.Errorf("读取 TUN 路由失败: %w", err)
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))
	var prefixes []netip.Prefix
	for _, row := range table.Rows() {
		if row.InterfaceIndex == uint32(index) && row.Protocol != windows.MIB_IPPROTO_LOCAL && row.Loopback == 0 {
			var addr netip.Addr
			raw := &row.DestinationPrefix.Prefix
			switch raw.Family {
			case windows.AF_INET:
				addr = netip.AddrFrom4((*windows.RawSockaddrInet4)(unsafe.Pointer(raw)).Addr)
			case windows.AF_INET6:
				addr = netip.AddrFrom16((*windows.RawSockaddrInet6)(unsafe.Pointer(raw)).Addr)
			default:
				continue
			}
			prefixes = append(prefixes, netip.PrefixFrom(addr, int(row.DestinationPrefix.PrefixLength)).Masked())
		}
	}
	return checkRouteCoverage(expected, prefixes)
}
