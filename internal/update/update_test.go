package update

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type fakeBackend struct {
	check func(context.Context) (*Release, error)
	calls atomic.Int32
}

func (*fakeBackend) Enabled() bool { return true }
func (b *fakeBackend) Check(ctx context.Context) (*Release, error) {
	b.calls.Add(1)
	return b.check(ctx)
}

func await(t *testing.T, c *Controller, phase Phase) State {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state := c.Snapshot()
		if state.Phase == phase {
			return state
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("phase = %s, want %s", c.Snapshot().Phase, phase)
	return State{}
}

func TestCheckDoesNotInstallAndCoalescesRequests(t *testing.T) {
	finish := make(chan struct{})
	var installs atomic.Int32
	b := &fakeBackend{check: func(ctx context.Context) (*Release, error) {
		select {
		case <-finish:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return &Release{Version: "0.2.0", Notes: "New release", Install: func(context.Context, func(int64, int64)) error { installs.Add(1); return nil }}, nil
	}}
	c := New(b, nil)
	defer c.Close()
	c.Check(false)
	c.Check(true)
	close(finish)
	s := await(t, c, Available)
	if b.calls.Load() != 1 || installs.Load() != 0 || !s.Visible || s.Version != "0.2.0" {
		t.Fatalf("unexpected state: %+v", s)
	}
	c.Install()
	await(t, c, Ready)
	if installs.Load() != 1 {
		t.Fatal("install did not run once")
	}
	c.Check(true)
	if c.Snapshot().Phase != Ready || b.calls.Load() != 1 {
		t.Fatal("recheck discarded staged update")
	}
}

func TestFailedSignatureCanBeRetriedWithoutClaimingSuccess(t *testing.T) {
	var installs atomic.Int32
	b := &fakeBackend{check: func(context.Context) (*Release, error) {
		return &Release{Version: "0.2.0", Install: func(ctx context.Context, progress func(int64, int64)) error {
			progress(50, 100)
			if installs.Add(1) == 1 {
				return errors.New("invalid update signature")
			}
			return nil
		}}, nil
	}}
	c := New(b, nil)
	defer c.Close()
	c.Check(true)
	await(t, c, Available)
	c.Install()
	s := await(t, c, Available)
	if s.Error != "invalid update signature" {
		t.Fatalf("install failure hidden: %+v", s)
	}
	c.Install()
	await(t, c, Ready)
}

func TestCancelDownloadPreservesUpdateForRetry(t *testing.T) {
	started := make(chan struct{})
	b := &fakeBackend{check: func(context.Context) (*Release, error) {
		return &Release{Version: "0.2.0", Install: func(ctx context.Context, _ func(int64, int64)) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		}}, nil
	}}
	c := New(b, nil)
	defer c.Close()
	c.Check(true)
	await(t, c, Available)
	c.Install()
	<-started
	c.Cancel()
	if s := await(t, c, Available); s.Error != "" || s.Version != "0.2.0" {
		t.Fatalf("cancel lost update: %+v", s)
	}
}

func TestCheckFailureAndCurrentVersion(t *testing.T) {
	var calls atomic.Int32
	b := &fakeBackend{check: func(context.Context) (*Release, error) {
		if calls.Add(1) == 1 {
			return nil, errors.New("network unavailable")
		}
		return nil, nil
	}}
	c := New(b, nil)
	defer c.Close()
	c.Check(false)
	if s := await(t, c, Failed); s.Visible || s.Error == "" {
		t.Fatalf("background check interrupted the user: %+v", s)
	}
	c.Check(true)
	if s := await(t, c, Current); !s.Visible || s.LastCheck.IsZero() || s.Error != "" {
		t.Fatalf("current version state: %+v", s)
	}
}

func TestCloseCancelsInFlightCheck(t *testing.T) {
	done := make(chan struct{})
	b := &fakeBackend{check: func(ctx context.Context) (*Release, error) { <-ctx.Done(); close(done); return nil, ctx.Err() }}
	c := New(b, nil)
	c.Check(false)
	c.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("check survived app shutdown")
	}
}
