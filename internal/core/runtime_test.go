package core

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestMixedProbeRequiresSOCKSGreeting(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reply  []byte
		wantOK bool
	}{
		{"mixed", []byte{5, 0}, true},
		{"authentication required", []byte{5, 2}, false},
		{"other TCP server", []byte("HTTP/1.1 200 OK\r\n"), false},
		{"closed listener", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				var greeting [3]byte
				_, _ = io.ReadFull(conn, greeting[:])
				_, _ = conn.Write(tc.reply)
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			err = ProbeMixed(ctx, listener.Addr().(*net.TCPAddr).Port)
			if (err == nil) != tc.wantOK {
				t.Fatalf("probe error = %v", err)
			}
		})
	}
}
