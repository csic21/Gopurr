package app

import (
	"go.yaml.in/yaml/v3"
	"gopurr/internal/core"
)

func fixtureRuntime(raw string) core.RuntimeStatus {
	var config struct {
		Port int `yaml:"mixed-port"`
		Tun  struct {
			Enable    bool
			AutoRoute bool `yaml:"auto-route"`
			Stack     string
		} `yaml:"tun"`
	}
	_ = yaml.Unmarshal([]byte(raw), &config)
	return core.RuntimeStatus{MixedPort: config.Port, ProxyReady: config.Port > 0,
		TunEnabled: config.Tun.Enable, TunReady: config.Tun.Enable, AutoRoute: config.Tun.AutoRoute,
		TunDevice: "fixture-tun", TunStack: config.Tun.Stack, DNSReady: true}
}
