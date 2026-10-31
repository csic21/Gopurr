package kernel

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestKernelDNSDiagnosticSupportsAAAAAndAuthentication(t *testing.T) {
	dir, err := os.MkdirTemp("", "gp-dns-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "core.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-secret" {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path != "/dns/query" || r.URL.Query().Get("name") != "ipv6.example" {
			t.Error("unexpected DNS request")
		}
		if r.URL.Query().Get("type") == "AAAA" {
			w.Write([]byte(`{"Status":0,"Answer":[{"type":28}]}`))
		} else {
			w.Write([]byte(`{"Status":0}`))
		}
	})}
	go server.Serve(listener)
	defer server.Close()
	if err := NewRemote(socket, "private-secret").QueryDNS(t.Context(), "ipv6.example"); err != nil {
		t.Fatal(err)
	}
	if err := NewRemote(socket, "wrong").QueryDNS(t.Context(), "ipv6.example"); err == nil {
		t.Fatal("DNS diagnostic bypassed authentication")
	}
}
