package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConnectionSnapshotSharesFetchAndSurvivesCanceledReader(t *testing.T) {
	var reads atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	_, r := uxController(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		reads.Add(1)
		close(started)
		<-release
		_, _ = w.Write([]byte(`{"connections":[{"id":"one","metadata":{"host":"example.com"}}]}`))
	}))
	ctx, cancel := context.WithCancel(context.Background())
	canceled := make(chan error, 1)
	go func() { _, err := r.CachedConnections(ctx); canceled <- err }()
	<-started
	next := make(chan error, 1)
	go func() {
		rows, err := r.CachedConnections(context.Background())
		if err == nil && len(rows) != 1 {
			err = errors.New("shared snapshot missing")
		}
		next <- err
	}()
	cancel()
	if err := <-canceled; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if err := <-next; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		rows, err := r.CachedConnections(context.Background())
		if err != nil || len(rows) != 1 {
			t.Fatalf("snapshot unavailable: %v", err)
		}
	}
	if reads.Load() != 1 {
		t.Fatalf("display readers issued %d requests", reads.Load())
	}
}

func TestConnectionSnapshotDiscardsReadAcrossClose(t *testing.T) {
	var reads atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	_, r := uxController(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodDelete {
			w.WriteHeader(204)
			return
		}
		if reads.Add(1) == 1 {
			close(started)
			<-release
			_, _ = w.Write([]byte(`{"connections":[{"id":"closed"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"connections":[]}`))
	}))
	result := make(chan error, 1)
	go func() {
		rows, err := r.CachedConnections(context.Background())
		if err == nil && len(rows) != 0 {
			err = errors.New("closed connection resurrected")
		}
		result <- err
	}()
	<-started
	if err := r.CloseConnection("closed"); err != nil {
		t.Fatal(err)
	}
	// Also cover a reader joining after invalidation but before the old read ends.
	other := make(chan error, 1)
	go func() {
		rows, err := r.CachedConnections(context.Background())
		if err == nil && len(rows) != 0 {
			err = errors.New("joining reader got an invalidated snapshot")
		}
		other <- err
	}()
	close(release)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := <-other; err != nil {
		t.Fatal(err)
	}
	if reads.Load() != 2 {
		t.Fatalf("expected one replacement fetch, got %d", reads.Load())
	}
}

func TestConnectionCountUsesSummaryAndFallsBackOnlyForOldServices(t *testing.T) {
	for _, status := range []int{200, 403, 404, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var summaries, full atomic.Int32
			_, r := uxController(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/gopurr/connections/count" {
					summaries.Add(1)
					w.WriteHeader(status)
					if status == 200 {
						_ = json.NewEncoder(w).Encode(map[string]int{"count": 12345})
					}
					return
				}
				full.Add(1)
				_, _ = w.Write([]byte(`{"connections":[{},{}]}`))
			}))
			for i := 0; i < 2; i++ {
				count, err := r.connectionCount()
				switch status {
				case 200:
					if err != nil || count != 12345 {
						t.Fatal("summary count lost")
					}
				case 403, 404:
					if err != nil || count != 2 {
						t.Fatal("old service fallback failed")
					}
				case 503:
					if err == nil {
						t.Fatal("outage was hidden by fallback")
					}
				}
			}
			if status == 200 && full.Load() != 0 {
				t.Fatal("count transferred full connection metadata")
			}
			if (status == 403 || status == 404) && (summaries.Load() != 1 || full.Load() != 2) {
				t.Fatal("unsupported capability was probed repeatedly")
			}
			if status == 503 && full.Load() != 0 {
				t.Fatal("server error triggered full-list fallback")
			}
		})
	}
}

func TestConnectionSnapshotHonorsReaderDeadline(t *testing.T) {
	r := NewRemote("unused", "secret")
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := r.CachedConnections(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

type countBenchmarkTransport struct{ body string }

func (t countBenchmarkTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(t.body)), Header: make(http.Header)}, nil
}

func BenchmarkConnectionCount(b *testing.B) {
	rows := make([]map[string]any, 10000)
	for i := range rows {
		rows[i] = map[string]any{"id": fmt.Sprint(i), "metadata": map[string]string{"host": fmt.Sprintf("domain-%06d.example.com", i), "destinationIP": "192.0.2.1", "destinationPort": "443", "sourceIP": "127.0.0.1", "sourcePort": "55555", "network": "tcp", "process": "Browser Helper", "processPath": "/Applications/Browser.app/Contents/Frameworks/Browser Helper.app/Contents/MacOS/Browser Helper"}, "rule": "DomainSuffix", "rulePayload": "example.com", "chains": []string{"Hong Kong 01", "Proxy"}, "upload": 123456, "download": 9876543, "start": time.Unix(1700000000, 0).UTC()}
	}
	legacy, _ := json.Marshal(map[string]any{"connections": rows})
	for _, summary := range []bool{false, true} {
		label, body := "legacy-full-list", string(legacy)
		if summary {
			label, body = "summary", `{"count":10000}`
		}
		b.Run(label, func(b *testing.B) {
			r := NewRemote("unused", "secret")
			r.countUnsupported.Store(!summary)
			r.call = &http.Client{Transport: countBenchmarkTransport{body}}
			b.ReportAllocs()
			for b.Loop() {
				if n, err := r.connectionCount(); err != nil || n != 10000 {
					b.Fatalf("count=%d err=%v", n, err)
				}
			}
		})
	}
}
