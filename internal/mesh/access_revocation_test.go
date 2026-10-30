package mesh

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/metacubex/tailscale/client/local"
	"github.com/metacubex/tailscale/ipn"
)

func TestPendingAllowCannotOverrideRevocation(t *testing.T) {
	for _, revoke := range []string{"block", "device", "services", "disconnect", "cancel"} {
		t.Run(revoke, func(t *testing.T) {
			b := NewEmbedded(t.TempDir(), nil)
			entered, release := make(chan struct{}), make(chan struct{})
			var grants atomic.Int32
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/localapi/v0/status" {
					io.WriteString(w, `{"BackendState":"Running"}`)
					return
				}
				var p ipn.MaskedPrefs
				if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
					t.Error(err)
					return
				}
				if !p.ShieldsUp && grants.Add(1) == 1 {
					close(entered)
					<-release
				}
				json.NewEncoder(w).Encode(p.Prefs)
			}))
			defer api.Close()
			nctx, stop := context.WithCancel(context.Background())
			defer stop()
			b.node = &nodeSession{ctx: nctx, cancel: stop, lc: &local.Client{Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "tcp", strings.TrimPrefix(api.URL, "http://"))
			}}}
			b.policy.Services = []Service{{Protocol: "tcp", Port: 22}}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- b.SetIncoming(ctx, true) }()
			<-entered
			switch revoke {
			case "block":
				b.BlockNow()
			case "device":
				b.BlockDeviceNow("peer-A")
			case "services":
				if err := b.SetServices(ctx, "3389/tcp"); err != nil {
					t.Error(err)
				}
			case "disconnect":
				if err := b.Disconnect(ctx); err != nil {
					t.Error(err)
				}
			case "cancel":
				cancel()
			}
			close(release)
			if err := <-done; err == nil {
				t.Error("stale authorization reported success")
			}
			b.mu.Lock()
			opened := b.allow
			b.mu.Unlock()
			if opened {
				t.Fatal("pending allow reopened local service access after revocation")
			}
			if revoke != "disconnect" {
				if err := b.SetIncoming(context.Background(), true); err != nil {
					t.Fatalf("fresh explicit authorization failed after revocation: %v", err)
				}
				b.mu.Lock()
				opened = b.allow
				b.mu.Unlock()
				if !opened {
					t.Fatal("fresh explicit authorization did not reopen access")
				}
			}
		})
	}
}
