// Command verify-update installs a real signed release into a disposable old
// app. It exercises the same checker, downloader, verifier and replacement
// code as the desktop UI, without touching the user's installed application.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	artifacts := flag.String("artifacts", "", "platform release artifact directory")
	flag.Parse()
	dir, err := filepath.Abs(*artifacts)
	if err != nil {
		return err
	}
	manifestName := "update-" + runtime.GOOS + "-" + runtime.GOARCH + ".json"
	body, err := os.ReadFile(filepath.Join(dir, manifestName))
	if err != nil {
		return err
	}
	var manifest map[string]any
	if err := json.Unmarshal(body, &manifest); err != nil {
		return err
	}
	version, _ := manifest["version"].(string)
	archiveURL, _ := manifest["url"].(string)
	parsed, err := url.Parse(archiveURL)
	if err != nil {
		return err
	}
	archive := filepath.Base(parsed.Path)
	var config struct {
		Updates struct {
			PublicKey string `json:"publicKey"`
		} `json:"updates"`
	}
	configBody, err := os.ReadFile("mygo.json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(configBody, &config); err != nil {
		return err
	}
	if err := os.MkdirAll(".cache", 0o755); err != nil {
		return err
	}
	work, err := os.MkdirTemp(".cache", "update-roundtrip-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	work, err = filepath.Abs(work)
	if err != nil {
		return err
	}
	var response []byte
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/update.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(response)
		case "/" + archive:
			http.ServeFile(w, r, filepath.Join(dir, archive))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	manifest["url"] = "http://" + server.Listener.Addr().String() + "/" + archive
	response, err = json.Marshal(manifest)
	if err != nil {
		return err
	}
	server.Start()
	source := filepath.Join(work, "main.go")
	if err := os.WriteFile(source, []byte(fixture), 0o600); err != nil {
		return err
	}
	appDir := filepath.Join(work, "app")
	binary := filepath.Join(appDir, "Gopurr.exe")
	if runtime.GOOS == "darwin" {
		appDir = filepath.Join(work, "Gopurr.app")
		binary = filepath.Join(appDir, "Contents", "MacOS", "Gopurr")
	}
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		return err
	}
	flags := "-X github.com/egoist/mygo.production=1 -X github.com/egoist/mygo.packageUpdateFeed=" + server.URL + "/update.json -X github.com/egoist/mygo.packageUpdateKey=" + config.Updates.PublicKey
	build := exec.Command("go", "build", "-ldflags", flags, "-o", binary, source)
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	install := exec.CommandContext(ctx, binary)
	install.Stdout, install.Stderr = os.Stdout, os.Stderr
	if err := install.Run(); err != nil {
		return fmt.Errorf("update installation: %w", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		checkCtx, checkCancel := context.WithTimeout(context.Background(), 3*time.Second)
		out, err := exec.CommandContext(checkCtx, binary, "--version").CombinedOutput()
		checkCancel()
		if err == nil && strings.TrimSpace(string(out)) == "Gopurr "+version {
			fmt.Printf("Signed update roundtrip passed on %s/%s: 0.0.0 -> %s.\n", runtime.GOOS, runtime.GOARCH, version)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("the installed application did not report the release version")
}

const fixture = `package main
import (
 "context"
 "fmt"
 "os"
 "time"
 "github.com/egoist/mygo"
 "gopurr/internal/update"
)
func main() {
 if len(os.Args)==2 && os.Args[1]=="--version" {fmt.Println("Gopurr 0.0.0");return}
 mygo.App.SetName("Gopurr")
 mygo.App.SetVersion("0.0.0")
 ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second);defer cancel()
 backend:=update.Native{}
 if !backend.Enabled(){fmt.Fprintln(os.Stderr,"fixture cannot update");os.Exit(1)}
 release,err:=backend.Check(ctx)
 if err!=nil||release==nil{fmt.Fprintf(os.Stderr,"check: %v\n",err);os.Exit(1)}
 if err:=release.Install(ctx,nil);err!=nil{fmt.Fprintf(os.Stderr,"install: %v\n",err);os.Exit(1)}
 fmt.Println("Signed update installed/staged:",release.Version)
}
`
