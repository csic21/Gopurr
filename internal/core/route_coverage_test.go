package core

import (
	"net/netip"
	"testing"

	LC "github.com/metacubex/mihomo/listener/config"
)

func prefixes(values ...string) []netip.Prefix {
	var out []netip.Prefix
	for _, value := range values {
		out = append(out, netip.MustParsePrefix(value))
	}
	return out
}

func TestTunRequiresCompletePlannedRouteCoverage(t *testing.T) {
	c := LC.Tun{Inet4Address: prefixes("198.18.0.1/30"), Inet6Address: prefixes("fdfe:dcba:9876::1/126")}
	expected, err := expectedTunRoutes(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, observed := range [][]netip.Prefix{
		prefixes("198.18.0.0/30"),     // connected subnet alone
		prefixes("0.0.0.0/1", "::/0"), // half the IPv4 space missing
		prefixes("0.0.0.0/0"),         // IPv6 missing
	} {
		if checkRouteCoverage(expected, observed) == nil {
			t.Fatalf("incomplete routes accepted: %v", observed)
		}
	}
	if err := checkRouteCoverage(expected, prefixes("0.0.0.0/1", "128.0.0.0/1", "::/1", "8000::/1")); err != nil {
		t.Fatal(err)
	}
}

func TestTunRouteCoverageRespectsCustomRangesExclusionsAndLegacyKeys(t *testing.T) {
	c := LC.Tun{Inet4Address: prefixes("198.18.0.1/30"), RouteAddress: prefixes("203.0.113.0/24"),
		Inet4RouteAddress: prefixes("192.0.2.1/32"), RouteExcludeAddress: prefixes("203.0.113.128/25")}
	expected, err := expectedTunRoutes(c)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRouteCoverage(expected, prefixes("203.0.113.0/25", "192.0.2.1/32", "198.18.0.0/30")); err != nil {
		t.Fatal(err)
	}
	if checkRouteCoverage(expected, prefixes("203.0.113.0/25", "198.18.0.0/30")) == nil {
		t.Fatal("legacy host route was not checked")
	}
}
