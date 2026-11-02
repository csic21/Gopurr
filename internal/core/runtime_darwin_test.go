package core

import (
	"net/netip"
	"syscall"
	"testing"

	"golang.org/x/net/route"
)

func TestDarwinRouteObservationRejectsForeignDownAndBlackholeRoutes(t *testing.T) {
	message := func(index, flags int, host byte) *route.RouteMessage {
		addresses := make([]route.Addr, syscall.RTAX_MAX)
		addresses[syscall.RTAX_DST] = &route.Inet4Addr{IP: [4]byte{192, 0, 2, host}}
		addresses[syscall.RTAX_NETMASK] = &route.Inet4Addr{IP: [4]byte{255, 255, 255, 255}}
		return &route.RouteMessage{Index: index, Flags: flags, Addrs: addresses}
	}
	messages := []route.Message{message(3, syscall.RTF_UP|syscall.RTF_HOST, 1), message(4, syscall.RTF_UP, 2),
		message(3, 0, 3), message(3, syscall.RTF_UP|syscall.RTF_BLACKHOLE, 4)}
	got := darwinRoutePrefixes(messages, 3)
	if len(got) != 1 || got[0] != netip.MustParsePrefix("192.0.2.1/32") {
		t.Fatalf("observed routes: %v", got)
	}
}
