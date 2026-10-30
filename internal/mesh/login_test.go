package mesh

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/metacubex/tailscale/ipn/ipnstate"
)

type loginFixture struct {
	read  func(context.Context) (*ipnstate.Status, error)
	start func(context.Context) error
}

func (f loginFixture) Status(ctx context.Context) (*ipnstate.Status, error) { return f.read(ctx) }
func (f loginFixture) StartLoginInteractive(ctx context.Context) error      { return f.start(ctx) }

func TestLoginWaitsForDelayedAuthorizationLink(t *testing.T) {
	reads, logins := 0, 0
	client := loginFixture{
		read: func(context.Context) (*ipnstate.Status, error) {
			reads++
			s := &ipnstate.Status{BackendState: "NeedsLogin"}
			if reads == 1 {
				s.BackendState = "NoState"
			}
			if reads >= 4 {
				s.AuthURL = "https://login.tailscale.com/a/test"
			}
			return s, nil
		},
		start: func(context.Context) error { logins++; return nil },
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := waitForLogin(ctx, client); err != nil {
		t.Fatal(err)
	}
	if reads != 4 || logins != 1 {
		t.Fatalf("login returned early or restarted repeatedly: reads=%d logins=%d", reads, logins)
	}
}

func TestLoginReadyStatesDoNotRequestAnotherLogin(t *testing.T) {
	for _, state := range []string{"Running", "NeedsMachineAuth"} {
		t.Run(state, func(t *testing.T) {
			client := loginFixture{
				read:  func(context.Context) (*ipnstate.Status, error) { return &ipnstate.Status{BackendState: state}, nil },
				start: func(context.Context) error { t.Fatal("ready node was asked to log in again"); return nil },
			}
			if err := waitForLogin(context.Background(), client); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLoginTimeoutCancellationAndErrorsAreReported(t *testing.T) {
	for _, kind := range []string{"timeout", "cancel", "status", "login", "untrusted-link"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			client := loginFixture{
				read: func(context.Context) (*ipnstate.Status, error) {
					if kind == "status" {
						return nil, errors.New("status unavailable")
					}
					s := &ipnstate.Status{BackendState: "NeedsLogin"}
					if kind == "untrusted-link" {
						s.AuthURL = "https://example.com/untrusted"
					}
					return s, nil
				},
				start: func(context.Context) error {
					if kind == "cancel" {
						cancel()
					}
					if kind == "login" {
						return errors.New("login unavailable")
					}
					return nil
				},
			}
			err := waitForLogin(ctx, client)
			if err == nil {
				t.Fatal("login failure was reported as success")
			}
			want := map[string]string{"timeout": "超时", "status": "无法读取", "login": "无法发起", "untrusted-link": "未通过验证"}[kind]
			if kind == "cancel" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation lost: %v", err)
				}
			} else if !strings.Contains(err.Error(), want) {
				t.Fatalf("missing actionable error: %v", err)
			}
		})
	}
}

// Opt-in live handshake only: use a disposable identity, request an official
// browser link, and stop before account authorization. Never open a browser.
func TestEmbeddedLoginLinkIntegration(t *testing.T) {
	if os.Getenv("GOPURR_TEST_TAILSCALE_LOGIN") != "1" {
		t.Skip("set GOPURR_TEST_TAILSCALE_LOGIN=1 for the official login-link handshake")
	}
	b := NewEmbedded(t.TempDir(), nil)
	t.Cleanup(func() { _ = b.Disconnect(context.Background()) })
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	auth, err := b.Connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !ValidAuthURL(auth) {
		t.Fatal("connect finished without an official authorization link")
	}
	s, err := b.Read(ctx)
	if err != nil || !s.ShieldsKnown || !s.ShieldsUp || s.Running {
		t.Fatalf("unexpected pre-authorization state: error=%v protected=%t running=%t", err, s.ShieldsKnown && s.ShieldsUp, s.Running)
	}
}
