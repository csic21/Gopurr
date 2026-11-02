package sysproxy

import (
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"gopurr/internal/process"
)

func NewController() Controller {
	dir, _ := os.UserConfigDir()
	return NewControllerAt(filepath.Join(dir, "Gopurr", "sysproxy-recovery.json"))
}

func NewControllerAt(path string) Controller {
	c := &sessionController{path: path, owner: os.Getpid(), started: processStart(os.Getpid()), current: Current, write: Restore,
		alive: func(pid int, start uint64) bool { return processStart(pid) == start }}
	c.lock = func() (func(), error) { return lockWindowsJournal(path) }
	c.watch = func() error {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		cmd := exec.Command(exe, "--sysproxy-watch-windows", "--journal", path, "--owner", strconv.Itoa(c.owner), "--started", strconv.FormatUint(c.started, 10))
		process.Configure(cmd)
		if err := cmd.Start(); err != nil {
			return err
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}
	return c
}

func processStart(pid int) uint64 {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(h)
	state, err := windows.WaitForSingleObject(h, 0)
	if err != nil || state != uint32(windows.WAIT_TIMEOUT) {
		return 0
	}
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil {
		return 0
	}
	return uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
}

func lockWindowsJournal(path string) (func(), error) {
	// Windows mutexes belong to the acquiring OS thread, not a goroutine.
	runtime.LockOSThread()
	name, _ := windows.UTF16PtrFromString(fmt.Sprintf(`Local\Gopurr-Proxy-%x`, sha256.Sum256([]byte(strings.ToLower(filepath.Clean(path))))))
	h, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		runtime.UnlockOSThread()
		return nil, err
	}
	state, err := windows.WaitForSingleObject(h, 10000)
	if err != nil || (state != windows.WAIT_OBJECT_0 && state != windows.WAIT_ABANDONED) {
		windows.CloseHandle(h)
		runtime.UnlockOSThread()
		return nil, errors.New("等待系统代理恢复操作超时")
	}
	return func() { _ = windows.ReleaseMutex(h); _ = windows.CloseHandle(h); runtime.UnlockOSThread() }, nil
}

func HandleCommand(args []string) (bool, error) {
	if len(args) == 0 || args[0] != "--sysproxy-watch-windows" {
		return false, nil
	}
	fs := flag.NewFlagSet("sysproxy-watch-windows", flag.ContinueOnError)
	path := fs.String("journal", "", "recovery journal")
	owner := fs.Int("owner", 0, "application pid")
	started := fs.Uint64("started", 0, "application creation time")
	if err := fs.Parse(args[1:]); err != nil {
		return true, err
	}
	if !filepath.IsAbs(*path) || *owner <= 1 || *started == 0 || fs.NArg() != 0 {
		return true, errors.New("系统代理恢复助手参数无效")
	}
	c := NewControllerAt(*path).(*sessionController)
	for c.alive(*owner, *started) {
		time.Sleep(time.Second)
	}
	for attempt := 0; attempt < 30; attempt++ {
		unlock, err := c.lock()
		if err != nil {
			return true, err
		}
		j, err := c.load()
		if err == nil && j != nil && j.Owner == *owner && j.Started == *started {
			err = c.restoreLocked(j)
		}
		unlock()
		if err == nil {
			return true, nil
		}
		if attempt == 29 {
			return true, err
		}
		time.Sleep(2 * time.Second)
	}
	return true, nil
}
