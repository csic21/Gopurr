//go:build darwin

package sysproxy

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"gopurr/internal/localize"
)

const supported = true

// bypasDomains keeps loopback and local names out of the proxy, which is what
// every other Clash client does and what keeps the app's own traffic direct.
var bypassDomains = []string{"127.0.0.1", "localhost", "169.254/16", "*.local"}

const runTimeout = 5 * time.Second

func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

// Service resolves the network service in front of the default route, e.g.
// "Wi-Fi" for en0.
func Service() (string, error) {
	return serviceUsing(run)
}

type commandRunner func(string, ...string) (string, error)

func serviceUsing(command commandRunner) (string, error) {
	route, err := command("route", "-n", "get", "default")
	if err != nil {
		return "", err
	}
	device := ""
	for _, line := range strings.Split(route, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "interface:") {
			device = strings.TrimSpace(strings.TrimPrefix(line, "interface:"))
		}
	}
	if device == "" {
		return "", errors.New(localize.Text("找不到默认网卡"))
	}
	return serviceForDeviceUsing(command, device)
}

func serviceForDevice(device string) (string, error) {
	return serviceForDeviceUsing(run, device)
}

func serviceForDeviceUsing(command commandRunner, device string) (string, error) {
	order, err := command("networksetup", "-listnetworkserviceorder")
	if err != nil {
		return "", err
	}

	var current string
	for _, line := range strings.Split(order, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "(") && !strings.Contains(line, "Hardware Port") {
			if i := strings.Index(line, ") "); i >= 0 {
				current = strings.TrimSpace(line[i+1:])
			}
			continue
		}
		if current != "" && strings.Contains(line, "Device: "+device+")") {
			return current, nil
		}
	}
	return "", fmt.Errorf(localize.Text("网卡 %s 没有对应的网络服务"), device)
}

// Current reads the HTTP proxy state of the active service.
func Current() (State, error) {
	service, err := Service()
	if err != nil {
		return State{Supported: true}, err
	}
	out, err := run("networksetup", "-getwebproxy", service)
	if err != nil {
		return State{Supported: true, Service: service}, err
	}

	st := State{Supported: true, Service: service}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		switch key {
		case "Enabled":
			st.Enabled = strings.EqualFold(value, "Yes")
		case "Server":
			st.Server = value
		case "Port":
			st.Port, _ = strconv.Atoi(value)
		}
	}
	return st, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
