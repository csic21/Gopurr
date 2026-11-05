//go:build darwin

package sysproxy

import (
	"fmt"
	"runtime"
	"strings"
	"sync"

	"github.com/ebitengine/purego"
	"gopurr/internal/localize"
)

// Read the selected network service's saved settings, including disabled
// protocols. Effective global proxies alone lose the values needed by recovery.
// No preferences are written and no native callbacks outlive this call.
type proxyObserver struct {
	poolPush          func() uintptr
	poolPop           func(uintptr)
	release           func(uintptr)
	typeID            func(uintptr) uintptr
	stringType        func() uintptr
	numberType        func() uintptr
	booleanType       func() uintptr
	dictionaryType    func() uintptr
	arrayType         func() uintptr
	stringCreate      func(uintptr, string, uint32) uintptr
	stringLength      func(uintptr) int64
	stringMaxBytes    func(int64, uint32) int64
	stringGet         func(uintptr, *byte, int64, uint32) bool
	dictionaryGet     func(uintptr, uintptr) uintptr
	numberGet         func(uintptr, int32, *int64) bool
	booleanGet        func(uintptr) bool
	arrayCount        func(uintptr) int64
	arrayGet          func(uintptr, int64) uintptr
	storeCreate       func(uintptr, uintptr, uintptr, uintptr) uintptr
	storeCopy         func(uintptr, uintptr) uintptr
	preferencesCreate func(uintptr, uintptr, uintptr) uintptr
	serviceCopy       func(uintptr, uintptr) uintptr
	serviceName       func(uintptr) uintptr
	protocolCopy      func(uintptr, uintptr) uintptr
	protocolConfig    func(uintptr) uintptr
}

const cfUTF8 = 0x08000100

var loadProxyObserver = sync.OnceValues(func() (api *proxyObserver, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			api = nil
			err = fmt.Errorf("SystemConfiguration: %v", recovered)
		}
	}()
	cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	sc, err := purego.Dlopen("/System/Library/Frameworks/SystemConfiguration.framework/SystemConfiguration", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	objc, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	a := &proxyObserver{}
	purego.RegisterLibFunc(&a.poolPush, objc, "objc_autoreleasePoolPush")
	purego.RegisterLibFunc(&a.poolPop, objc, "objc_autoreleasePoolPop")
	for _, binding := range []struct {
		fn   any
		name string
	}{
		{&a.release, "CFRelease"}, {&a.typeID, "CFGetTypeID"},
		{&a.stringType, "CFStringGetTypeID"}, {&a.numberType, "CFNumberGetTypeID"},
		{&a.booleanType, "CFBooleanGetTypeID"}, {&a.dictionaryType, "CFDictionaryGetTypeID"}, {&a.arrayType, "CFArrayGetTypeID"},
		{&a.stringCreate, "CFStringCreateWithCString"}, {&a.stringLength, "CFStringGetLength"},
		{&a.stringMaxBytes, "CFStringGetMaximumSizeForEncoding"}, {&a.stringGet, "CFStringGetCString"},
		{&a.dictionaryGet, "CFDictionaryGetValue"}, {&a.numberGet, "CFNumberGetValue"}, {&a.booleanGet, "CFBooleanGetValue"},
		{&a.arrayCount, "CFArrayGetCount"}, {&a.arrayGet, "CFArrayGetValueAtIndex"},
	} {
		purego.RegisterLibFunc(binding.fn, cf, binding.name)
	}
	for _, binding := range []struct {
		fn   any
		name string
	}{
		{&a.storeCreate, "SCDynamicStoreCreate"}, {&a.storeCopy, "SCDynamicStoreCopyValue"},
		{&a.preferencesCreate, "SCPreferencesCreate"}, {&a.serviceCopy, "SCNetworkServiceCopy"},
		{&a.serviceName, "SCNetworkServiceGetName"}, {&a.protocolCopy, "SCNetworkServiceCopyProtocol"},
		{&a.protocolConfig, "SCNetworkProtocolGetConfiguration"},
	} {
		purego.RegisterLibFunc(binding.fn, sc, binding.name)
	}
	return a, nil
})

