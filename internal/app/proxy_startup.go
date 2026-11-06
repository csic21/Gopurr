package app

const (
	proxyStartupLast = "last"
	proxyStartupOn   = "on"
	proxyStartupOff  = "off"
)

func normalizedProxyStartup(policy string) string {
	if policy == proxyStartupOn || policy == proxyStartupOff {
		return policy
	}
	return proxyStartupLast
}

func proxyStartupIndex(policy string) int {
	switch policy {
	case proxyStartupOn:
		return 1
	case proxyStartupOff:
		return 2
	}
	return 0
}

func proxyStartupValue(index int) string {
	switch index {
	case 1:
		return proxyStartupOn
	case 2:
		return proxyStartupOff
	}
	return proxyStartupLast
}

func (p Prefs) systemProxyOnStartup() bool {
	switch p.SystemProxyStartup {
	case proxyStartupOn:
		return true
	case proxyStartupOff:
		return false
	default:
		return p.RestoreSystemProxy
	}
}
