package nativehost

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestHomeLockExcludesDuplicateModelAndReleases(t *testing.T) {
	home := t.TempDir()
	first, err := LockHome(home)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	if second, err := LockHome(home); !errors.Is(err, ErrHomeInUse) {
		if second != nil {
			second.Close()
		}
		t.Fatal("same home was not excluded", err)
	}
	other, err := LockHome(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := LockHome(home)
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
	info, err := os.Stat(filepath.Join(home, "desktop.lock"))
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) || (info != nil && !info.Mode().IsRegular()) {
		t.Fatal("lock file must remain private and stable", err)
	}
}
func TestHomeLockIsReleasedByProcessExit(t *testing.T) {
	if os.Getenv("GOPURR_TEST_HOME_LOCK_CHILD") == "1" {
		lock, err := LockHome(os.Getenv("GOPURR_TEST_HOME_LOCK_DIR"))
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		fmt.Println("LOCKED")
		_, _ = io.Copy(io.Discard, os.Stdin)
		return
	}
	home := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestHomeLockIsReleasedByProcessExit$")
	command.Env = append(os.Environ(), "GOPURR_TEST_HOME_LOCK_CHILD=1", "GOPURR_TEST_HOME_LOCK_DIR="+home)
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if command.ProcessState == nil {
			command.Process.Kill()
			command.Wait()
		}
	}()
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || line != "LOCKED\n" {
		t.Fatal("lock child not ready", line, err)
	}
	if lock, err := LockHome(home); !errors.Is(err, ErrHomeInUse) {
		if lock != nil {
			lock.Close()
		}
		t.Fatal("second process could lock the home", err)
	}
	// Only this dedicated test child is killed; it never creates an app/core.
	command.Process.Kill()
	command.Wait()
	lock, err := LockHome(home)
	if err != nil {
		t.Fatal("crashed process left a stale home lock", err)
	}
	lock.Close()
}

func TestHomeLockRefusesSharedTemporaryRoot(t *testing.T) {
	lock, err := LockHome(os.TempDir())
	if lock != nil {
		lock.Close()
	}
	if err == nil {
		t.Fatal("shared temporary root was accepted as an application home")
	}
}
