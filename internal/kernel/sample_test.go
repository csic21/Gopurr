package kernel

import (
	"gopurr/internal/core"
	"testing"
	"time"
)

func TestSampleExpiresTrafficAndConnectionsIndependently(t *testing.T) {
	r := NewRemote("unused", "secret")
	r.traffic = core.Sample{UpNow: 12, DownNow: 34, UpTotal: 56, DownTotal: 78}
	r.conns = 9
	r.trafficAt = time.Now().Add(-6 * time.Second)
	r.connsAt = time.Now()
	stale := r.Sample()
	if !stale.TrafficStale || stale.UpNow != 0 || stale.DownNow != 0 || stale.UpTotal != 56 || stale.DownTotal != 78 || stale.ConnectionsStale || stale.Conns != 9 {
		t.Fatalf("stale traffic: %+v", stale)
	}
	r.trafficAt = time.Now()
	r.connsAt = time.Now().Add(-6 * time.Second)
	fresh := r.Sample()
	if fresh.TrafficStale || fresh.UpNow != 12 || !fresh.ConnectionsStale || fresh.Conns != 0 {
		t.Fatalf("stale connections: %+v", fresh)
	}
	r.SetUIActive(false)
	r.connsAt = time.Now().Add(-16 * time.Second)
	if r.Sample().ConnectionsStale {
		t.Fatal("hidden polling interval misclassified as stale")
	}
	r.SetUIActive(true)
	if !r.Sample().ConnectionsStale {
		t.Fatal("old hidden sample shown as live on reopening")
	}
}
