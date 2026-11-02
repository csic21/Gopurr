//go:build darwin

package sysproxy

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopurr/internal/localize"
)

var errServiceUnavailable = errors.New("system proxy service unavailable")

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

func connectProxyService(dir string) (net.Conn, error) {
	return connectProxyServiceUsing(dir, func(c *net.UnixConn) error { return checkHelperPeer(c, 0, 0) })
}

func connectProxyServiceUsing(dir string, verify func(*net.UnixConn) error) (net.Conn, error) {
	secret, err := os.ReadFile(filepath.Join(dir, "core.secret"))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errServiceUnavailable, err)
	}
	conn, err := net.DialTimeout("unix", filepath.Join(dir, "core.sock"), time.Second)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errServiceUnavailable, err)
	}
	success := false
	defer func() {
		if !success {
			conn.Close()
		}
	}()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := verify(conn.(*net.UnixConn)); err != nil {
		return nil, fmt.Errorf("%w: %v", errServiceUnavailable, err)
	}
	request, _ := http.NewRequest(http.MethodConnect, "http://localhost/gopurr/system-proxy", nil)
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(secret)))
	if err := request.Write(conn); err != nil {
		return nil, err
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusNotImplemented {
			return nil, errServiceUnavailable // pre-broker services remain usable
		}
		return nil, fmt.Errorf(localize.Text("系统代理服务暂不可用（HTTP %d），请检查服务状态"), response.StatusCode)
	}
	_ = conn.SetDeadline(time.Time{})
	success = true
	return &bufferedConn{conn, reader}, nil
}

// ServiceBroker shares the existing privileged daemon's authorization. The
// authenticated gateway owns access; a connection owns one proxy lease. EOF
// restores on UI crash, and a protected journal survives daemon crashes.
// Only typed proxy operations are accepted, never paths or shell commands.
type ServiceBroker struct {
	mu         sync.Mutex
	stateMu    sync.Mutex
	journal    string
	command    commandRunner
	privileged bool
	conn       net.Conn
	closed     bool
}

func NewServiceBroker(journal string) *ServiceBroker {
	return &ServiceBroker{journal: journal, command: privilegedCommand, privileged: os.Geteuid() == 0}
}

func (b *ServiceBroker) Recover() error {
	if !b.privileged {
		return nil
	}
	leases, err := loadLeases(b.journal, b.command)
	if err != nil {
		return err
	}
	return leases.restore()
}

func (b *ServiceBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !b.privileged {
		http.Error(w, "Privileged service required", http.StatusServiceUnavailable)
		return
	}
	if !b.mu.TryLock() {
		http.Error(w, "Proxy session already active", http.StatusConflict)
		return
	}
	defer b.mu.Unlock()
	leases, err := loadLeases(b.journal, b.command)
	if err == nil {
		err = leases.restore()
	}
	if err != nil {
		http.Error(w, "Proxy recovery failed", http.StatusServiceUnavailable)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Streaming unavailable", http.StatusInternalServerError)
		return
	}
	conn, buffer, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	// Close is called before kernel shutdown, including hijacked connections.
	b.stateMu.Lock()
	if b.closed {
		b.stateMu.Unlock()
		return
	}
	b.conn = conn
	b.stateMu.Unlock()
	defer func() { b.stateMu.Lock(); b.conn = nil; b.stateMu.Unlock() }()
	_, _ = buffer.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
	if err := buffer.Flush(); err != nil {
		return
	}
	_ = serveProxySession(&bufferedConn{conn, buffer.Reader}, leases)
}

func (b *ServiceBroker) Close() {
	// The handler holds mu while reading the session. Closing its connection
	// wakes the handler, which restores and releases mu before this returns.
	b.stateMu.Lock()
	b.closed = true
	conn := b.conn
	b.stateMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	b.mu.Lock()
	b.mu.Unlock()
}
