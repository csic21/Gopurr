package sysproxy

import (
	"errors"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"gopurr/internal/localize"
)

const supported = true

var wininet = windows.NewLazySystemDLL("wininet.dll")
var internetSetOption = wininet.NewProc("InternetSetOptionW")
var internetQueryOption = wininet.NewProc("InternetQueryOptionW")
var globalFree = windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalFree")

// INTERNET_PER_CONN_OPTIONW contains a union whose largest member is FILETIME.
type internetOption struct {
	Option uint32
	Value  uint64
}

type internetOptionList struct {
	Size       uint32
	Connection *uint16 // nil selects the current user's LAN settings
	Count      uint32
	Error      uint32
	Options    *internetOption
}

func SetTimeout(time.Duration) {}
func Service() (string, error) { return localize.Text("当前用户"), nil }

func Current() (State, error) {
	state, err := queryWindowsOptions(10) // INTERNET_PER_CONN_FLAGS_UI
	if err != nil {
		state, err = queryWindowsOptions(1) // legacy INTERNET_PER_CONN_FLAGS
	}
	return state, err
}

func queryWindowsOptions(flagsOption uint32) (State, error) {
	options := []internetOption{{Option: flagsOption}, {Option: 2}, {Option: 3}, {Option: 4}}
	list := internetOptionList{Count: uint32(len(options)), Options: &options[0]}
	list.Size = uint32(unsafe.Sizeof(list))
	size := list.Size
	ok, _, callErr := internetQueryOption.Call(0, 75, uintptr(unsafe.Pointer(&list)), uintptr(unsafe.Pointer(&size)))
	defer func() {
		for _, option := range options[1:] {
			if option.Value != 0 {
				_, _, _ = globalFree.Call(uintptr(option.Value))
			}
		}
	}()
	if ok == 0 {
		return State{}, fmt.Errorf(localize.Text("读取 Windows 系统代理失败: %v"), callErr)
	}
	state := State{Supported: true, Service: localize.Text("当前用户"), WindowsOptions: true,
		Flags: uint32(options[0].Value), RawServer: optionString(options[1]), Bypass: optionString(options[2]), PAC: optionString(options[3])}
	state.Enabled = state.Flags&proxyTypeProxy != 0
	state.Server, state.Port = parseWindowsProxy(state.RawServer)
	return state, nil
}

//go:nocheckptr
func optionString(option internetOption) string {
	if option.Value == 0 {
		return ""
	}
	return windows.UTF16PtrToString((*uint16)(unsafe.Pointer(uintptr(option.Value))))
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
	return writeWindowsOptions(windowsManualProxy(server, port))
}

func Restore(state State) error {
	if !state.WindowsOptions {
		current, err := Current()
		if err != nil {
			return err
		}
		current.RawServer = state.RawServer
		if current.RawServer == "" && state.Server != "" && state.Port > 0 {
			current.RawServer = net.JoinHostPort(state.Server, strconv.Itoa(state.Port))
		}
		current.Flags &^= proxyTypeProxy
		if state.Enabled {
			current.Flags |= proxyTypeProxy
		}
		state = current
	}
	return writeWindowsOptions(state)
}

func Disable() error {
	state, err := Current()
	if err != nil {
		return err
	}
	state.Flags &^= proxyTypeProxy
	state.Flags |= proxyTypeDirect
	return writeWindowsOptions(state)
}

func writeWindowsOptions(state State) error {
	before, err := Current()
	if err != nil {
		return err
	}
	if err := setWindowsOptions(state); err != nil {
		return errors.Join(err, setWindowsOptions(before))
	}
	return nil
}

func setWindowsOptions(state State) error {
	stringsUTF16 := make([]*uint16, 3)
	for index, value := range []string{state.RawServer, state.Bypass, state.PAC} {
		p, err := windows.UTF16PtrFromString(value)
		if err != nil {
			return err
		}
		stringsUTF16[index] = p
	}
	options := []internetOption{{Option: 1, Value: uint64(state.Flags)},
		{Option: 2, Value: uint64(uintptr(unsafe.Pointer(stringsUTF16[0])))},
		{Option: 3, Value: uint64(uintptr(unsafe.Pointer(stringsUTF16[1])))},
		{Option: 4, Value: uint64(uintptr(unsafe.Pointer(stringsUTF16[2])))}}
	list := internetOptionList{Count: uint32(len(options)), Options: &options[0]}
	list.Size = uint32(unsafe.Sizeof(list))
	ok, _, callErr := internetSetOption.Call(0, 75, uintptr(unsafe.Pointer(&list)), uintptr(list.Size))
	runtime.KeepAlive(stringsUTF16)
	if ok == 0 {
		return fmt.Errorf(localize.Text("设置 Windows 系统代理失败: %v"), callErr)
	}
	return notifyWindowsProxy()
}

func notifyWindowsProxy() error {
	for _, option := range []uintptr{39, 37} {
		if ok, _, err := internetSetOption.Call(0, option, 0, 0); ok == 0 {
			return fmt.Errorf(localize.Text("通知 Windows 系统代理失败: %v"), err)
		}
	}
	return nil
}
