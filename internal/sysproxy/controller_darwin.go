//go:build darwin

package sysproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const helperFlag = "--sysproxy-helper"

type helperRequest struct {
	Operation string
	Port      int
	Previous  *State
}

type helperResponse struct {
	Error string
}

// The helper dials a private socket owned by the UI. Both ends verify the
// peer's kernel credentials; it accepts only typed proxy operations, never
// shell commands, paths to executables, or arbitrary network services.
type helperController struct {
	mu    sync.Mutex
	conn  net.Conn
	input *bufio.Scanner
	open  func() (net.Conn, error)
	port  int
}

func NewController() Controller { return &helperController{open: startProxyHelper} }

func (c *helperController) Enable(port int, previous *State) error {
	if err := validateProxy("127.0.0.1", port); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.port = port
	if c.conn == nil {
		conn, err := c.open()
		if err != nil {
			return err
		}
		c.conn, c.input = conn, helperScanner(conn)
	}
	return c.request(helperRequest{Operation: "enable", Port: port, Previous: previous})
}

func (c *helperController) Restore(previous *State) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return c.request(helperRequest{Operation: "restore"})
	}
	// Never try to recover missing privileges by prompting during quit.
	if current, err := Current(); err == nil && (!current.Enabled || (c.port > 0 &&
		(current.Server != "127.0.0.1" || current.Port != c.port))) {
		return nil
	}
	return errors.New("系统代理权限会话已断开，请重新开启系统代理后重试")
}

func (c *helperController) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	if err := c.request(helperRequest{Operation: "restore"}); err != nil {
		return err
	}
	err := c.conn.Close()
	c.conn, c.input = nil, nil
	return err
}

func (c *helperController) request(request helperRequest) error {
	_ = c.conn.SetDeadline(time.Now().Add(20 * time.Second))
	if err := json.NewEncoder(c.conn).Encode(request); err != nil {
		c.disconnect()
		return fmt.Errorf("系统代理助手连接中断: %w", err)
	}
	if !c.input.Scan() {
		err := errors.Join(io.EOF, c.input.Err())
		c.disconnect()
		return fmt.Errorf("系统代理助手未响应: %w", err)
	}
	var response helperResponse
	if err := json.Unmarshal(c.input.Bytes(), &response); err != nil {
		c.disconnect()
		return err
	}
	if response.Error != "" {
		return errors.New(response.Error)
	}
	return nil
}

func (c *helperController) disconnect() {
	_ = c.conn.Close()
	c.conn, c.input = nil, nil
}

func helperScanner(reader io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 16*1024)
	return scanner
}

func startProxyHelper() (net.Conn, error) {
	dir, err := os.MkdirTemp("/tmp", "gopurr-proxy-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "helper.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	defer listener.Close()
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	command := fmt.Sprintf("%s %s --socket %s --parent %d >/dev/null 2>&1 &",
		shellQuote(executable), helperFlag, shellQuote(path), os.Getpid())
	// Only starting the session is interactive. The helper retains root until
	// the UI disconnects, regardless of Authorization Services' cache expiry.
	apple := "do shell script " + strconv.Quote(command) + " with administrator privileges"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "osascript", "-e", apple)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(strings.ToLower(string(output)), "cancel") || strings.Contains(string(output), "-128") {
			return nil, errors.New("已取消授权")
		}
		return nil, fmt.Errorf("启动系统代理助手失败: %s: %w", strings.TrimSpace(string(output)), err)
	}
	_ = listener.SetDeadline(time.Now().Add(5 * time.Second))
	conn, err := listener.AcceptUnix()
	if err != nil {
		return nil, fmt.Errorf("系统代理助手未连接: %w", err)
	}
	if err := checkHelperPeer(conn, 0, 0); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func checkHelperPeer(conn *net.UnixConn, wantUID, wantPID int) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var peerErr error
	if err := raw.Control(func(fd uintptr) {
		credential, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if err != nil {
			peerErr = err
			return
		}
		if wantUID >= 0 && credential.Uid != uint32(wantUID) {
			peerErr = errors.New("系统代理助手身份验证失败")
			return
		}
		if wantPID > 0 {
			pid, err := unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
			if err != nil || pid != wantPID {
				peerErr = errors.New("系统代理助手的应用进程不匹配")
			}
		}
	}); err != nil {
		return err
	}
	return peerErr
}

// HandleCommand runs before AppKit or user configuration is initialized.
func HandleCommand(args []string) (bool, error) {
	if len(args) == 0 || args[0] != helperFlag {
		return false, nil
	}
	fs := flag.NewFlagSet("sysproxy-helper", flag.ContinueOnError)
	path := fs.String("socket", "", "private application socket")
	parent := fs.Int("parent", 0, "application pid")
	if err := fs.Parse(args[1:]); err != nil {
		return true, err
	}
	if os.Geteuid() != 0 || *parent <= 1 || !filepath.IsAbs(*path) || fs.NArg() != 0 {
		return true, errors.New("系统代理助手需要管理员权限和有效的应用连接")
	}
	conn, err := net.DialTimeout("unix", *path, 5*time.Second)
	if err != nil {
		return true, err
	}
	defer conn.Close()
	if err := checkHelperPeer(conn.(*net.UnixConn), -1, *parent); err != nil {
		return true, err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	return true, serveProxyHelper(conn, privilegedCommand)
}

func privilegedCommand(name string, args ...string) (string, error) {
	// Never search a user-controlled PATH in the elevated process.
	switch name {
	case "networksetup":
		return run("/usr/sbin/networksetup", args...)
	case "route":
		return run("/sbin/route", args...)
	default:
		return "", errors.New("不支持的系统代理命令")
	}
}

func serveProxyHelper(conn net.Conn, command commandRunner) (result error) {
	leases := proxyLeases{command: command, services: make(map[string]*proxyLease)}
	// EOF also covers crashes and force-quit: restore without involving AppKit
	// or asking a user who may no longer have an application window.
	defer func() { result = errors.Join(result, leases.restore()) }()
	input := helperScanner(conn)
	for input.Scan() {
		var request helperRequest
		err := json.Unmarshal(input.Bytes(), &request)
		if err == nil {
			switch request.Operation {
			case "enable":
				err = leases.enable(request.Port, request.Previous)
			case "restore":
				err = leases.restore()
			default:
				err = errors.New("不支持的系统代理操作")
			}
		}
		response := helperResponse{}
		if err != nil {
			response.Error = err.Error()
		}
		_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := json.NewEncoder(conn).Encode(response); err != nil {
			return err
		}
	}
	return input.Err()
}
