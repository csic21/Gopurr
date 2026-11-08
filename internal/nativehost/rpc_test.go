package nativehost

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPrivateSocketAuthenticationAndEnvelope(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "gp-rpc-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "rpc")
	token := strings.Repeat("a", 64)
	calls := 0
	s, err := Listen(path, token, func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		calls++
		if method == "fail" {
			return nil, errors.New("expected failure")
		}
		return map[string]any{"method": method, "params": json.RawMessage(params)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("socket permission: %v %v", info, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	b, err := Call(ctx, path, "wrong", "echo", json.RawMessage(`{}`))
	if err != nil || !strings.Contains(string(b), "unauthorized") || calls != 0 {
		t.Fatalf("unauthenticated call reached handler: %s %v", b, err)
	}
	b, err = Call(ctx, path, token, "echo", json.RawMessage(`{"hello":"world"}`))
	if err != nil || !strings.Contains(string(b), `"hello":"world"`) || calls != 1 {
		t.Fatalf("call: %s %v", b, err)
	}
	b, err = Call(ctx, path, token, "fail", nil)
	if err != nil || !strings.Contains(string(b), "expected failure") {
		t.Fatalf("error envelope: %s %v", b, err)
	}
}

func TestCloseInterruptsIncompleteUnauthenticatedRequest(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "gp-close-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "rpc")
	s, err := Listen(path, strings.Repeat("x", 64), func(context.Context, string, json.RawMessage) (any, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_, _ = c.Write([]byte(`{"token":`))
	done := make(chan struct{})
	go func() { _ = s.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown blocked on an incomplete request")
	}
}

func TestEventsRetryAndBoundedRetention(t *testing.T) {
	e := &Events{}
	for i := 0; i < 140; i++ {
		e.Add("show", i)
	}
	all := e.Since(0)
	if len(all) != 128 || all[0].ID != 13 || all[127].ID != 140 {
		t.Fatalf("bad retention: %v", all)
	}
	if got := e.Since(139); len(got) != 1 || got[0].ID != 140 {
		t.Fatalf("retry cursor: %v", got)
	}
	if len(e.Since(140)) != 0 {
		t.Fatal("acknowledged event replayed")
	}
}

func TestClientDisconnectCancelsHandler(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "gp-cancel-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	entered, canceled := make(chan struct{}), make(chan struct{})
	server, err := Listen(filepath.Join(dir, "rpc"), strings.Repeat("x", 64), func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Call(ctx, filepath.Join(dir, "rpc"), strings.Repeat("x", 64), "wait", nil)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("client did not cancel")
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("server kept disconnected work running")
	}
}
func TestSocketRefusesSharedParentWithoutChangingPermissions(t *testing.T) {
	dir := t.TempDir()
	public := filepath.Join(dir, "shared")
	if err := os.Mkdir(public, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(public, 0755); err != nil {
		t.Fatal(err)
	}
	server, err := Listen(filepath.Join(public, "rpc"), strings.Repeat("x", 64), func(context.Context, string, json.RawMessage) (any, error) { return nil, nil })
	if err == nil {
		server.Close()
		t.Fatal("created bearer socket in shared directory")
	}
	info, _ := os.Stat(public)
	if info.Mode().Perm() != 0755 {
		t.Fatal("modified shared directory permissions")
	}
}
func TestTokenRequiresPrivateRegularBoundedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "auth")
	value := strings.Repeat("a", 64)
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadToken(path)
	if err != nil || got != value {
		t.Fatal("valid private token rejected", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadToken(path); err == nil {
		t.Fatal("public token accepted")
	}
	os.Chmod(path, 0600)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadToken(link); err == nil {
		t.Fatal("token symlink accepted")
	}
	for _, invalid := range []string{"short", strings.Repeat("a", 4097), strings.Repeat("a", 63) + "\n"} {
		os.WriteFile(path, []byte(invalid), 0600)
		if _, err := ReadToken(path); err == nil {
			t.Fatal("malformed token accepted")
		}
	}
}
func TestHomeLockExcludesDuplicateModelAndReleases(t *testing.T) {
	home := t.TempDir()
	first, err := LockHome(home)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := LockHome(home); !errors.Is(err, ErrHomeInUse) {
		if second != nil {
			second.Close()
		}
		t.Fatal("same home was not excluded", err)
	}
	other, err := LockHome(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := LockHome(home)
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
	info, err := os.Stat(filepath.Join(home, "desktop.lock"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("lock file must remain private and stable", err)
	}
}
func TestHomeLockIsReleasedByProcessExit(t *testing.T) {
	if os.Getenv("GOPURR_TEST_HOME_LOCK_CHILD") == "1" {
		lock, err := LockHome(os.Getenv("GOPURR_TEST_HOME_LOCK_DIR"))
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		fmt.Println("LOCKED")
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	home := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHomeLockIsReleasedByProcessExit$")
	command.Env = append(os.Environ(), "GOPURR_TEST_HOME_LOCK_CHILD=1", "GOPURR_TEST_HOME_LOCK_DIR="+home)
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.ProcessState == nil {
			command.Process.Kill()
			command.Wait()
		}
	}()
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "LOCKED\n" {
		t.Fatal("lock child not ready", line, err)
	}
	if lock, err := LockHome(home); !errors.Is(err, ErrHomeInUse) {
		if lock != nil {
			lock.Close()
		}
		t.Fatal("second process could lock the home", err)
	}
	// Only this dedicated test child is killed; it never creates an app/core.
	command.Process.Kill()
	command.Wait()
	lock, err := LockHome(home)
	if err != nil {
		t.Fatal("crashed process left a stale home lock", err)
	}
	lock.Close()
}

func TestBackendSessionCleanupPreservesUnrelatedFilesAndRejectsChangedToken(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "gp-clean-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	tokenPath := filepath.Join(dir, "auth")
	socketPath := filepath.Join(dir, "rpc")
	token := strings.Repeat("a", 64)
	if err := os.WriteFile(tokenPath, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(dir, "keep.log")
	os.WriteFile(keep, []byte("fixture"), 0600)
	server, err := Listen(socketPath, token, func(context.Context, string, json.RawMessage) (any, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	server.Close()
	os.WriteFile(tokenPath, []byte(strings.Repeat("b", 64)), 0600)
	if err := CleanupSession(socketPath, tokenPath, token); err == nil {
		t.Fatal("removed a replaced credential")
	}
	if _, err := os.Stat(tokenPath); err != nil {
		t.Fatal("foreign token removed")
	}
	os.WriteFile(tokenPath, []byte(token), 0600)
	if err := CleanupSession(socketPath, tokenPath, token); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tokenPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned credential left behind", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("unrelated session files removed", err)
	}
	os.Remove(keep)
	os.WriteFile(tokenPath, []byte(token), 0600)
	if err := CleanupSession(socketPath, tokenPath, token); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("empty private session directory retained", err)
	}
}

func TestServerClosePreservesReplacedSocketPath(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "gp-socket-owner-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "rpc")
	server, err := Listen(path, strings.Repeat("x", 64), func(context.Context, string, json.RawMessage) (any, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		server.Close()
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement fixture"), 0600); err != nil {
		server.Close()
		t.Fatal(err)
	}
	server.Close()
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "replacement fixture" {
		t.Fatal("server removed an object it did not own", err)
	}
}

func TestHomeLockRefusesSharedRootsWithoutChangingPermissions(t *testing.T) {
	for _, path := range []string{os.TempDir(), "/tmp", "/"} {
		before, err := os.Stat(path)
		if err != nil {
			continue
		}
		lock, err := LockHome(path)
		if err == nil {
			lock.Close()
			t.Fatal("accepted a shared directory as application home")
		}
		after, statErr := os.Stat(path)
		if statErr != nil || after.Mode() != before.Mode() {
			t.Fatal("shared directory permissions changed", statErr)
		}
	}
}
