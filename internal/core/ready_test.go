package core

import (
	"net"
	"testing"
	"time"
)

func TestMixedPortFromDoc(t *testing.T) {
	if got := mixedPortFromDoc(nil); got != 0 {
		t.Fatalf("nil doc = %d", got)
	}
	if got := mixedPortFromDoc(map[string]any{"mixed-port": 7890}); got != 7890 {
		t.Fatalf("int = %d", got)
	}
	if got := mixedPortFromDoc(map[string]any{"mixed-port": int64(9050)}); got != 9050 {
		t.Fatalf("int64 = %d", got)
	}
	if got := mixedPortFromDoc(map[string]any{"mixed-port": float64(1080)}); got != 1080 {
		t.Fatalf("float64 = %d", got)
	}
	if got := mixedPortFromDoc(map[string]any{"mode": "rule"}); got != 0 {
		t.Fatalf("missing = %d", got)
	}
	if got := mixedPortFromDoc(map[string]any{"mixed-port": 0}); got != 0 {
		t.Fatalf("zero = %d", got)
	}
	if got := mixedPortFromDoc(map[string]any{"mixed-port": "9050"}); got != 9050 {
		t.Fatalf("string = %d", got)
	}
}

func TestWaitLocalTCPNonPositive(t *testing.T) {
	if err := waitLocalTCP("127.0.0.1:1", 0); err == nil {
		t.Fatal("non-positive timeout should error immediately")
	}
}

func TestWaitLocalTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if err := waitLocalTCP(ln.Addr().String(), time.Second); err != nil {
		t.Fatalf("listening addr should be ready: %v", err)
	}
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := closed.Addr().String()
	closed.Close()
	if err := waitLocalTCP(addr, 150*time.Millisecond); err == nil {
		t.Fatal("closed addr should time out")
	}
}
