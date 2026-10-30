package mesh

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/metacubex/tailscale/client/local"
)

func TestSessionRoutesSurviveOutageAndClearOnExplicitDisconnect(t *testing.T) {
	var state atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch state.Load() {
		case 1:
			io.WriteString(w, `{"BackendState":"Stopped"}`)
		case 2:
			http.Error(w, "temporarily unavailable", 503)
		case 3:
			io.WriteString(w, `{"BackendState":"Running","Peer":{}}`)
		default:
			io.WriteString(w, `{"BackendState":"Running","Peer":{"nodekey:0000000000000000000000000000000000000000000000000000000000000000":{"ID":"peer-A","TailscaleIPs":["100.70.0.2","fd7a:115c:a1e0::2"]}}}`)
		}
	}))
	defer api.Close()
	b := NewEmbedded(t.TempDir(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n := &nodeSession{ctx: ctx, cancel: cancel, lc: &local.Client{Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(api.URL, "http://"))
	}}}
	var dialed atomic.Int32
	n.dial = func(context.Context, string, string) (net.Conn, error) {
		dialed.Add(1)
		return nil, errors.New("test encrypted transport")
	}
	if err := b.startBridge(n); err != nil {
		t.Fatal(err)
	}
	defer b.Disconnect(ctx)
	connected, err := b.Read(ctx)
	if err != nil || len(connected.RouteIPs) != 2 || connected.ProxyAddress == "" {
		t.Fatalf("missing session routes: %+v %v", connected, err)
	}
	for _, mode := range []int32{1, 0, 2, 0, 3, 0} {
		state.Store(mode)
		s, err := b.Read(ctx)
		if (err != nil) != (mode == 2) {
			t.Fatalf("unexpected read result in mode %d: %v", mode, err)
		}
		if !slices.Equal(s.RouteIPs, connected.RouteIPs) || s.ProxyAddress != connected.ProxyAddress || s.ProxyPassword != connected.ProxyPassword {
			t.Fatalf("outage removed session guards in mode %d", mode)
		}
		before := dialed.Load()
		_, _ = b.dialPeer(n, ctx, "tcp", "100.70.0.2:22")
		if (dialed.Load() > before) != (mode == 0) {
			t.Fatalf("bridge did not respect current peer availability in mode %d", mode)
		}
	}
	// Snapshot callers must not be able to mutate the session's protected set.
	connected.RouteIPs[0] = netip.MustParseAddr("100.70.0.99")
	s, _ := b.Read(ctx)
	if slices.Contains(s.RouteIPs, connected.RouteIPs[0]) {
		t.Fatal("mutable route snapshot escaped")
	}
	if err := b.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	s, err = b.Read(ctx)
	if err != nil || len(s.RouteIPs) != 0 || s.ProxyAddress != "" || s.ProxyPassword != "" {
		t.Fatalf("explicit disconnect retained session routing: %+v %v", s, err)
	}
}
