package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"gopurr/internal/process"
	"gopurr/internal/securefs"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

func Available() bool { return true }

// Query access works for ordinary users; managing the service requires UAC.
func Installed() bool {
	h, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false
	}
	defer windows.CloseServiceHandle(h)
	name, _ := windows.UTF16PtrFromString(Label)
	s, err := windows.OpenService(h, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return false
	}
	windows.CloseServiceHandle(s)
	return true
}

func Install(binary, dataDir string) error {
	if !filepath.IsAbs(binary) || !filepath.IsAbs(dataDir) {
		return errors.New("安装服务需要绝对路径")
	}
	if _, err := os.Stat(binary); err != nil {
		return err
	}
	return elevate(binary, "install", dataDir)
}

func Uninstall(dataDir string) error {
	if !filepath.IsAbs(dataDir) {
		return errors.New("数据目录必须是绝对路径")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return elevate(exe, "uninstall", dataDir)
}

type adminResult struct {
	Error string `json:"error"`
}

func elevate(binary, action, dataDir string) error {
	result, err := os.CreateTemp(dataDir, ".service-result-*.json")
	if err != nil {
		return err
	}
	path := result.Name()
	result.Close()
	defer os.Remove(path)
	args := []string{"--service-admin", action, dataDir, path}
	var quoted []string
	for _, arg := range args {
		quoted = append(quoted, syscall.EscapeArg(arg))
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(binary)
	params, err := windows.UTF16PtrFromString(strings.Join(quoted, " "))
	if err != nil {
		return err
	}
	if err := windows.ShellExecute(0, verb, file, params, nil, windows.SW_HIDE); err != nil {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return errors.New("已取消管理员授权")
		}
		return err
	}
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(path)
		var result adminResult
		if json.Unmarshal(data, &result) == nil {
			if result.Error != "" {
				return errors.New(result.Error)
			}
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("等待管理员操作超时，请重试")
}

// HandleCommand runs before any UI initialization, both for the UAC child and
// for the Service Control Manager's instance of the protected kernel copy.
func HandleCommand(args []string, run func([]string) error) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "--service-admin":
		if len(args) != 4 {
			return true, errors.New("服务管理参数不完整")
		}
		if !windows.GetCurrentProcessToken().IsElevated() {
			return true, errors.New("服务管理需要管理员权限")
		}
		var err error
		switch args[1] {
		case "install":
			err = installWindowsService(args[2])
		case "uninstall":
			err = uninstallWindowsService()
		default:
			err = errors.New("未知服务操作")
		}
		result := adminResult{}
		if err != nil {
			result.Error = err.Error()
		}
		body, _ := json.Marshal(result)
		if writeErr := securefs.WriteFile(args[3], body, 0o600); writeErr != nil {
			return true, writeErr
		}
		return true, err
	case "--windows-service":
		if len(args) != 2 || !filepath.IsAbs(args[1]) {
			return true, errors.New("服务数据目录无效")
		}
		isService, err := svc.IsWindowsService()
		if err != nil || !isService {
			return true, errors.New("此入口只能由 Windows 服务管理器运行")
		}
		return true, svc.Run(Label, &windowsHandler{dir: args[1], run: run})
	}
	return false, nil
}

func serviceBinary() (string, error) {
	base, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, 0)
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Gopurr", "Service", "Gopurr-core.exe"), nil
}

func stopWindowsService(s *mgr.Service) error {
	state, err := s.Query()
	if err != nil {
		return err
	}
	if state.State == svc.Stopped {
		return nil
	}
	if state.State != svc.StopPending {
		if _, err := s.Control(svc.Stop); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		state, err = s.Query()
		if err != nil {
			return err
		}
		if state.State == svc.Stopped {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("停止 TUN 服务超时")
}

func installWindowsService(dataDir string) error {
	if !filepath.IsAbs(dataDir) {
		return errors.New("服务数据目录必须是绝对路径")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "runtime.yaml")); err != nil {
		return fmt.Errorf("请先运行应用生成配置: %w", err)
	}
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	old, err := manager.OpenService(Label)
	if err == nil {
		defer old.Close()
		if err := stopWindowsService(old); err != nil {
			return err
		}
	} else if !errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return err
	}
	target, err := serviceBinary()
	if err != nil {
		return err
	}
	if err := securefs.AdministratorDir(filepath.Dir(filepath.Dir(target))); err != nil {
		return err
	}
	if err := securefs.AdministratorDir(filepath.Dir(target)); err != nil {
		return err
	}
	source, err := os.Executable()
	if err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	staged, err := os.CreateTemp(filepath.Dir(target), ".core-*.exe")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	_, copyErr := io.Copy(staged, in)
	closeErr := staged.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	from, _ := windows.UTF16PtrFromString(staged.Name())
	to, _ := windows.UTF16PtrFromString(target)
	deadline := time.Now().Add(5 * time.Second)
	for {
		err = windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	config := mgr.Config{ServiceType: windows.SERVICE_WIN32_OWN_PROCESS, ErrorControl: mgr.ErrorNormal, DisplayName: "Gopurr Core", Description: "Gopurr mihomo kernel for TUN networking", StartType: mgr.StartAutomatic}
	var service *mgr.Service
	if old != nil {
		config.BinaryPathName = syscall.EscapeArg(target) + " --windows-service " + syscall.EscapeArg(dataDir)
		if err := old.UpdateConfig(config); err != nil {
			return err
		}
		service = old
	} else {
		service, err = manager.CreateService(Label, target, config, "--windows-service", dataDir)
		if err != nil {
			return err
		}
		defer service.Close()
	}
	return service.Start()
}

func uninstallWindowsService() error {
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	s, err := manager.OpenService(Label)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if err != nil {
		return err
	}
	defer s.Close()
	if err := stopWindowsService(s); err != nil {
		return err
	}
	if err := s.Delete(); err != nil {
		return err
	}
	if target, err := serviceBinary(); err == nil {
		_ = os.Remove(target)
		_ = os.Remove(filepath.Dir(target))
	}
	return nil
}

type windowsHandler struct {
	dir string
	run func([]string) error
}

func (h *windowsHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}
	// Privileged logs and provider caches must not follow paths planted in a
	// user-writable directory. Keep them beside the protected service copy.
	binary, err := serviceBinary()
	if err != nil {
		return true, 1
	}
	serviceDir := filepath.Dir(binary)
	file, err := os.OpenFile(filepath.Join(serviceDir, "core.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return true, 1
	}
	defer file.Close()
	log.SetOutput(file)
	logrus.SetOutput(file)
	result := make(chan error, 1)
	go func() {
		result <- h.run([]string{"--config", filepath.Join(h.dir, "runtime.yaml"), "--socket", filepath.Join(h.dir, "core.sock"),
			"--secret-file", filepath.Join(h.dir, "core.secret"), "--home", filepath.Join(serviceDir, "mihomo"), "--status", filepath.Join(h.dir, "core.json")})
	}()
	status := svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	changes <- status
	for {
		select {
		case err := <-result:
			if err != nil {
				log.Print(err)
				return true, 1
			}
			return false, 0
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				changes <- status
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				p, _ := os.FindProcess(os.Getpid())
				// Wait for the daemon's event to exist if stop raced startup.
				deadline := time.Now().Add(5 * time.Second)
				for process.Stop(p) != nil && time.Now().Before(deadline) {
					time.Sleep(50 * time.Millisecond)
				}
				select {
				case <-result:
					return false, 0
				case <-time.After(15 * time.Second):
					return true, 1
				}
			}
		}
	}
}
