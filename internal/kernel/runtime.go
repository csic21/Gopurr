package kernel

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gopurr/internal/core"
	"gopurr/internal/localize"

	"go.yaml.in/yaml/v3"
)

func (r *Remote) Runtime(ctx context.Context) (core.RuntimeStatus, error) {
	var status core.RuntimeStatus
	err := r.requestContext(ctx, r.call, "GET", "/gopurr/runtime", nil, &status)
	return status, err
}

func (k *Kernel) Runtime(ctx context.Context) (core.RuntimeStatus, error) {
	return k.r.Runtime(ctx)
}

type runtimeExpectation struct {
	MixedPort int `yaml:"mixed-port"`
	Tun       struct {
		Enable    bool `yaml:"enable"`
		AutoRoute bool `yaml:"auto-route"`
	} `yaml:"tun"`
}

func (e runtimeExpectation) check(s core.RuntimeStatus) error {
	if e.MixedPort > 0 && (s.MixedPort != e.MixedPort || !s.ProxyReady) {
		return errors.New(localize.Text("本地代理未就绪，请检查端口占用和入站认证设置"))
	}
	if e.Tun.Enable && (!s.TunEnabled || !s.TunReady || !s.DNSReady || (e.Tun.AutoRoute && !s.AutoRoute)) {
		return errors.New(localize.Text("TUN 接管未就绪，请检查网卡、路由和内置 DNS"))
	}
	if !e.Tun.Enable && s.TunEnabled {
		return errors.New(localize.Text("TUN 尚未关闭，请重试"))
	}
	return nil
}

func (k *Kernel) verifyRuntime(raw []byte) error {
	var expected runtimeExpectation
	if err := yaml.Unmarshal(raw, &expected); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	status, err := k.r.Runtime(ctx)
	if err != nil {
		return fmt.Errorf(localize.Text("无法验证内核实际状态；已安装服务时请更新服务: %w"), err)
	}
	return expected.check(status)
}
