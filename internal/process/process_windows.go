package process

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"golang.org/x/sys/windows"
)

func Configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

func Alive(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return err == windows.ERROR_ACCESS_DENIED
	}
	defer windows.CloseHandle(h)
	state, err := windows.WaitForSingleObject(h, 0)
	return err == nil && state == uint32(windows.WAIT_TIMEOUT)
}

func UID() int {
	if windows.GetCurrentProcessToken().IsElevated() {
		return 0
	}
	return -1
}

func eventName(pid int) *uint16 {
	name, _ := windows.UTF16PtrFromString(fmt.Sprintf(`Local\Gopurr-core-%d`, pid))
	return name
}

// Windows has no SIGTERM. A named event lets the owning app request the same
// graceful shutdown as on macOS, so mihomo can release its listeners.
func Stop(p *os.Process) error {
	h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, eventName(p.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.SetEvent(h)
}

func NotifyStop(ch chan os.Signal) (func(), error) {
	h, err := windows.CreateEvent(nil, 1, 0, eventName(os.Getpid()))
	if err != nil {
		return nil, err
	}
	signal.Notify(ch, os.Interrupt)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if result, err := windows.WaitForSingleObject(h, windows.INFINITE); err == nil && result == windows.WAIT_OBJECT_0 {
			select {
			case ch <- os.Interrupt:
			default:
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		_ = windows.SetEvent(h)
		<-done
		_ = windows.CloseHandle(h)
	}, nil
}
