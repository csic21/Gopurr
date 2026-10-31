package diagnostic

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDirectAndProxyAreIndependentAndReportsAreRedacted(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer origin.Close()
	var proxyCalls atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCalls.Add(1)
		if r.URL.Hostname() != "localhost" || r.URL.Query().Get("token") != "private-value" {
			t.Errorf("wrong proxy request: %s", r.URL)
		}
		w.WriteHeader(204)
	}))
	defer proxy.Close()
	u, _ := url.Parse(proxy.URL)
	port, _ := strconv.Atoi(u.Port())
	t.Setenv("HTTP_PROXY", proxy.URL) // The direct check must still bypass it.
	var snapshots [][]Step
	address := strings.Replace(origin.URL, "127.0.0.1", "localhost", 1) + "/probe?token=private-value"
	opt := Options{URL: address, Port: port, Timeout: time.Second,
		Probe:       func(context.Context) error { return nil },
		ProxyStatus: func(context.Context) (bool, bool, error) { return false, false, nil },
		Lookup:      func(context.Context, string) ([]string, error) { return nil, errors.New("secret-error") },
		KernelDNS:   func(context.Context, string) error { return nil },
	}
	steps := Run(context.Background(), opt, func(s []Step) { snapshots = append(snapshots, s) })
	if proxyCalls.Load() != 1 || steps[5].State != Failed || steps[6].State != Passed {
		t.Fatalf("direct/proxy paths not independent: %v / %d", steps, proxyCalls.Load())
	}
	if steps[2].State != Info || steps[3].State != Failed || steps[4].State != Passed {
		t.Fatalf("resolver and proxy statuses wrong: %v", steps)
	}
	if snapshots[0][0].State != Pending {
		t.Fatal("published snapshot was mutated")
	}
	report := Report(steps)
	for _, secret := range []string{"private-value", "localhost", "secret-error", "127.0.0.1"} {
		if strings.Contains(report, secret) {
			t.Fatal("report leaked " + secret)
		}
	}
	called := false
	opt.Direct = func(ctx context.Context, address string) error {
		called = true
		if address != opt.URL {
			t.Fatal("DIRECT received wrong target")
		}
		return nil
	}
	steps = Run(context.Background(), opt, nil)
	if !called || steps[5].State != Passed || !strings.Contains(steps[5].Detail, "DIRECT") {
		t.Fatal("explicit DIRECT test was bypassed")
	}
}

func TestCancellationStopsPendingChecks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	steps := Run(ctx, Options{URL: "https://example.com", Port: 7890, Probe: func(ctx context.Context) error { cancel(); return ctx.Err() }}, nil)
	for _, step := range steps {
		if step.State != Skipped || step.Detail != "已取消" {
			t.Fatalf("cancellation: %+v", step)
		}
	}
}

func TestIPTargetSkipsDNSAndInvalidInputDoesNotProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	defer server.Close()
	_, p, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	port, _ := strconv.Atoi(p)
	steps := Run(context.Background(), Options{URL: server.URL, Port: port, Probe: func(context.Context) error { return nil }, KernelDNS: func(context.Context, string) error { t.Fatal("DNS called for IP"); return nil }}, nil)
	if steps[3].State != Skipped || steps[4].State != Skipped || steps[5].State != Passed {
		t.Fatalf("IP test: %v", steps)
	}
	steps = Run(context.Background(), Options{URL: "https://user:secret@example.com", Port: port, Probe: func(context.Context) error { t.Fatal("invalid input probed"); return nil }}, nil)
	for _, step := range steps {
		if step.State != Skipped {
			t.Fatal("invalid input was run")
		}
	}
}
