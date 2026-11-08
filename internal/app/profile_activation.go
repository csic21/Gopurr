package app

import (
	"context"
	"fmt"

	"gopurr/internal/workflow"
)

// activateManagedProfile compiles an explicit source before publishing Active.
// The same workflow is used by legacy desktop actions and native RPC. A failed
// index write after runtime acceptance restores the previous running state.
func (a *App) activateManagedProfile(ctx context.Context, id string) error {
	candidate, err := a.configForProfile(id, a.profileOverrides(id))
	if err != nil {
		return fmt.Errorf("%s%w", tr("配置无法使用: "), err)
	}
	running := a.k != nil && a.k.RunningForChange()
	var previous []byte
	if running {
		previous, err = a.lastAcceptedConfig()
		if err != nil {
			return err
		}
	}
	change := workflow.ConfigChange{Candidate: candidate, Commit: func(ctx context.Context) error { return a.store.SetActiveContext(ctx, id) }}
	if a.k != nil {
		if !a.ensureMixedPortAvailable() {
			return fmt.Errorf(tr("端口 %d 已被占用（可能是另一个代理客户端），换一个端口再试"), a.mixedPort())
		}
		change.Apply = func(_ context.Context, raw []byte) error {
			a.networkGeneration.Add(1)
			if err := a.k.Start(raw); err != nil {
				return fmt.Errorf("%s%w", tr("启动失败: "), err)
			}
			return nil
		}
		change.Restore = func(context.Context) error {
			if running {
				return a.k.Start(previous)
			}
			return a.k.Stop()
		}
	}
	result, err := workflow.RunConfigChange(ctx, change)
	if err == nil && result.Applied {
		a.cancelDelayTest()
		a.bmu.Lock()
		a.delays = map[string]uint16{}
		a.delayErrors = map[string]string{}
		a.bmu.Unlock()
	}
	return err
}
