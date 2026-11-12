// Package instance lets a second launch activate the desktop that already
// holds a data directory. The socket replaces the old toolkit singleton.
package instance

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"time"
)

type message struct {
	Args []string `json:"args"`
}

// Listen accepts activation messages until closed. The socket stays in the
// data directory and is removed on close.
func Listen(home string, on func([]string)) (func(), error) {
	path := filepath.Join(home, "instance.sock")
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err = os.Chmod(path, 0o600); err != nil {
		ln.Close()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-done:
					return
				default:
				}
				if errors.Is(err, net.ErrClosed) {
					return
				}
				continue
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				var msg message
				if json.NewDecoder(conn).Decode(&msg) == nil && on != nil {
					on(msg.Args)
				}
			}(conn)
		}
	}()
	return func() {
		close(done)
		ln.Close()
		_ = os.Remove(path)
	}, nil
}

// Signal delivers args to the instance listening on home. It retries briefly
// so a launch that wins the race against Listen still activates.
func Signal(home string, args []string) error {
	path := filepath.Join(home, "instance.sock")
	body, err := json.Marshal(message{Args: args})
	if err != nil {
		return err
	}
	var last error
	deadline := time.Now().Add(2 * time.Second)
	for {
		conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
		if err == nil {
			_, werr := conn.Write(body)
			conn.Close()
			return werr
		}
		last = err
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(50 * time.Millisecond)
	}
}
