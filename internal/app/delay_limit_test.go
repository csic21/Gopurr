package app

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gopurr/internal/kernel"
	"gopurr/internal/remote"
)

func TestDelayReachesTheNodePastOneHundred(t *testing.T) {
	dir, err := os.MkdirTemp("", "gopurr-delay-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	manager, err := kernel.NewManager(dir)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	seen := map[string]int{}
	inflight, maxInflight := 0, 0
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	t.Cleanup(finish)

	members := make([]string, 0, 103)
	members = append(members, "DIRECT", "n-001")
	for i := 1; i <= 101; i++ {
		members = append(members, fmt.Sprintf("n-%03d", i))
	}
	body := `{"proxies":{"节点选择":{"type":"Selector","now":"n-001","all":["` + strings.Join(members, `","`) + `"]}}}`

	ln, err := net.Listen("unix", filepath.Join(dir, "core.sock"))
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/version":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == "/proxies":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		case strings.HasPrefix(r.URL.Path, "/proxies/") && strings.HasSuffix(r.URL.Path, "/delay"):
			raw := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/proxies/"), "/delay")
			name, err := url.PathUnescape(raw)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			seen[name]++
			inflight++
			if inflight > maxInflight {
				maxInflight = inflight
			}
			mu.Unlock()
			select {
			case <-release:
			case <-r.Context().Done():
			}
			mu.Lock()
			inflight--
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"delay":5}`))
		default:
			http.NotFound(w, r)
		}
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	prefs := defaultPrefs()
	prefs.TestConcurrency = 32
	a := &App{k: kernel.New(manager), prefs: prefs}
	names := []string{"DIRECT", "节点选择", "n-001"}
	for i := 1; i <= 101; i++ {
		names = append(names, fmt.Sprintf("n-%03d", i))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	var listed map[string]remote.DelayResult
	go func() {
		var err error
		listed, err = webHost{a}.Delay(ctx, "", names)
		done <- err
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		n := maxInflight
		mu.Unlock()
		if n >= 32 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("concurrency stayed at %d", n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	finish()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	mu.Lock()
	got := seen["n-101"]
	direct := seen["DIRECT"]
	group := seen["节点选择"]
	peak := maxInflight
	mu.Unlock()
	if got != 1 || direct != 0 || group != 0 || peak != 32 || listed["n-101"].Delay != 5 || listed["n-101"].Error != "" {
		t.Fatalf("node 101 calls=%d direct=%d group=%d peak=%d result=%+v", got, direct, group, peak, listed["n-101"])
	}

	mu.Lock()
	seen = map[string]int{}
	mu.Unlock()
	groupResults, err := webHost{a}.Delay(ctx, "节点选择", nil)
	if err != nil {
		t.Fatal(err)
	}
	if groupResults["n-101"].Delay != 5 || groupResults["n-101"].Error != "" {
		t.Fatalf("group test result = %+v", groupResults["n-101"])
	}
	if _, skipped := groupResults["DIRECT"]; skipped {
		t.Fatal("built-in node was tested")
	}
	mu.Lock()
	groupCalls := seen["n-101"]
	mu.Unlock()
	if groupCalls != 1 {
		t.Fatalf("group path called node 101 %d times", groupCalls)
	}
}
