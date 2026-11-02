package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"gopurr/internal/localize"
	"gopurr/internal/process"
	"gopurr/internal/rotatelog"
	"gopurr/internal/securefs"
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
		return errors.New(localize.Text("安装服务需要绝对路径"))
	}
	if _, err := os.Stat(binary); err != nil {
		return err
	}
	return elevate(binary, "install", dataDir)
}

func Uninstall(dataDir string) error {
	if !filepath.IsAbs(dataDir) {
		return errors.New(localize.Text("数据目录必须是绝对路径"))
	}
	if Installed() && !ForDataDir(dataDir) {
		return errors.New(localize.Text("系统服务不属于当前数据目录，请重新安装服务"))
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
			return errors.New(localize.Text("已取消管理员授权"))
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
	return errors.New(localize.Text("等待管理员操作超时，请重试"))
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
			return true, errors.New(localize.Text("服务管理参数不完整"))
		}
		if !windows.GetCurrentProcessToken().IsElevated() {
			return true, errors.New(localize.Text("服务管理需要管理员权限"))
		}
		var err error
		switch args[1] {
		case "install":
			err = installWindowsService(args[2])
		case "uninstall":
			err = uninstallWindowsService()
		case "restart":
			err = restartWindowsService()
		default:
			err = errors.New(localize.Text("未知服务操作"))
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
			return true, errors.New(localize.Text("服务数据目录无效"))
		}
		isService, err := svc.IsWindowsService()
		if err != nil || !isService {
			return true, errors.New(localize.Text("此入口只能由 Windows 服务管理器运行"))
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
	return waitWindowsServiceStopped(s.Query, func() error { _, err := s.Control(svc.Stop); return err }, 20*time.Second, 100*time.Millisecond)
}

func waitWindowsServiceStopped(query func() (svc.Status, error), stop func() error, timeout, poll time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, err := query()
		if err != nil {
			return err
		}
		if state.State == svc.Stopped {
			return nil
		}
		// A controller can become ready before SCM receives the Running status.
		// StartPending cannot accept STOP, even if the data path is already up.
		if (state.State == svc.Running || state.State == svc.Paused) && state.Accepts&svc.AcceptStop != 0 {
			if err := stop(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_CANNOT_ACCEPT_CTRL) && !errors.Is(err, windows.ERROR_INVALID_SERVICE_CONTROL) && !errors.Is(err, windows.ERROR_SERVICE_NOT_ACTIVE) {
				return err
			}
		}
		time.Sleep(poll)
	}
	return errors.New(localize.Text("停止 TUN 服务超时"))
}

