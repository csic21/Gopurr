package sysproxy

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"gopurr/internal/localize"
)

const supported = true
const internetSettings = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

var internetSetOption = windows.NewLazySystemDLL("wininet.dll").NewProc("InternetSetOptionW")

func SetTimeout(time.Duration) {}
func Service() (string, error) { return localize.Text("当前用户"), nil }

func Current() (State, error) {
	st := State{Supported: true, Service: localize.Text("当前用户")}
	key, err := registry.OpenKey(registry.CURRENT_USER, internetSettings, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	defer key.Close()
	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return st, err
	}
	st.Enabled = enabled != 0
	st.RawServer, _, err = key.GetStringValue("ProxyServer")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return st, err
	}
	st.Server, st.Port = parseWindowsProxy(st.RawServer)
	return st, nil
}

func parseWindowsProxy(raw string) (string, int) {
	server := strings.TrimSpace(raw)
	if strings.Contains(server, "=") {
		server = ""
		for _, part := range strings.Split(raw, ";") {
			protocol, value, ok := strings.Cut(strings.TrimSpace(part), "=")
			if ok && strings.EqualFold(protocol, "http") {
				server = value
				break
			}
		}
	}
	host, portText, err := net.SplitHostPort(server)
	if err != nil {
		return server, 0
	}
	port, _ := strconv.Atoi(portText)
	return host, port
}

func Enable(port int) error { return Apply("127.0.0.1", port) }

func Apply(server string, port int) error {
	if strings.TrimSpace(server) == "" || strings.ContainsAny(server, "\x00;=\r\n") || port < 1 || port > 65535 {
		return errors.New(localize.Text("系统代理地址或端口无效"))
	}
	return writeWindowsProxy(net.JoinHostPort(server, strconv.Itoa(port)), true)
}

func Restore(st State) error {
	if st.RawServer != "" {
		return writeWindowsProxy(st.RawServer, st.Enabled)
	}
	return Apply(st.Server, st.Port)
}

func Disable() error { return writeWindowsProxy("", false) }

// PAC and bypass preferences keep their values. Raw per-protocol proxy values
// are preserved in State and restored exactly after Gopurr gives up ownership.
func writeWindowsProxy(server string, enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, internetSettings, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	oldServer, _, oldServerErr := key.GetStringValue("ProxyServer")
	oldEnable, _, oldEnableErr := key.GetIntegerValue("ProxyEnable")
	rollback := func() {
		if oldServerErr == nil {
			_ = key.SetStringValue("ProxyServer", oldServer)
		} else {
			_ = key.DeleteValue("ProxyServer")
		}
		if oldEnableErr == nil {
			_ = key.SetDWordValue("ProxyEnable", uint32(oldEnable))
		} else {
			_ = key.DeleteValue("ProxyEnable")
		}
		_ = notifyWindowsProxy()
	}
	if server != "" {
		if err := key.SetStringValue("ProxyServer", server); err != nil {
			return err
		}
	}
	var value uint32
	if enabled {
		value = 1
	}
	if err := key.SetDWordValue("ProxyEnable", value); err != nil {
		rollback()
		return err
	}
	if err := notifyWindowsProxy(); err != nil {
		rollback()
		return err
	}
	return nil
}

func notifyWindowsProxy() error {
	// INTERNET_OPTION_SETTINGS_CHANGED and INTERNET_OPTION_REFRESH.
	for _, option := range []uintptr{39, 37} {
		if ok, _, err := internetSetOption.Call(0, option, 0, 0); ok == 0 {
			return fmt.Errorf(localize.Text("通知 Windows 系统代理失败: %v"), err)
		}
	}
	return nil
}
