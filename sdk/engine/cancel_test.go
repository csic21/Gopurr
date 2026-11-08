package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gopurr/internal/profile"
)

func TestCancelPendingInterruptsImportWithoutCommitting(t *testing.T) {
	for _, streamBody := range []bool{false, true} {
		t.Run(fmt.Sprintf("streamBody=%v", streamBody), func(t *testing.T) {
			// Import cancellation happens before config validation or kernel start.
			// An isolated store keeps this independent of mihomo's global owner.
			store, err := profile.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			e := &Engine{store: store}
			started := make(chan struct{})
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if streamBody {
					_, _ = io.WriteString(w, "mode: rule\n")
					w.(http.Flusher).Flush()
				}
				close(started)
				select {
				case <-r.Context().Done():
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)
			params, _ := json.Marshal(map[string]string{"name": "Cancelled import", "url": server.URL})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				_, err := e.Call(ctx, "profiles.import", params)
				result <- err
			}()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("subscription request did not reach the local server")
			}

			cancelled := make(chan struct{})
			go func() { e.CancelPending(); close(cancelled) }()
			select {
			case <-cancelled:
			case <-time.After(time.Second):
				t.Fatal("cancelling waited for the engine's network lock")
			}
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled import returned %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("subscription download continued after cancellation")
			}
			if len(store.List()) != 0 || store.ActiveID() != "" {
				t.Fatal("cancelled import committed a profile")
			}
			e.cancelMu.Lock()
			pending := len(e.activeCancels)
			e.cancelMu.Unlock()
			if pending != 0 {
				t.Fatalf("completed call left %d cancellation registrations", pending)
			}
			// A stop or status call queued after cancellation must still work.
			if _, err := e.Call(context.Background(), "profiles.list", nil); err != nil {
				t.Fatalf("new call inherited cancellation: %v", err)
			}
		})
	}
}

func TestCancelPendingCancelsAllRegisteredCalls(t *testing.T) {
	e := &Engine{}
	first, unregisterFirst := e.registerCall(context.Background())
	defer unregisterFirst()
	second, unregisterSecond := e.registerCall(context.Background())
	defer unregisterSecond()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.CancelPending()
	if first.Err() != context.Canceled || second.Err() != context.Canceled {
		t.Fatal("cancellation did not reach every registered context")
	}
	third, unregisterThird := e.registerCall(context.Background())
	defer unregisterThird()
	if third.Err() != nil {
		t.Fatal("cancellation affected a later registration")
	}
}