func installWindowsService(dataDir string) error {
	if !filepath.IsAbs(dataDir) {
		return errors.New(localize.Text("服务数据目录必须是绝对路径"))
	}
	if _, err := os.Stat(filepath.Join(dataDir, "runtime.yaml")); err != nil {
		return fmt.Errorf(localize.Text("请先运行应用生成配置: %w"), err)
	}
	manager, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer manager.Disconnect()
	old, err := manager.OpenService(Label)
	if err == nil {
		defer old.Close()
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
	var previousConfig mgr.Config
	wasRunning := false
	if old != nil {
		previousConfig, err = old.Config()
		if err != nil {
			return err
		}
		state, err := old.Query()
		if err != nil {
			return err
		}
		wasRunning = state.State != svc.Stopped
	}
	var staged, backup string
	retainBackup := false
	defer func() {
		if staged != "" {
			_ = os.Remove(staged)
		}
		if backup != "" && !retainBackup {
			_ = os.Remove(backup)
		}
	}()
	var service *mgr.Service
	upgrade := serviceUpgrade{
		prepare: func() error {
			var err error
			staged, err = stageServiceBinary(source, filepath.Dir(target))
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			check := exec.CommandContext(ctx, staged, "--version")
			process.Configure(check)
			output, err := check.Output()
			if err != nil || !strings.HasPrefix(string(output), "Gopurr ") {
				return fmt.Errorf("staged service executable failed validation: %v", err)
			}
			if _, err := os.Stat(target); err == nil {
				backup, err = stageServiceBinary(target, filepath.Dir(target))
				return err
			} else if !os.IsNotExist(err) {
				return err
			}
			return nil
		},
		stop: func() error {
			if old != nil {
				return stopWindowsService(old)
			}
			return nil
		},
		commit: func() error {
			if err := replaceServiceBinary(staged, target); err != nil {
				return err
			}
			config := mgr.Config{ServiceType: windows.SERVICE_WIN32_OWN_PROCESS, ErrorControl: mgr.ErrorNormal, DisplayName: "Gopurr Core", Description: "Gopurr mihomo kernel for TUN networking", StartType: mgr.StartAutomatic}
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
			}
			// Only the installing data directory's owner can start/stop this service;
			// changing its executable or configuration still requires administrator rights.
			sd, err := windows.GetNamedSecurityInfo(dataDir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
			if err != nil {
				return err
			}
			owner, _, err := sd.Owner()
			if err != nil {
				return err
			}
			for _, path := range []string{filepath.Dir(filepath.Dir(target)), filepath.Dir(target)} {
				readAccess, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;GRGX;;;" + owner.String() + ")")
				if err != nil {
					return err
				}
				dacl, _, err := readAccess.DACL()
				if err != nil {
					return err
				}
				if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
					return err
				}
			}
			access, err := windows.SecurityDescriptorFromString("D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;CCLCRPWPLO;;;" + owner.String() + ")")
			if err != nil {
				return err
			}
			dacl, _, err := access.DACL()
			if err != nil {
				return err
			}
			if err := windows.SetSecurityInfo(service.Handle, windows.SE_SERVICE, windows.DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
				return err
			}
			if err := service.SetRecoveryActions([]mgr.RecoveryAction{
				{Type: mgr.ServiceRestart, Delay: 2 * time.Second},
				{Type: mgr.ServiceRestart, Delay: 5 * time.Second},
				{Type: mgr.ServiceRestart, Delay: 15 * time.Second},
				{Type: mgr.NoAction},
			}, 300); err != nil {
				return err
			}
			if err := service.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
				return err
			}
			if err := configureFirewall(target); err != nil {
				return err
			}
			return nil
		},
		start:  func() error { return service.Start() },
		verify: func() error { return waitWindowsServiceReady(service, dataDir, 30*time.Second) },
		rollback: func() error {
			var restoreErr error
			if service != nil {
				if err := stopWindowsService(service); err != nil {
					retainBackup = true
					return err
				}
			}
			if backup != "" {
				if err := replaceServiceBinary(backup, target); err != nil {
					retainBackup = true
					return fmt.Errorf("previous binary retained at %s: %w", backup, err)
				}
			} else if old == nil {
				restoreErr = errors.Join(restoreErr, os.Remove(target))
			}
			if old != nil {
				restoreErr = errors.Join(restoreErr, old.UpdateConfig(previousConfig))
				if wasRunning && restoreErr == nil {
					restoreErr = old.Start()
					if restoreErr == nil {
						restoreErr = waitWindowsServiceReady(old, dataDir, 30*time.Second)
					}
				}
			} else if service != nil {
				restoreErr = errors.Join(restoreErr, service.Delete())
				removeFirewall(target)
			}
			return restoreErr
		},
	}
	result := upgrade.run()
	if service != nil && old == nil {
		_ = service.Close()
	}
	return result
}

// A fully written, synced, hash-checked copy exists before SCM stop is sent.
func stageServiceBinary(source, directory string) (string, error) {
	in, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.CreateTemp(directory, ".core-*.exe")
	if err != nil {
		return "", err
	}
	path := out.Name()
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(out, hash), in)
	syncErr, closeErr := out.Sync(), out.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		os.Remove(path)
		return "", err
	}
	check, err := os.Open(path)
	if err != nil {
		os.Remove(path)
		return "", err
	}
	verify := sha256.New()
	_, err = io.Copy(verify, check)
	check.Close()
	if err != nil || !bytes.Equal(hash.Sum(nil), verify.Sum(nil)) {
		os.Remove(path)
		return "", errors.New("service copy checksum mismatch")
	}
	return path, nil
}

