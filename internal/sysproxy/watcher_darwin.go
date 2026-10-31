//go:build darwin

package sysproxy

import "gopurr/internal/localize"

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const watcherFlag = "--sysproxy-watch"

// The unprivileged direct path needs its own survivor as well: a parent killed
// with SIGKILL cannot run Close. No administrator prompt or shell is involved.
func startLeaseWatcher(journal string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, watcherFlag, "--journal", journal, "--parent", strconv.Itoa(os.Getpid()))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf(localize.Text("启动系统代理恢复助手失败: %w"), err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func runLeaseWatcher(args []string) error {
	fs := flag.NewFlagSet("sysproxy-watch", flag.ContinueOnError)
	journal := fs.String("journal", "", "proxy recovery journal")
	parent := fs.Int("parent", 0, "application pid")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *parent <= 1 || !filepath.IsAbs(*journal) || fs.NArg() != 0 {
		return errors.New(localize.Text("系统代理恢复助手参数无效"))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return watchLease(ctx, *journal, *parent, func() bool {
		err := syscall.Kill(*parent, 0)
		return err == nil || errors.Is(err, syscall.EPERM)
	}, run, 2*time.Second)
}

func watchLease(ctx context.Context, journal string, parent int, alive func() bool, command commandRunner, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if alive() {
				continue
			}
			unlock, err := lockJournal(journal)
			if err != nil {
				return err
			}
			defer unlock()
			leases, err := loadLeases(journal, command)
			if err != nil {
				return err
			}
			if leases.owner != parent {
				return nil // a newer application session has already taken over
			}
			// Leave the durable record available if networksetup rejects a
			// restore; the next application launch can retry it.
			return leases.restore()
		}
	}
}
