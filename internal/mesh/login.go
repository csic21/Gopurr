package mesh

import "gopurr/internal/localize"

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/metacubex/tailscale/ipn/ipnstate"
)

type loginClient interface {
	Status(context.Context) (*ipnstate.Status, error)
	StartLoginInteractive(context.Context) error
}

// Starting tsnet only starts its asynchronous login. Keep the connect action
// pending until the browser has somewhere to go, or the saved identity is ready.
func waitForLogin(ctx context.Context, client loginClient) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	requested := false
	for {
		s, err := client.Status(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return loginContextError(ctx)
			}
			return fmt.Errorf(localize.Text("无法读取组网登录状态：%w"), err)
		}
		if s.BackendState == "Running" || s.BackendState == "NeedsMachineAuth" {
			return nil
		}
		if s.AuthURL != "" {
			if !ValidAuthURL(s.AuthURL) {
				return errors.New(localize.Text("登录地址未通过验证，请重新连接"))
			}
			return nil
		}
		if s.BackendState == "NeedsLogin" && !requested {
			if err := client.StartLoginInteractive(ctx); err != nil {
				if ctx.Err() != nil {
					return loginContextError(ctx)
				}
				return fmt.Errorf(localize.Text("无法发起 Tailscale 登录：%w"), err)
			}
			requested = true
		}
		select {
		case <-ctx.Done():
			return loginContextError(ctx)
		case <-ticker.C:
		}
	}
}

func loginContextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New(localize.Text("连接 Tailscale 超时，请检查网络或代理设置后重试"))
	}
	return ctx.Err()
}
