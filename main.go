package main

import (
	"errors"
	"fmt"
	"log"
	"os"

	"flag"

	"gopurr/internal/app"
	"gopurr/internal/buildinfo"
	"gopurr/internal/core"
	"gopurr/internal/instance"
	"gopurr/internal/nativehost"
	"gopurr/internal/service"
	"gopurr/internal/sysproxy"
)

// runDaemon parses the --core flags and blocks until the process is asked to
// stop. It is deliberately dumb: everything it does comes from the config.
func runDaemon(args []string) error {
	fs := flag.NewFlagSet("core", flag.ContinueOnError)
	config := fs.String("config", "", "config file to run")
	socket := fs.String("socket", "", "unix socket for the controller")
	secret := fs.String("secret", "", "controller secret")
	secretFile := fs.String("secret-file", "", "read the controller secret from this file")
	parent := fs.Int("parent", 0, "exit when this pid disappears")
	home := fs.String("home", "", "kernel home (geoip, cache)")
	status := fs.String("status", "", "status file written once ready")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *config == "" || *socket == "" {
		return errors.New("--config and --socket are required")
	}
	return core.RunDaemon(core.DaemonOptions{
		ConfigPath: *config,
		Socket:     *socket,
		Secret:     *secret,
		SecretFile: *secretFile,
		ParentPID:  *parent,
		Home:       *home,
		StatusPath: *status,
	})
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Printf("Gopurr %s\n", buildinfo.Version)
		return
	}
	if handled, err := sysproxy.HandleCommand(os.Args[1:]); handled {
		if err != nil {
			log.Printf("system proxy helper: %v", err)
			os.Exit(1)
		}
		return
	}
	if handled, err := service.HandleCommand(os.Args[1:], runDaemon); handled {
		if err != nil {
			log.Printf("service: %v", err)
			os.Exit(1)
		}
		return
	}
	// The privileged helper runs the same binary with a flag, so a single
	// artifact is both the UI and the thing root executes.
	// The kernel side of the app: one process, one config, the standard
	// mihomo controller on a unix socket. Installed as a LaunchDaemon when it
	// needs to be root (TUN), started by the app otherwise.
	if len(os.Args) > 1 && os.Args[1] == "--core" {
		if err := runDaemon(os.Args[2:]); err != nil {
			log.Printf("core: %v", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--native-backend" {
		if err := runNativeBackend(os.Args[2:]); err != nil {
			if errors.Is(err, nativehost.ErrHomeInUse) {
				os.Exit(23)
			}
			log.Printf("native desktop: %v", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--native-activate" {
		home, err := app.DataDirectory()
		if err != nil {
			log.Printf("native activate: %v", err)
			os.Exit(1)
		}
		if err = instance.Signal(home, os.Args[1:]); err != nil {
			log.Printf("native activate: %v", err)
			os.Exit(1)
		}
		return
	}
	log.Fatal("Gopurr's desktop is the native host. Launch the app bundle, or pass --native-backend from that host.")
}
