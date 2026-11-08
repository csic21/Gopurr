// Package nativehost connects the NSH desktop window to the existing desktop
// application. Its authenticated socket is private to one frontend process.
package nativehost

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const MaxMessage = 32 << 20

type Request struct {
	Token  string          `json:"token,omitempty"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
type Response struct {
	Result any    `json:"result"`
	Error  *Error `json:"error,omitempty"`
}
type Handler func(context.Context, string, json.RawMessage) (any, error)

type Server struct {
	listener    net.Listener
	path        string
	socketInfo  os.FileInfo
	token       string
	handler     Handler
	stop        context.CancelFunc
	wg          sync.WaitGroup
	mu          sync.Mutex
	connections map[net.Conn]bool
	closed      bool
}

func Listen(path, token string, handler Handler) (*Server, error) {
	if len(token) < 32 || len(token) > 4096 || handler == nil {
		return nil, errors.New("invalid native host configuration")
	}
	if err := privateParent(path); err != nil {
		return nil, err
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0600); err != nil {
		l.Close()
		return nil, err
	}
	socketInfo, err := os.Lstat(path)
	if err != nil {
		l.Close()
		return nil, err
	}
	if unixListener, ok := l.(*net.UnixListener); ok {
		unixListener.SetUnlinkOnClose(false)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &Server{listener: l, path: path, socketInfo: socketInfo, token: token, handler: handler, stop: cancel, connections: map[net.Conn]bool{}}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				c.Close()
				return
			}
			s.connections[c] = true
			s.wg.Add(1)
			s.mu.Unlock()
			go func() {
				defer s.wg.Done()
				defer func() { c.Close(); s.mu.Lock(); delete(s.connections, c); s.mu.Unlock() }()
				s.serve(ctx, c)
			}()
		}
	}()
	return s, nil
}
func (s *Server) serve(parent context.Context, c net.Conn) {
	_ = c.SetDeadline(time.Now().Add(3 * time.Minute))
	var req Request
	d := json.NewDecoder(io.LimitReader(c, MaxMessage+1))
	if err := d.Decode(&req); err != nil || d.InputOffset() > MaxMessage {
		_ = json.NewEncoder(c).Encode(Response{Error: &Error{"invalid_request", "Invalid native request"}})
		return
	}
	if subtle.ConstantTimeCompare([]byte(req.Token), []byte(s.token)) != 1 {
		_ = json.NewEncoder(c).Encode(Response{Error: &Error{"unauthorized", "Unauthorized native request"}})
		return
	}
	ctx, cancel := context.WithTimeout(parent, 150*time.Second)
	defer cancel()
	// One request owns one connection. A disappearing frontend must cancel
	// queued work and network reads rather than leaving them alive for 150s.
	// Atomic model transactions may finish their rollback before returning.
	go func() { var extra [1]byte; _, _ = c.Read(extra[:]); cancel() }()
	var response Response
	func() {
		defer func() {
			if recover() != nil {
				response = Response{Error: &Error{"internal", "Native request failed"}}
			}
		}()
		result, err := s.handler(ctx, req.Method, req.Params)
		if err != nil {
			response.Error = &Error{"request_failed", err.Error()}
		} else {
			response.Result = result
		}
	}()
	_ = json.NewEncoder(c).Encode(response)
}
func (s *Server) Close() error {
	s.stop()
	err := s.listener.Close()
	if current, statErr := os.Lstat(s.path); statErr == nil && os.SameFile(current, s.socketInfo) {
		_ = os.Remove(s.path)
	}
	s.mu.Lock()
	s.closed = true
	for c := range s.connections {
		c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	return err
}

func Call(ctx context.Context, path, token, method string, params json.RawMessage) ([]byte, error) {
	c, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if err = json.NewEncoder(c).Encode(Request{Token: token, Method: method, Params: params}); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	// A response is one JSON value, just like the request. Do not wait for
	// socket EOF after receiving it: teardown of a Windows Unix socket can
	// lag the response and must not stall the next tray/quit event poll.
	limited := &io.LimitedReader{R: c, N: MaxMessage + 1}
	decoder := json.NewDecoder(limited)
	var b json.RawMessage
	err = decoder.Decode(&b)
	if limited.N == 0 && (err != nil || decoder.InputOffset() > MaxMessage) {
		return nil, fmt.Errorf("native response exceeds %d bytes", MaxMessage)
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	if len(b) > MaxMessage || decoder.InputOffset() > MaxMessage {
		return nil, fmt.Errorf("native response exceeds %d bytes", MaxMessage)
	}
	if !json.Valid(b) {
		return nil, errors.New("invalid native response")
	}
	return b, nil
}

// Events are sequenced so a failed poll can retry without dropping a tray or
// shortcut action. Only the last 128 host events need to be retained.
type Event struct {
	ID    uint64 `json:"id"`
	Kind  string `json:"kind"`
	Value any    `json:"value,omitempty"`
}
type Events struct {
	mu   sync.Mutex
	next uint64
	list []Event
}

func (e *Events) Add(kind string, value any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.next++
	e.list = append(e.list, Event{e.next, kind, value})
	if len(e.list) > 128 {
		e.list = e.list[len(e.list)-128:]
	}
}
func (e *Events) Since(after uint64) []Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []Event{}
	for _, v := range e.list {
		if v.ID > after {
			out = append(out, v)
		}
	}
	return out
}

// A socket or bearer credential must live in a private session directory.
// Validate, rather than chmod, a caller-supplied parent: changing a shared
// directory such as /tmp would be destructive.
func privateParent(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("absolute native session path required")
	}
	info, err := os.Lstat(filepath.Dir(path))
	if err != nil {
		return err
	}
	if !info.IsDir() || privateAccess(filepath.Dir(path)) != nil {
		return errors.New("native session directory must be private (0700)")
	}
	return nil
}

// ReadToken rejects links, public credentials and oversized files before the
// backend starts the model or touches any kernel/configuration state.
func ReadToken(path string) (string, error) {
	if err := privateParent(path); err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || privateAccess(path) != nil || info.Size() < 32 || info.Size() > 4096 {
		return "", errors.New("invalid private native session credential")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(raw) < 32 || len(raw) > 4096 {
		return "", errors.New("invalid native session credential size")
	}
	for _, c := range raw {
		if c < 33 || c > 126 {
			return "", errors.New("invalid native session credential encoding")
		}
	}
	return string(raw), nil
}

// CleanupSession is called by the backend as well as the frontend waiter: the
// frontend can disappear before its process-local cleanup goroutine runs.
// Only matching private credentials authorize removal. No recursive deletion
// is used, so an integration/test session containing other files is retained.
func CleanupSession(socketPath, tokenPath, token string) error {
	if filepath.Dir(socketPath) != filepath.Dir(tokenPath) {
		return errors.New("native socket and token do not share a session directory")
	}
	current, err := ReadToken(tokenPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare([]byte(current), []byte(token)) != 1 {
		return errors.New("native session credential changed")
	}
	if info, err := os.Lstat(socketPath); err == nil {
		if info.Mode()&os.ModeSocket == 0 {
			return errors.New("native socket path was replaced")
		}
		if err := os.Remove(socketPath); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(tokenPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// Remove only an empty private session directory; other files belong to the
	// caller and must survive, including smoke-test homes and diagnostic logs.
	_ = os.Remove(filepath.Dir(tokenPath))
	return nil
}