func replaceServiceBinary(source, target string) error {
	from, _ := windows.UTF16PtrFromString(source)
	to, _ := windows.UTF16PtrFromString(target)
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
		if err == nil || time.Now().After(deadline) {
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func waitWindowsServiceReady(service *mgr.Service, dataDir string, timeout time.Duration) error {
	secret, err := os.ReadFile(filepath.Join(dataDir, "core.secret"))
	if err != nil {
		return err
	}
	socket := filepath.Join(dataDir, "core.sock")
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		state, queryErr := service.Query()
		var status struct{ PID uint32 }
		raw, _ := os.ReadFile(filepath.Join(dataDir, "core.json"))
		if queryErr == nil && state.State == svc.Running && json.Unmarshal(raw, &status) == nil && status.PID == state.ProcessId {
			req, _ := http.NewRequestWithContext(ctx, "GET", "http://localhost/version", nil)
			req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(secret)))
			if resp, err := client.Do(req); err == nil {
				_, readErr := io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK && readErr == nil {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("service readiness: %w", ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}

const firewallRule = "Gopurr Core TUN"

func configureFirewall(binary string) error {
	// The system/mixed TUN stack needs Windows Firewall to allow the protected
	// core executable. Never disable the firewall or grant all programs access.
	removeFirewall(binary)
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	for _, direction := range []string{"in", "out"} {
		out, err := exec.Command(filepath.Join(system, "netsh.exe"), "advfirewall", "firewall", "add", "rule", "name="+firewallRule,
			"dir="+direction, "action=allow", "program="+binary, "profile=any", "enable=yes").CombinedOutput()
		if err != nil {
			return fmt.Errorf(localize.Text("配置 TUN 防火墙规则失败: %s: %w"), strings.TrimSpace(string(out)), err)
		}
	}
	return nil
}

func removeFirewall(binary string) {
	system, err := windows.GetSystemDirectory()
	if err != nil {
		return
	}
	_ = exec.Command(filepath.Join(system, "netsh.exe"), "advfirewall", "firewall", "delete", "rule", "name="+firewallRule, "program="+binary).Run()
}

func withWindowsService(access uint32, action func(*mgr.Service) error) error {
	h, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(h)
	name, _ := windows.UTF16PtrFromString(Label)
	s, err := windows.OpenService(h, name, access)
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(s)
	return action(&mgr.Service{Name: Label, Handle: s})
}

// EnsureRunning never elevates or interrupts a healthy service. Newly installed
// services grant the owning user only the rights needed for normal lifecycle.
func EnsureRunning() error {
	return withWindowsService(windows.SERVICE_QUERY_STATUS|windows.SERVICE_START, func(s *mgr.Service) error {
		state, err := s.Query()
		if err != nil {
			return err
		}
		if state.State == svc.Running || state.State == svc.StartPending {
			return nil
		}
		if state.State == svc.StopPending {
			return errors.New(localize.Text("TUN 服务正在停止，请稍后重试"))
		}
		return s.Start()
	})
}

func ForDataDir(dir string) bool {
	matched := false
	_ = withWindowsService(windows.SERVICE_QUERY_CONFIG, func(s *mgr.Service) error {
		config, err := s.Config()
		if err != nil {
			return err
		}
		args, err := windows.DecomposeCommandLine(config.BinaryPathName)
		matched = err == nil && len(args) == 3 && args[1] == "--windows-service" && strings.EqualFold(filepath.Clean(args[2]), filepath.Clean(dir))
		return err
	})
	return matched
}

func StopRunning() error {
	return withWindowsService(windows.SERVICE_QUERY_STATUS|windows.SERVICE_STOP, stopWindowsService)
}

func restartWindowsService() error {
	if err := StopRunning(); err != nil {
		return err
	}
	return EnsureRunning()
}

// Restart may elevate only in response to an explicit repair action, for
// services installed by an older version without owner lifecycle rights.
func Restart(dataDir string) error {
	if !ForDataDir(dataDir) {
		return errors.New(localize.Text("系统服务不属于当前数据目录，请重新安装服务"))
	}
	err := restartWindowsService()
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return elevate(exe, "restart", dataDir)
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
		removeFirewall(target)
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
	file, err := rotatelog.Open(filepath.Join(serviceDir, "core.log"))
	if err != nil {
		return true, 1
	}
	defer file.Close()
	log.SetOutput(file)
	logrus.SetOutput(file)
	result := make(chan error, 1)
	_ = os.Remove(filepath.Join(h.dir, "core.json"))
	go func() {
		result <- h.run([]string{"--config", filepath.Join(h.dir, "runtime.yaml"), "--socket", filepath.Join(h.dir, "core.sock"),
			"--secret-file", filepath.Join(h.dir, "core.secret"), "--home", filepath.Join(serviceDir, "mihomo"), "--status", filepath.Join(h.dir, "core.json")})
	}()
	status := svc.Status{State: svc.StartPending, WaitHint: 45000, CheckPoint: 1}
	changes <- status
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if status.State == svc.StartPending {
				var ready struct {
					PID int `json:"pid"`
				}
				raw, _ := os.ReadFile(filepath.Join(h.dir, "core.json"))
				if json.Unmarshal(raw, &ready) == nil && ready.PID == os.Getpid() {
					status = svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
				} else {
					status.CheckPoint++
				}
				changes <- status
			}
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
