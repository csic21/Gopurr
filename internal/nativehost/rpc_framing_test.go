package nativehost

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A complete reply must reach the caller even while its peer is still tearing
// down the connection. Waiting for EOF here strands activation/quit polling.
func TestCompleteReplyDoesNotWaitForPeerClose(t *testing.T) {
	root := ""
	if runtime.GOOS == "darwin" {
		root = "/tmp"
	}
	dir, err := os.MkdirTemp(root, "gp-frame-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "rpc")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	release := make(chan struct{})
	defer close(release)
	go func() {
		c, err := listener.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		var request Request
		if json.NewDecoder(c).Decode(&request) != nil {
			return
		}
		_ = json.NewEncoder(c).Encode(Response{Result: map[string]bool{"ready": true}})
		<-release
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	reply, err := Call(ctx, path, strings.Repeat("x", 64), "host.ping", nil)
	if err != nil || !strings.Contains(string(reply), `"ready":true`) {
		t.Fatalf("complete framed reply was blocked by peer teardown: %s %v", reply, err)
	}
}
