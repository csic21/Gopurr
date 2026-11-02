package sysproxy

import (
	"net"
	"strconv"
)

const (
	proxyTypeDirect = 1
	proxyTypeProxy  = 2
)

func windowsManualProxy(host string, port int) State {
	return State{Supported: true, WindowsOptions: true, Enabled: true,
		Server: host, Port: port, RawServer: net.JoinHostPort(host, strconv.Itoa(port)),
		Flags: proxyTypeDirect | proxyTypeProxy, Bypass: "<local>;localhost;127.*;[::1]"}
}

func sameWindowsOptions(a, b State) bool {
	return a.WindowsOptions && b.WindowsOptions && a.Flags == b.Flags &&
		a.RawServer == b.RawServer && a.Bypass == b.Bypass && a.PAC == b.PAC
}
