//go:build darwin

package sysproxy

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"gopurr/internal/localize"
)

var proxyProtocols = [...]string{"web", "secureweb", "socksfirewall"}

type proxySetting struct {
	enabled bool
	server  string
	port    int
}

type automaticProxy struct {
	url            string
	pac, discovery bool
}

func readAutomaticProxy(command commandRunner, service string) (automaticProxy, error) {
	var setting automaticProxy
	for _, option := range []string{"autoproxyurl", "proxyautodiscovery"} {
		output, err := command("networksetup", "-get"+option, service)
		if err != nil {
			return setting, err
		}
		found := false
		for _, line := range strings.Split(output, "\n") {
			key, value, _ := strings.Cut(line, ":")
			value = strings.TrimSpace(value)
			switch strings.TrimSpace(key) {
			case "URL":
				setting.url = value
			case "Enabled", "Auto Proxy Discovery":
				if value != "Yes" && value != "No" && value != "On" && value != "Off" {
					continue
				}
				found = true
				on := value == "Yes" || value == "On"
				if option == "autoproxyurl" {
					setting.pac = on
				} else {
					setting.discovery = on
				}
			}
		}
		if !found {
			return setting, errors.New(localize.Text("无法读取自动代理状态"))
		}
	}
	return setting, nil
}

type proxyLease struct {
	original    [3]proxySetting
	bypass      []string
	automatic   automaticProxy
	autoDirty   bool
	ports       map[int]bool
	dirty       [3]bool
	restoring   [3]bool
	bypassDirty bool
}

type proxyLeases struct {
	command  commandRunner
	services map[string]*proxyLease
	journal  string
	owner    int
}

func validateProxy(server string, port int) error {
	if strings.TrimSpace(server) == "" || strings.ContainsAny(server, "\x00\r\n") || port < 1 || port > 65535 {
		return errors.New(localize.Text("系统代理地址或端口无效"))
	}
	return nil
}

func readProxy(command commandRunner, service, protocol string) (proxySetting, error) {
	output, err := command("networksetup", "-get"+protocol+"proxy", service)
	if err != nil {
		return proxySetting{}, err
	}
	var setting proxySetting
	found := false
	for _, line := range strings.Split(output, "\n") {
		key, value, _ := strings.Cut(line, ":")
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "Enabled":
			if value != "Yes" && value != "No" {
				return setting, errors.New(localize.Text("系统代理状态无效"))
			}
			found, setting.enabled = true, value == "Yes"
		case "Server":
			setting.server = value
		case "Port":
			setting.port, _ = strconv.Atoi(value)
		}
	}
	if !found {
		return setting, errors.New(localize.Text("无法读取系统代理状态"))
	}
	return setting, nil
}

func readBypass(command commandRunner, service string) ([]string, error) {
	output, err := command("networksetup", "-getproxybypassdomains", service)
	if err != nil {
		return nil, err
	}
	output = strings.TrimSpace(output)
	if output == "" || strings.HasPrefix(output, "There aren't any bypass domains") {
		return nil, nil
	}
	return strings.Split(output, "\n"), nil
}

func (l *proxyLeases) enable(port int, previous *State) error {
	if err := validateProxy("127.0.0.1", port); err != nil {
		return err
	}
	if previous != nil && previous.Enabled {
		if err := validateProxy(previous.Server, previous.Port); err != nil {
			return err
		}
	}
	service, err := serviceUsing(l.command)
	if err != nil {
		return err
	}
	lease := l.services[service]
	if lease == nil {
		lease = &proxyLease{ports: make(map[int]bool)}
		for i, protocol := range proxyProtocols {
			setting, err := readProxy(l.command, service, protocol)
			if err != nil {
				return err
			}
			// Recover a proxy left behind by the older, non-session implementation.
			// Never preserve a listener that belongs to this app as its own backup.
			if setting.server == "127.0.0.1" && setting.port == port {
				setting.enabled = false
				if previous != nil && previous.Enabled && (previous.Service == "" || previous.Service == service) {
					setting = proxySetting{true, previous.Server, previous.Port}
				}
			}
			lease.original[i] = setting
		}
		lease.bypass, err = readBypass(l.command, service)
		if err != nil {
			return err
		}
		lease.automatic, err = readAutomaticProxy(l.command, service)
		if err != nil {
			return err
		}
		l.services[service] = lease
	}
	lease.ports[port] = true
	l.owner = os.Getpid()
	// Commit the complete snapshot before the first OS write. A process crash
	// must not lose protocol-specific settings or the previous bypass list.
	lease.dirty = [3]bool{true, true, true}
	lease.bypassDirty = true
	lease.autoDirty = true
	if err := l.persist(); err != nil {
		return fmt.Errorf(localize.Text("保存系统代理恢复记录失败: %w"), err)
	}
	for i, protocol := range proxyProtocols {
		lease.dirty[i] = true
		if _, err := l.command("networksetup", "-set"+protocol+"proxy", service, "127.0.0.1", strconv.Itoa(port)); err != nil {
			return errors.Join(err, l.restore())
		}
	}
	for _, option := range []string{"autoproxystate", "proxyautodiscovery"} {
		if _, err := l.command("networksetup", "-set"+option, service, "off"); err != nil {
			return errors.Join(err, l.restore())
		}
	}
	lease.bypassDirty = true
	args := append([]string{"-setproxybypassdomains", service}, bypassDomains...)
	if _, err := l.command("networksetup", args...); err != nil {
		return errors.Join(err, l.restore())
	}
	observed, err := currentUsing(l.command)
	if err != nil || !observed.OwnedBy(port) {
		return errors.Join(errors.New(localize.Text("系统代理设置未生效，请检查系统策略或其他代理软件")), err, l.restore())
	}
	return nil
}

