package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Exercise the real private service policy and local grant checks while keeping
// every connection on the temporary local API used by testNode.
type reconnectEmbedded struct {
	*Embedded
	t        *testing.T
	connects int
}

func (b *reconnectEmbedded) Connect(ctx context.Context) (string, error) {
	if err := b.Disconnect(ctx); err != nil {
		return "", err
	}
	b.connects++
	testNode(b.t, b.Embedded)
	return "", nil
}

func newReconnectController(t *testing.T, dir string) (*Controller, *reconnectEmbedded) {
	t.Helper()
	b := &reconnectEmbedded{Embedded: NewEmbedded(filepath.Join(dir, "tailnet"), nil), t: t}
	c := New(b, filepath.Join(dir, "mesh.json"), nil)
	t.Cleanup(func() {
		if err := c.Shutdown(); err != nil {
			t.Error(err)
		}
		if err := b.Disconnect(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return c, b
}

func savedIncoming(t *testing.T, c *Controller) bool {
	t.Helper()
	raw, err := os.ReadFile(c.path)
	if err != nil {
		t.Fatal(err)
	}
	var d diskState
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	return d.AllowIncoming
}

func TestIncomingChoiceSurvivesRestartAndReconnect(t *testing.T) {
	for _, auto := range []bool{false, true} {
		name := "manual"
		if auto {
			name = "automatic"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			c, _ := newReconnectController(t, dir)
			c.Do("connect")
			waitIdle(t, c)
			c.SetServices("22/tcp, 3389/tcp", true)
			if s := waitIdle(t, c); s.Error != "" || s.Status.ShieldsUp || !savedIncoming(t, c) {
				t.Fatalf("explicit choice was not saved: %+v", s)
			}
			assertPrivateMeshFile(t, c.path)
			c.SetAutoConnect(auto)
			if err := c.Shutdown(); err != nil {
				t.Fatal(err)
			}
			if !savedIncoming(t, c) {
				t.Fatal("shutdown forgot incoming choice")
			}

			next, b := newReconnectController(t, dir)
			next.Start()
			s := waitIdle(t, next)
			if !auto {
				if s.Status.Running || !s.Status.ShieldsUp || b.connects != 0 || !savedIncoming(t, next) {
					t.Fatalf("manual startup changed saved choice or started network: %+v", s)
				}
				next.Do("connect")
				s = waitIdle(t, next)
			}
			if s.Error != "" || s.Status.ShieldsUp || !s.Status.Running || len(s.Status.Services) != 2 {
				t.Fatalf("connection did not restore authorized services: %+v", s)
			}
			next.Do("disconnect")
			if s := waitIdle(t, next); s.Status.Running || !s.Status.ShieldsUp {
				t.Fatal("disconnect left node open")
			}
			next.Do("connect")
			if s := waitIdle(t, next); s.Error != "" || s.Status.ShieldsUp {
				t.Fatalf("reconnect lost choice: %+v", s)
			}
		})
	}
}

func TestIncomingRevocationSurvivesRestart(t *testing.T) {
	for _, revoke := range []string{"block", "services"} {
		t.Run(revoke, func(t *testing.T) {
			dir := t.TempDir()
			c, _ := newReconnectController(t, dir)
			c.Do("connect")
			waitIdle(t, c)
			c.SetServices("22/tcp", true)
			waitIdle(t, c)
			c.SetAutoConnect(true)
			if revoke == "block" {
				c.Do("block")
			} else {
				c.SetServices("3389/tcp", false)
			}
			if s := waitIdle(t, c); s.Error != "" || !s.Status.ShieldsUp || savedIncoming(t, c) {
				t.Fatalf("revocation was not saved: %+v", s)
			}
			if err := c.Shutdown(); err != nil {
				t.Fatal(err)
			}
			next, _ := newReconnectController(t, dir)
			next.Start()
			if s := waitIdle(t, next); s.Error != "" || !s.Status.ShieldsUp {
				t.Fatalf("restart reopened revoked access: %+v", s)
			}
		})
	}
}

func TestIncomingRestoreWaitsForRunningAndDoesNotRetryFailedGrant(t *testing.T) {
	b := &fakeBackend{status: Status{Installed: true, Official: true, ShieldsKnown: true, ShieldsUp: true, State: "NeedsLogin"}}
	c := New(b, "", nil)
	c.allowIncoming, c.restoreIncoming = true, true
	if err := c.perform(context.Background(), "refresh"); err != nil {
		t.Fatal(err)
	}
	if len(b.calls) != 0 {
		t.Fatal("opened services before login")
	}
	b.status.Running, b.status.State = true, "Running"
	b.switchErr = errors.New("grant rejected")
	if err := c.perform(context.Background(), "refresh"); err == nil {
		t.Fatal("hidden restore error")
	}
	b.switchErr = nil
	if err := c.perform(context.Background(), "refresh"); err != nil {
		t.Fatal(err)
	}
	if len(b.calls) != 1 || !b.status.ShieldsUp {
		t.Fatal("refresh retried a failed grant")
	}
}

func TestIncomingGrantPersistenceFailureClosesGate(t *testing.T) {
	b := &fakeBackend{status: Status{Running: true, ShieldsKnown: true, ShieldsUp: true}}
	c := New(b, filepath.Join(t.TempDir(), "missing", "mesh.json"), nil)
	if err := c.enableIncoming(context.Background()); err == nil {
		t.Fatal("accepted an unsaved grant")
	}
	if !b.status.ShieldsUp || c.allowIncoming {
		t.Fatal("storage failure left access open")
	}
}

func TestIncomingRestoreAfterDelayedLogin(t *testing.T) {
	b := &fakeBackend{status: Status{Installed: true, Official: true, ShieldsKnown: true, ShieldsUp: true, State: "NeedsMachineAuth"}}
	c := New(b, "", nil)
	c.allowIncoming, c.restoreIncoming = true, true
	if err := c.perform(context.Background(), "refresh"); err != nil {
		t.Fatal(err)
	}
	if len(b.calls) != 0 {
		t.Fatal("opened before device approval")
	}
	b.status.Running, b.status.State = true, "Running"
	if err := c.perform(context.Background(), "refresh"); err != nil {
		t.Fatal(err)
	}
	if b.status.ShieldsUp || len(b.calls) != 1 || b.calls[0] != "allow" {
		t.Fatal("did not restore after approval")
	}
	if err := c.perform(context.Background(), "refresh"); err != nil {
		t.Fatal(err)
	}
	if len(b.calls) != 1 {
		t.Fatal("reapplied grant on every refresh")
	}
}

type delayedIncoming struct {
	*Embedded
	granted, release chan struct{}
}

func (b *delayedIncoming) SetIncoming(ctx context.Context, allow bool) error {
	err := b.Embedded.SetIncoming(ctx, allow)
	if allow && err == nil {
		close(b.granted)
		<-b.release
	}
	return err
}

func TestCancelledGrantCannotPersistOrRestoreRevokedChoice(t *testing.T) {
	for _, restoring := range []bool{false, true} {
		name := "explicit"
		if restoring {
			name = "restore"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			b := &delayedIncoming{Embedded: NewEmbedded(filepath.Join(dir, "tailnet"), nil), granted: make(chan struct{}), release: make(chan struct{})}
			testNode(t, b.Embedded)
			if err := b.SetServices(context.Background(), "22/tcp"); err != nil {
				t.Fatal(err)
			}
			c := New(b, filepath.Join(dir, "mesh.json"), nil)
			c.s.Managed, c.allowIncoming, c.restoreIncoming = true, restoring, restoring
			if err := c.saveLocked(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { c.Shutdown(); b.Disconnect(context.Background()) })
			if restoring {
				c.Refresh()
			} else {
				c.Do("allow")
			}
			select {
			case <-b.granted:
			case <-time.After(2 * time.Second):
				t.Fatal("grant did not start")
			}
			c.Do("block")
			close(b.release)
			if s := waitIdle(t, c); !s.Status.ShieldsUp || savedIncoming(t, c) {
				t.Fatalf("stale grant survived revocation: %+v", s)
			}
			next, _ := newReconnectController(t, dir)
			next.Do("connect")
			if s := waitIdle(t, next); !s.Status.ShieldsUp {
				t.Fatal("restart restored revoked choice")
			}
		})
	}
}
