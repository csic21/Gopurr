package core

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServiceRestartRequiresAuthenticationAndAnExplicitCapability(t *testing.T) {
	dir, err := os.MkdirTemp("", "gp-restart-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "core.sock")
	restarted := make(chan struct{}, 1)
	server, err := serveController(socket, filepath.Join(dir, "backend.sock"), "fixture-token", newGeoUpdater(dir), func() { restarted <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	for _, tc := range []struct {
		token  string
		status int
	}{{"", 401}, {"wrong", 401}, {"fixture-token", 204}} {
		request, _ := http.NewRequest(http.MethodPost, "http://localhost/gopurr/restart", nil)
		request.Header.Set("Authorization", "Bearer "+tc.token)
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != tc.status {
			t.Fatalf("restart status: %d", response.StatusCode)
		}
	}
	select {
	case <-restarted:
	case <-time.After(time.Second):
		t.Fatal("authorized restart was not requested")
	}
	select {
	case <-restarted:
		t.Fatal("unauthorized request restarted service")
	default:
	}
}