func (l *proxyLeases) restore() error {
	var failures []error
	for service, lease := range l.services {
		if err := l.restoreService(service, lease); err != nil {
			failures = append(failures, fmt.Errorf(localize.Text("恢复 %s 系统代理失败: %w"), service, err))
		} else {
			delete(l.services, service)
		}
	}
	return errors.Join(append(failures, l.persist())...)
}

func (l *proxyLeases) restoreService(service string, lease *proxyLease) error {
	var failures []error
	// Decide ownership before restoring any protocol. Automatic settings must
	// not overwrite a later takeover by the user or another proxy client.
	ours := false
	for _, protocol := range proxyProtocols {
		if p, err := readProxy(l.command, service, protocol); err == nil && p.server == "127.0.0.1" && lease.ports[p.port] {
			ours = true
		}
	}
	if lease.autoDirty {
		current, err := readAutomaticProxy(l.command, service)
		pacWritten, discoveryWritten := false, false
		if err == nil && ours {
			if !current.pac && current.url == lease.automatic.url {
				state := "off"
				if lease.automatic.pac {
					state = "on"
				}
				_, err = l.command("networksetup", "-setautoproxystate", service, state)
				pacWritten = err == nil
			}
			if err == nil && !current.discovery {
				state := "off"
				if lease.automatic.discovery {
					state = "on"
				}
				_, err = l.command("networksetup", "-setproxyautodiscovery", service, state)
				discoveryWritten = err == nil
			}
		}
		if err == nil && (pacWritten || discoveryWritten) {
			var observed automaticProxy
			observed, err = readAutomaticProxy(l.command, service)
			if err == nil && ((pacWritten && observed.pac != lease.automatic.pac) || (discoveryWritten && observed.discovery != lease.automatic.discovery)) {
				err = errors.New(localize.Text("系统代理恢复未生效，请检查系统策略"))
			}
		}
		if err != nil {
			// Keep our manual endpoints intact so a retry can still establish
			// ownership after a failed automatic-proxy restore.
			return err
		} else {
			lease.autoDirty = false
		}
	}
	for i, protocol := range proxyProtocols {
		if !lease.dirty[i] {
			continue
		}
		current, err := readProxy(l.command, service, protocol)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		// Another client or the user may have taken ownership meanwhile.
		old := lease.original[i]
		ours := current.server == "127.0.0.1" && lease.ports[current.port]
		inProgress := lease.restoring[i] && current.server == old.server && current.port == old.port
		if !current.enabled || (!ours && !inProgress) {
			lease.dirty[i] = false
			continue
		}
		lease.restoring[i] = true
		if err := l.persist(); err != nil {
			failures = append(failures, err)
			continue
		}
		if old.server != "" && old.port > 0 {
			_, err = l.command("networksetup", "-set"+protocol+"proxy", service, old.server, strconv.Itoa(old.port))
			if err == nil {
				lease.restoring[i] = true
			}
		}
		if err == nil && !old.enabled {
			_, err = l.command("networksetup", "-set"+protocol+"proxystate", service, "off")
		}
		if err == nil {
			var observed proxySetting
			observed, err = readProxy(l.command, service, protocol)
			if err == nil && (observed.enabled != old.enabled || (old.server != "" && old.port > 0 && (observed.server != old.server || observed.port != old.port))) {
				err = errors.New(localize.Text("系统代理恢复未生效，请检查系统策略"))
			}
		}
		if err != nil {
			failures = append(failures, err)
		} else {
			lease.dirty[i] = false
		}
	}
	if lease.bypassDirty {
		current, err := readBypass(l.command, service)
		if err == nil && slices.Equal(current, bypassDomains) {
			values := lease.bypass
			if len(values) == 0 {
				values = []string{"Empty"}
			}
			_, err = l.command("networksetup", append([]string{"-setproxybypassdomains", service}, values...)...)
			if err == nil {
				var observed []string
				observed, err = readBypass(l.command, service)
				if err == nil && !slices.Equal(observed, lease.bypass) {
					err = errors.New(localize.Text("系统代理恢复未生效，请检查系统策略"))
				}
			}
		}
		if err != nil {
			failures = append(failures, err)
		} else {
			lease.bypassDirty = false
		}
	}
	return errors.Join(failures...)
}