func (a *proxyObserver) get(dict uintptr, key string) uintptr {
	if dict == 0 || a.typeID(dict) != a.dictionaryType() {
		return 0
	}
	k := a.stringCreate(0, key, cfUTF8)
	defer a.release(k)
	return a.dictionaryGet(dict, k)
}
func (a *proxyObserver) text(value uintptr) string {
	if value == 0 || a.typeID(value) != a.stringType() {
		return ""
	}
	size := a.stringMaxBytes(a.stringLength(value), cfUTF8) + 1
	if size < 1 || size > 1<<20 {
		return ""
	}
	buf := make([]byte, size)
	if !a.stringGet(value, &buf[0], size, cfUTF8) {
		return ""
	}
	return strings.TrimRight(string(buf), "\x00")
}
func (a *proxyObserver) number(value uintptr) int64 {
	if value == 0 {
		return 0
	}
	switch a.typeID(value) {
	case a.booleanType():
		if a.booleanGet(value) {
			return 1
		}
	case a.numberType():
		var n int64
		if a.numberGet(value, 4, &n) {
			return n
		}
	}
	return 0
}

func (a *proxyObserver) current() (State, error) {
	// SystemConfiguration creates autoreleased Objective-C objects internally,
	// even though its public API uses Core Foundation ownership. The health
	// watchdog runs off the AppKit thread, with no event-loop pool to drain them.
	// A pool is thread-local, so keep this goroutine on its thread until it drains.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := a.poolPush()
	defer a.poolPop(pool)

	st := State{Supported: true}
	name := a.stringCreate(0, "Gopurr proxy observation", cfUTF8)
	defer a.release(name)
	store := a.storeCreate(0, name, 0, 0)
	if store == 0 {
		return st, fmt.Errorf("SystemConfiguration: cannot open dynamic store")
	}
	defer a.release(store)
	var serviceID uintptr
	for _, family := range []string{"IPv4", "IPv6"} {
		key := a.stringCreate(0, "State:/Network/Global/"+family, cfUTF8)
		value := a.storeCopy(store, key)
		a.release(key)
		if value != 0 {
			serviceID = a.get(value, "PrimaryService")
			if serviceID != 0 {
				serviceID = a.stringCreate(0, a.text(serviceID), cfUTF8)
			}
			a.release(value)
		}
		if serviceID != 0 {
			break
		}
	}
	if serviceID == 0 {
		return st, fmt.Errorf("%s", localize.Text("找不到默认网卡"))
	}
	defer a.release(serviceID)
	prefs := a.preferencesCreate(0, name, 0)
	if prefs == 0 {
		return st, fmt.Errorf("SystemConfiguration: cannot read network preferences")
	}
	defer a.release(prefs)
	service := a.serviceCopy(prefs, serviceID)
	if service == 0 {
		return st, fmt.Errorf("SystemConfiguration: active network service unavailable")
	}
	defer a.release(service)
	st.Service = a.text(a.serviceName(service))
	if st.Service == "" {
		return st, fmt.Errorf("SystemConfiguration: network service has no name")
	}
	kind := a.stringCreate(0, "Proxies", cfUTF8)
	defer a.release(kind)
	protocol := a.protocolCopy(service, kind)
	if protocol == 0 {
		return st, fmt.Errorf("SystemConfiguration: proxy settings unavailable")
	}
	defer a.release(protocol)
	config := a.protocolConfig(protocol)
	return a.decodeProxies(st, config), nil
}

func (a *proxyObserver) decodeProxies(st State, config uintptr) State {
	st.ProtocolsKnown = true
	for i, prefix := range []string{"HTTP", "HTTPS", "SOCKS"} {
		st.Protocols[i] = Endpoint{Enabled: a.number(a.get(config, prefix+"Enable")) != 0, Server: a.text(a.get(config, prefix+"Proxy")), Port: int(a.number(a.get(config, prefix+"Port")))}
	}
	st.Enabled, st.Server, st.Port = st.Protocols[0].Enabled, st.Protocols[0].Server, st.Protocols[0].Port
	st.PACEnabled = a.number(a.get(config, "ProxyAutoConfigEnable")) != 0
	st.PAC = a.text(a.get(config, "ProxyAutoConfigURLString"))
	st.AutoDiscovery = a.number(a.get(config, "ProxyAutoDiscoveryEnable")) != 0
	exceptions := a.get(config, "ExceptionsList")
	if exceptions != 0 && a.typeID(exceptions) == a.arrayType() {
		var domains []string
		for i, n := int64(0), a.arrayCount(exceptions); i < n; i++ {
			domains = append(domains, a.text(a.arrayGet(exceptions, i)))
		}
		st.Bypass = strings.Join(domains, "\n")
	}
	return st
}
