package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gopurr/internal/core"
	"gopurr/internal/patch"
	"gopurr/internal/profile"
)

func TestFeatureCacheTracksProfileRefreshOverridesAndExternalEdits(t *testing.T) {
	a := extensionTestApp(t)
	a.dir = filepath.Dir(a.ovPath)
	var openErr error
	a.store, openErr = profile.Open(a.dir)
	if openErr != nil {
		t.Fatal(openErr)
	}
	source := filepath.Join(t.TempDir(), "source.yaml")
	write := func(server string) {
		t.Helper()
		if err := os.WriteFile(source, []byte("dns: {enable: true, nameserver: ["+server+"]}\nrules: [\"MATCH,DIRECT\"]\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("1.1.1.1")
	p, err := a.store.AddFile(source, "test")
	if err != nil {
		t.Fatal(err)
	}
	check := func(server string) {
		t.Helper()
		dns, _, err := a.effectiveFeatures()
		if err != nil || len(dns.Nameserver) != 1 || dns.Nameserver[0] != server {
			t.Fatalf("effective DNS did not follow committed source: %v", err)
		}
	}
	check("1.1.1.1")
	write("9.9.9.9")
	if err := a.store.Refresh(p.ID); err != nil {
		t.Fatal(err)
	}
	check("9.9.9.9")
	dns := patch.DefaultDNSSettings()
	dns.Nameserver = []string{"8.8.8.8"}
	if err := a.store.SetOverrides(p.ID, patch.ProfileOverrides{DNS: &dns}); err != nil {
		t.Fatal(err)
	}
	check("8.8.8.8")
	if err := a.store.SetOverrides(p.ID, patch.ProfileOverrides{}); err != nil {
		t.Fatal(err)
	}
	check("9.9.9.9")
	cached := filepath.Join(a.dir, "profiles", p.File)
	body := []byte("dns: {enable: true, nameserver: [4.4.4.4]}\nrules: [\"MATCH,DIRECT\"]\n")
	if err := os.WriteFile(cached, body, 0600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(cached, future, future); err != nil {
		t.Fatal(err)
	}
	a.featureFileChecked = time.Now().Add(-time.Minute)
	check("4.4.4.4")
}

func TestOverviewFeatureComputationRejectsStaleSource(t *testing.T) {
	a := extensionTestApp(t)
	queued := make(chan func(), 4)
	previous := runOnMain
	runOnMain = func(fn func()) { queued <- fn }
	defer func() { runOnMain = previous }()
	config := func(ip string) string {
		return "dns: {enable: true, nameserver: [" + ip + "]}\nrules: [\"MATCH,DIRECT\"]"
	}
	a.cfgText = config("1.1.1.1")
	if _, _, _, ready := a.overviewFeatureSettings(); ready {
		t.Fatal("uncached configuration was compiled during painting")
	}
	a.cfgText = config("9.9.9.9")
	select {
	case complete := <-queued:
		complete()
	case <-time.After(3 * time.Second):
		t.Fatal("background compilation did not complete")
	}
	if !a.featureCacheAt.IsZero() {
		t.Fatal("old configuration was published over a changed source")
	}
	a.overviewFeatureSettings()
	select {
	case complete := <-queued:
		complete()
	case <-time.After(3 * time.Second):
		t.Fatal("replacement compilation did not complete")
	}
	dns, _, err, ready := a.overviewFeatureSettings()
	if err != nil || !ready || len(dns.Nameserver) != 1 || dns.Nameserver[0] != "9.9.9.9" {
		t.Fatal("current configuration was not published")
	}
}

func TestConnectionPageDoesNotWaitForController(t *testing.T) {
	a := subscriptionTestApp(t)
	queued := make(chan func(), 4)
	previous := runOnMain
	runOnMain = func(fn func()) { queued <- fn }
	defer func() { runOnMain = previous }()
	// Holding publication proves the page returns before completion, independent
	// of machine speed or the local server's scheduling.
	a.requestConnections()
	if !a.connRefreshing || !a.connFetched.IsZero() {
		t.Fatal("connection refresh did not return before publication")
	}
	a.requestConnections() // coalesced while the first refresh is pending
	select {
	case complete := <-queued:
		complete()
	case <-time.After(3 * time.Second):
		t.Fatal("background snapshot did not complete")
	}
	if a.connRefreshing || len(a.connRows) == 0 {
		t.Fatal("connection snapshot was not published")
	}
	a.connQuery = "github"
	first := a.visibleConnections()
	if len(first) != 1 {
		t.Fatal("indexed search lost host matching")
	}
	for i := 0; i < 10; i++ {
		rows := a.visibleConnections()
		if len(rows) != 1 || &rows[0] != &first[0] {
			t.Fatal("unchanged frame rebuilt filtered rows")
		}
	}
	a.connQuery = "npmjs"
	if rows := a.visibleConnections(); len(rows) != 1 || rows[0].Host == first[0].Host {
		t.Fatal("search query did not invalidate results")
	}
}

func performanceConfig(n int) string {
	var text strings.Builder
	text.WriteString("mixed-port: 7890\nproxies: []\nproxy-groups: []\nrules:\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&text, "  - DOMAIN-SUFFIX,domain-%06d.example.com,DIRECT\n", i)
	}
	return text.String()
}

func BenchmarkOverviewFeaturesUnchanged(b *testing.B) {
	for _, n := range []int{10000, 100000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			a := &App{}
			a.cfgText = performanceConfig(n)
			a.ov.MixedPort = 7890
			if _, _, err := a.effectiveFeatures(); err != nil {
				b.Fatal(err)
			}
			// Advancing wall-clock age must no longer invalidate a derived config.
			a.featureCacheAt = time.Now().Add(-time.Hour)
			b.ReportAllocs()
			for b.Loop() {
				if _, _, err := a.effectiveFeatures(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkConnectionSearchUnchanged10K(b *testing.B) {
	a := &App{}
	a.connRows = make([]core.Conn, 10000)
	a.connQuery = "example"
	for i := range a.connRows {
		a.connRows[i] = core.Conn{ID: fmt.Sprint(i), Host: fmt.Sprintf("domain-%06d.example.com", i), Process: "Browser Helper", ProcessPath: "/Applications/Browser.app/Contents/Frameworks/Browser Helper.app/Contents/MacOS/Browser Helper", Rule: "DomainSuffix", RulePayload: "example.com", Chain: "Hong Kong 01 » Proxy", Network: "tcp"}
	}
	a.visibleConnections()
	b.ReportAllocs()
	for b.Loop() {
		a.visibleConnections()
	}
}

// Pump completions on the test's own goroutine, like the native main loop.
func proxySelectionTestMain(t *testing.T, a *App) func() {
	t.Helper()
	queued := make(chan func(), 32)
	previous := runOnMain
	runOnMain = func(fn func()) { queued <- fn }
	t.Cleanup(func() { runOnMain = previous })
	return func() {
		t.Helper()
		timeout := time.After(3 * time.Second)
		for {
			select {
			case complete := <-queued:
				complete()
				continue
			default:
			}
			a.bmu.Lock()
			busy := a.proxySelecting
			a.bmu.Unlock()
			if !busy {
				return
			}
			select {
			case complete := <-queued:
				complete()
			case <-timeout:
				t.Fatal("node selection did not complete")
			}
		}
	}
}

func TestProxyCleanupHasBoundedConcurrencyAndKeepsOtherGroups(t *testing.T) {
	rows := make([]core.Conn, 40)
	for i := range rows {
		rows[i] = core.Conn{ID: fmt.Sprint(i), Chains: []string{"selected"}}
	}
	rows = append(rows, core.Conn{ID: "unrelated", Chains: []string{"other"}})
	var mu sync.Mutex
	active, peak, closed := 0, 0, 0
	failed := closeProxyConnections(context.Background(), rows, "selected", func(id string) error {
		if id == "unrelated" {
			t.Error("unrelated group connection was closed")
		}
		mu.Lock()
		active++
		peak = max(peak, active)
		closed++
		mu.Unlock()
		time.Sleep(time.Millisecond)
		mu.Lock()
		active--
		mu.Unlock()
		if id == "7" {
			return errors.New("fixture close failure")
		}
		return nil
	})
	if failed != 1 || closed != 40 || peak < 2 || peak > 8 {
		t.Fatalf("cleanup failed=%d closed=%d peak=%d", failed, closed, peak)
	}
}

func TestProxyCleanupCancellationStopsQueuedWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rows := make([]core.Conn, 40)
	for i := range rows {
		rows[i] = core.Conn{ID: fmt.Sprint(i), Chains: []string{"selected"}}
	}
	started := make(chan struct{}, 8)
	result := make(chan int, 1)
	go func() {
		result <- closeProxyConnections(ctx, rows, "selected", func(string) error { started <- struct{}{}; <-ctx.Done(); return ctx.Err() })
	}()
	<-started
	cancel()
	select {
	case failed := <-result:
		if failed != 40 {
			t.Fatalf("uncleaned connections = %d", failed)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled cleanup did not release pending work")
	}
}
