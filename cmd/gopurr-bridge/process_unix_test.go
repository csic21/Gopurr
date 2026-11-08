//go:build cgo && !windows && !android

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// The frontend exits before the backend finishes network cleanup/relaunch.
// Its diagnostic reader must not become the backend's stderr lifetime owner.
func TestDesktopUnixLogSurvivesFrontendExit(t *testing.T) {
	const modeEnv = "GOPURR_DESKTOP_STDERR_TEST_MODE"
	const outputEnv = "GOPURR_DESKTOP_STDERR_TEST_OUTPUT"
	switch os.Getenv(modeEnv) {
	case "frontend":
		child := exec.Command(os.Args[0], "-test.run=^TestDesktopUnixLogSurvivesFrontendExit$")
		child.Env = append(os.Environ(), modeEnv+"=backend", "GOPURR_DESKTOP_STDERR_TEST_PARENT="+strconv.Itoa(os.Getpid()))
		child.Stderr = &desktopStartupLog{}
		configureDesktopCommand(child)
		if child.Start() != nil {
			os.Exit(2)
		}
		os.Exit(0)
	case "backend":
		parent, _ := strconv.Atoi(os.Getenv("GOPURR_DESKTOP_STDERR_TEST_PARENT"))
		until := time.Now().Add(5 * time.Second)
		for os.Getppid() == parent && time.Now().Before(until) {
			time.Sleep(10 * time.Millisecond)
		}
		// Writing fd 2 to a vanished diagnostic pipe terminates a Go process with
		// SIGPIPE, before it can request the staged updater's relaunch.
		_, _ = fmt.Fprintln(os.Stderr, "frontend exited; requesting relaunch")
		if os.WriteFile(os.Getenv(outputEnv), []byte("relaunch reached"), 0600) != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
	dir := t.TempDir()
	output := filepath.Join(dir, "relaunch")
	inherited, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	defer inherited.Close()
	frontend := exec.Command(os.Args[0], "-test.run=^TestDesktopUnixLogSurvivesFrontendExit$")
	frontend.Env = append(os.Environ(), modeEnv+"=frontend", outputEnv+"="+output)
	frontend.Stderr = inherited
	if err := frontend.Run(); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(6 * time.Second)
	for time.Now().Before(until) {
		if body, err := os.ReadFile(output); err == nil && string(body) == "relaunch reached" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("backend died on stderr after frontend exit, before requesting relaunch")
}
