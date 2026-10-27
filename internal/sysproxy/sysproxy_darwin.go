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
)

const supported = true

// bypasDomains keeps loopback and local names out of the proxy, which is what
// every other Clash client does and what keeps the app's own traffic direct.
var bypassDomains = []string{"127.0.0.1", "localhost", "169.254/16", "*.local"}

// runTimeout bounds every networksetup/route call. Quitting shortens it so a
// slow authorization dialog cannot hold the app open.
var runTimeout = 15 * time.Second

// SetTimeout shortens or restores the bound for these calls.
func SetTimeout(d time.Duration) {
	if d > 0 {
		runTimeout = d
	}
}

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
	route, err := run("route", "-n", "get", "default")
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
		return "", errors.New("找不到默认网卡")
	}
	return serviceForDevice(device)
}

func serviceForDevice(device string) (string, error) {
	order, err := run("networksetup", "-listnetworkserviceorder")
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
	return "", fmt.Errorf("网卡 %s 没有对应的网络服务", device)
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

// Enable points HTTP, HTTPS and SOCKS at the kernel's mixed port. macOS wants
// administrator rights for this, so the commands go through one Authorization
// prompt instead of three.
func Enable(port int) error { return Apply("127.0.0.1", port) }

func Restore(st State) error { return Apply(st.Server, st.Port) }

// Apply points the proxies at any server, which is how a previous client's
// settings get put back.
func Apply(server string, port int) error {
	service, err := Service()
	if err != nil {
		return err
	}
	return runPrivileged(enableScript(service, server, port))
}

// Disable turns the three proxy switches back off.
func Disable() error {
	service, err := Service()
	if err != nil {
		return err
	}
	return runPrivileged(disableScript(service))
}

func runPrivileged(script string) error {
	// The script is executed by a shell as root; the only interpolation is the
	// service name and an integer port, both quoted.
	apple := fmt.Sprintf("do shell script %s with administrator privileges", strconv.Quote(script))
	out, err := run("osascript", "-e", apple)
	if err != nil {
		if strings.Contains(strings.ToLower(out+err.Error()), "cancel") {
			return errors.New("已取消授权")
		}
		return err
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func enableScript(service, server string, port int) string {
	svc := shellQuote(service)
	host := shellQuote(server)
	bypass := make([]string, len(bypassDomains))
	for i, d := range bypassDomains {
		bypass[i] = shellQuote(d)
	}
	p := strconv.Itoa(port)
	return strings.Join([]string{
		fmt.Sprintf("networksetup -setwebproxy %s %s %s", svc, host, p),
		fmt.Sprintf("networksetup -setsecurewebproxy %s %s %s", svc, host, p),
		fmt.Sprintf("networksetup -setsocksfirewallproxy %s %s %s", svc, host, p),
		fmt.Sprintf("networksetup -setproxybypassdomains %s %s", svc, strings.Join(bypass, " ")),
	}, "; ")
}

func disableScript(service string) string {
	svc := shellQuote(service)
	return strings.Join([]string{
		fmt.Sprintf("networksetup -setwebproxystate %s off", svc),
		fmt.Sprintf("networksetup -setsecurewebproxystate %s off", svc),
		fmt.Sprintf("networksetup -setsocksfirewallproxystate %s off", svc),
	}, "; ")
}
