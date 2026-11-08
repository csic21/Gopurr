package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopurr/internal/nativehost"
	"gopurr/internal/securefs"
)

// Actual Win32 host + Go DLL + backend. Every installation/data directory and
// key is disposable; updates travel over loopback only. No OS proxy/TUN change
// or user profile is permitted by the fixture.
func TestWindowsNativeMigrationRoundtrip(t *testing.T) {
	source := os.Getenv("GOPURR_NATIVE_WINDOWS_TEST_APP")
	if source == "" {
		t.Skip("set GOPURR_NATIVE_WINDOWS_TEST_APP on Windows x64")
	}
	source, _ = filepath.Abs(source)
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	work, err := os.MkdirTemp(filepath.Join(repo, ".cache"), "windows-nsh-roundtrip-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(work)
	home := filepath.Join(work, "home")
	if err := securefs.PrivateDir(home); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "profiles"), 0700); err != nil {
		t.Fatal(err)
	}
	seed := map[string]string{
		"prefs.json":           `{"appearance":"dark","language":"en","systemProxyStartup":"off","restoreSystemProxy":false,"checkAppUpdates":false,"autoUpdateHours":0}`,
		"local.yaml":           "mode: rule\nrules: [MATCH,DIRECT]\n",
		"profiles.json":        `{"active":"legacy","profiles":[{"id":"legacy","name":"Retained subscription","kind":"url","url":"https://example.invalid/sub?token=fixture-secret","file":"legacy.yaml"}]}`,
		"profiles/legacy.yaml": "mode: rule\nproxies: [{name: retained-node, type: http, server: 127.0.0.1, port: 9, password: fixture-secret}]\nproxy-groups: [{name: Route, type: select, proxies: [DIRECT, REJECT]}]\nrules: [MATCH,Route]\n",
		"overrides.json":       `{"mixedPort":17889,"mode":"rule","tunEnable":false}`,
	}
	for name, body := range seed {
		if err := securefs.WriteFile(filepath.Join(home, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var offer atomic.Int32
	var corrupt atomic.Bool
	artifacts := filepath.Join(work, "artifacts")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Path)
		if strings.HasPrefix(name, "update-") {
			version := "0.0.1"
			if name == nativeManifest && offer.Load() == 2 {
				version = "0.0.2"
			}
			body, err := os.ReadFile(filepath.Join(artifacts, version, nativeManifest))
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			if corrupt.Load() {
				var m manifest
				_ = json.Unmarshal(body, &m)
				m.Signature = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
				body, _ = json.Marshal(m)
			}
			_, _ = w.Write(body)
			return
		}
		for _, version := range []string{"0.0.1", "0.0.2"} {
			path := filepath.Join(artifacts, version, name)
			if _, err := os.Stat(path); err == nil {
				http.ServeFile(w, r, path)
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	for _, version := range []string{"0.0.1", "0.0.2"} {
		app := filepath.Join(work, "build-"+version)
		copyFixtureTree(t, source, app)
		var c config
		c.Name, c.Slug, c.Version = "Gopurr", "gopurr", version
		c.Updates.URL = server.URL
		c.Updates.PublicKey = base64.StdEncoding.EncodeToString(pub)
		flags := "-H=windowsgui -X github.com/egoist/mygo.production=1 -X github.com/egoist/mygo.packageName=Gopurr -X github.com/egoist/mygo.packageIdentifier=io.github.gopurr.app -X github.com/egoist/mygo.packageVersion=" + version + " -X gopurr/internal/buildinfo.Version=" + version + " -X github.com/egoist/mygo.packageUpdateFeed=" + feed(c) + " -X github.com/egoist/mygo.packageUpdateKey=" + c.Updates.PublicKey
		fixtureCommand(t, repo, nil, "go", "build", "-ldflags", flags, "-o", filepath.Join(app, "gopurr-desktop.exe"), ".")
		env := append(os.Environ(), "GOPURR_VERSION="+version, "GOPURR_WINDOWS_ICON="+filepath.Join(app, "Gopurr.ico"), "RUSTFLAGS=-C target-feature=+crt-static")
		fixtureCommand(t, repo, env, "cargo", "build", "--locked", "--release", "--manifest-path", "native/windows/Cargo.toml")
		cargoTarget := os.Getenv("CARGO_TARGET_DIR")
		if cargoTarget == "" {
			t.Fatal("CARGO_TARGET_DIR must use the workspace cache")
		}
		body, err := os.ReadFile(filepath.Join(cargoTarget, "release/Gopurr.exe"))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(app, "Gopurr.exe"), body, 0755); err != nil {
			t.Fatal(err)
		}
		m := marker{Schema: 1, UI: "nsh-desktop", Backend: "desktop-v2", Platform: target, Version: version, UpdateFeed: feed(c), UpdatePublicKey: c.Updates.PublicKey, UpdateEnabled: true, DataDirectory: "Gopurr"}
		body, _ = json.Marshal(m)
		if err = os.WriteFile(filepath.Join(app, "native-build.json"), body, 0644); err != nil {
			t.Fatal(err)
		}
		// Only the in-memory test config permits loopback. Production config
		// parsing still requires HTTPS and the established production key.
		if err := validate(app, c); err != nil {
			t.Fatal(err)
		}
		if err := packageDirectory(app, filepath.Join(artifacts, version), c, key, ""); err != nil {
			t.Fatal(err)
		}
	}
	installed := filepath.Join(work, "installed")
	if err := os.MkdirAll(installed, 0700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(installed, "Gopurr.exe")
	sourcePath := filepath.Join(work, "legacy.go")
	if err := os.WriteFile(sourcePath, []byte(windowsLegacyFixture), 0600); err != nil {
		t.Fatal(err)
	}
	flags := "-X github.com/egoist/mygo.production=1 -X github.com/egoist/mygo.packageUpdateFeed=" + server.URL + "/update-windows-amd64.json -X github.com/egoist/mygo.packageUpdateKey=" + base64.StdEncoding.EncodeToString(pub)
	fixtureCommand(t, repo, nil, "go", "build", "-ldflags", flags, "-o", binary, sourcePath)
	sessionPath := filepath.Join(work, "session.json")
	old := exec.Command(binary, "--gopurr-home", home)
	old.Env = append(os.Environ(), "GOPURR_HOME="+home, "GOPURR_NATIVE_SESSION_OUTPUT="+sessionPath, "GOPURR_AUTOSTART=1", "GOPURR_TUN=0")
	logFile, err := os.Create(filepath.Join(work, "host.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	old.Stdout, old.Stderr = logFile, logFile
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	defer old.Process.Kill()
	var session windowsSession
	defer func() {
		if session.Socket == "" {
			session, _ = readWindowsSession(sessionPath)
		}
		if session.Socket != "" {
			_, _ = session.request("host.shutdown", nil)
		}
	}()
	defer func() {
		if t.Failed() {
			body, _ := os.ReadFile(filepath.Join(work, "host.log"))
			t.Logf("updater log: %s", body)
		}
	}()
	done := make(chan error, 1)
	go func() { done <- old.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Minute):
		t.Fatal("legacy updater did not exit")
	}
	waitWindows(t, 90*time.Second, func() bool {
		s, err := readWindowsSession(sessionPath)
		if err != nil {
			return false
		}
		body, err := s.request("host.ping", nil)
		if err == nil && bytes.Contains(body, []byte(`"0.0.1"`)) {
			session = s
			return true
		}
		return false
	})
	assertWindowsData(t, session, home, seed)
	verifyWindowsHostUI(t, session, repo)
	t.Log("Old mygo updater installed the actual NSH host; private old-data snapshot and original credentials retained")
	hostBefore, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	beforeHash := sha256.Sum256(hostBefore)
	offer.Store(2)
	corrupt.Store(true)
	windowsMust(t, session, "desktop.update.check", nil)
	waitWindowsPhase(t, session, "available")
	windowsMust(t, session, "desktop.update.install", nil)
	waitWindows(t, 45*time.Second, func() bool {
		body, err := session.request("desktop.snapshot", map[string]any{"page": "settings"})
		var s struct{ Update struct{ Phase, Error string } }
		_ = json.Unmarshal(body, &s)
		return err == nil && s.Update.Phase == "available" && s.Update.Error != ""
	})
	unchanged, err := os.ReadFile(binary)
	if err != nil || sha256.Sum256(unchanged) != beforeHash {
		t.Fatal("bad signature changed installed frontend", err)
	}
	corrupt.Store(false)
	windowsMust(t, session, "desktop.update.check", nil)
	waitWindowsPhase(t, session, "available")
	windowsMust(t, session, "desktop.update.install", nil)
	waitWindowsPhase(t, session, "ready")
	previousPID := session.BackendPID
	windowsMust(t, session, "desktop.update.restart", nil)
	waitWindows(t, 90*time.Second, func() bool {
		s, err := readWindowsSession(sessionPath)
		if err != nil || s.BackendPID == previousPID {
			return false
		}
		body, err := s.request("host.ping", nil)
		if err == nil && bytes.Contains(body, []byte(`"0.0.2"`)) {
			session = s
			return true
		}
		return false
	})
	assertWindowsData(t, session, home, seed)
	waitWindows(t, 20*time.Second, func() bool { return windowsHostVisible(session.FrontendPID) })
	output, err := exec.Command(binary, "--version").CombinedOutput()
	if err != nil || strings.TrimSpace(string(output)) != "Gopurr 0.0.2" {
		t.Fatalf("new Rust frontend was not installed: %s %v", output, err)
	}
	windowsMust(t, session, "host.shutdown", nil)
	waitWindows(t, 20*time.Second, func() bool { _, err := session.request("host.ping", nil); return err != nil })
	session = windowsSession{}
	t.Log("NSH -> NSH passed: invalid signature refused, mapped EXE/DLL replaced, new Rust frontend relaunched and old data retained")
}

type windowsSession struct {
	Socket, TokenFile string
	BackendPID        int
	token             string
	FrontendPID       uint32
}

func readWindowsSession(path string) (windowsSession, error) {
	var s windowsSession
	body, err := os.ReadFile(path)
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(body, &s); err != nil {
		return s, err
	}
	s.token, err = nativehost.ReadToken(s.TokenFile)
	return s, err
}
func (s windowsSession) request(method string, params any) (json.RawMessage, error) {
	if params == nil {
		params = map[string]any{}
	}
	body, _ := json.Marshal(params)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	reply, err := nativehost.Call(ctx, s.Socket, s.token, method, body)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Result json.RawMessage
		Error  *nativehost.Error
	}
	if err = json.Unmarshal(reply, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("%s: %s", method, envelope.Error.Message)
	}
	return envelope.Result, nil
}
func windowsMust(t *testing.T, s windowsSession, method string, params any) json.RawMessage {
	t.Helper()
	body, err := s.request(method, params)
	if err != nil {
		t.Fatal(err)
	}
	return body
}
func waitWindows(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("Windows NSH acceptance timed out")
}
func waitWindowsPhase(t *testing.T, s windowsSession, phase string) {
	t.Helper()
	waitWindows(t, 90*time.Second, func() bool {
		body, err := s.request("desktop.snapshot", map[string]any{"page": "settings"})
		var v struct{ Update struct{ Phase string } }
		_ = json.Unmarshal(body, &v)
		return err == nil && v.Update.Phase == phase
	})
}
func assertWindowsData(t *testing.T, s windowsSession, home string, seed map[string]string) {
	t.Helper()
	body := windowsMust(t, s, "desktop.snapshot", map[string]any{"page": "subs"})
	var v struct {
		ActiveProfile string
		Profiles      []struct{ ID, Name string }
		Prefs         struct{ Appearance, Language string }
		Status        struct{ SystemProxy, Tun bool }
	}
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatal(err)
	}
	if v.ActiveProfile != "legacy" || len(v.Profiles) != 1 || v.Profiles[0].Name != "Retained subscription" || v.Prefs.Appearance != "dark" || v.Prefs.Language != "en" || v.Status.SystemProxy || v.Status.Tun {
		t.Fatalf("legacy model changed: %s", body)
	}
	for name, value := range seed {
		snapshot, err := os.ReadFile(filepath.Join(home, nativehost.DesktopMigration, name))
		if err != nil || string(snapshot) != value {
			t.Fatalf("initial data snapshot changed: %s (%v)", name, err)
		}
	}
	for _, name := range []string{"profiles.json", "profiles/legacy.yaml", "overrides.json"} {
		body, err := os.ReadFile(filepath.Join(home, name))
		if err != nil || string(body) != seed[name] {
			t.Fatalf("durable data changed: %s (%v)", name, err)
		}
	}
}
func fixtureCommand(t *testing.T, dir string, env []string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	if body, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", name, err, body)
	}
}
func copyFixtureTree(t *testing.T, source, dest string) {
	t.Helper()
	err := filepath.WalkDir(source, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("nonregular fixture %s", path)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, 0755)
	})
	if err != nil {
		t.Fatal(err)
	}
}

const windowsLegacyFixture = `package main
import("context";"fmt";"os";"time";"github.com/egoist/mygo";"gopurr/internal/update")
func main(){
 mygo.App.SetName("Gopurr");mygo.App.SetVersion("0.0.0");mygo.App.OnWindowAllClosed(func(){})
 mygo.App.WhenReady(func(){go func(){ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second);defer cancel();release,err:= (update.Native{}).Check(ctx);if err!=nil||release==nil{fmt.Fprintln(os.Stderr,"check",err);os.Exit(1)};if err=release.Install(ctx,nil);err!=nil{fmt.Fprintln(os.Stderr,"install",err);os.Exit(1)};mygo.RunOnMain(mygo.App.Relaunch)}()})
 if err:=mygo.App.Run();err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}
}`
