package kernel

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"gopurr/internal/core"
)

func TestStartSkipsOnlyVerifiedIdenticalRuntimeAndManualReloadStillApplies(t *testing.T) {
	for _, kind := range []string{"same", "different", "unready", "old-service"} {
		t.Run(kind, func(t *testing.T) {
			raw := []byte("mixed-port: 7890\ntun: {enable: false}\n")
			var reloads atomic.Int32
			manager, _ := uxController(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/version":
					_, _ = w.Write([]byte(`{"version":"test"}`))
				case "/configs":
					reloads.Add(1)
					w.WriteHeader(204)
				case "/gopurr/runtime":
					st := core.RuntimeStatus{MixedPort: 7890, ProxyReady: true}
					if kind != "old-service" {
						st.ConfigDigest = core.ConfigDigest(raw)
					}
					if kind == "different" {
						st.ConfigDigest = core.ConfigDigest([]byte("other"))
					}
					if kind == "unready" && reloads.Load() == 0 {
						st.ProxyReady = false
					}
					_ = json.NewEncoder(w).Encode(st)
				default:
					w.WriteHeader(404)
				}
			}))
			k := New(manager)
			if err := k.Start(raw); err != nil {
				t.Fatal(err)
			}
			expected := int32(1)
			if kind == "same" {
				expected = 0
			}
			if reloads.Load() != expected {
				t.Fatalf("reload count = %d", reloads.Load())
			}
			if err := k.Reload(raw); err != nil {
				t.Fatal(err)
			}
			if reloads.Load() != expected+1 {
				t.Fatal("explicit reload was skipped")
			}
		})
	}
}
