//go:build darwin

package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"debug/buildinfo"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gopurr/internal/nativehost"
)

// This opt-in test launches only disposable bundles and uses generated keys,
// loopback HTTP, a synthetic legacy home and the actual Swift/NSH frontend.
// It covers both updater locations (old MacOS executable and NSH Resources
// helper), the installer/relaunch handshake and retained model data.
func TestNativeDesktopMigrationRoundtrip(t *testing.T) {
	source := os.Getenv("GOPURR_NATIVE_PACKAGE_TEST_APP")
	if source == "" || runtime.GOARCH != "arm64" {
		t.Skip("set GOPURR_NATIVE_PACKAGE_TEST_APP on macOS arm64")
	}
	source, _ = filepath.Abs(source)
	sourcePlist, err := readPlist(filepath.Join(source, "Contents/Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	sourceExecutable := sourcePlist["CFBundleExecutable"]
	before := fingerprint(t, source)
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	work, err := os.MkdirTemp(filepath.Join(repo, ".cache"), "native-roundtrip-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(work)
	home := filepath.Join(work, "home")
	os.MkdirAll(filepath.Join(home, "profiles"), 0700)
	seed := map[string]string{
		"prefs.json":           `{"appearance":"dark","language":"en","systemProxyStartup":"off","restoreSystemProxy":false,"checkAppUpdates":false,"autoUpdateHours":0}`,
		"local.yaml":           "mode: rule\nrules: [MATCH,DIRECT]\n",
		"profiles.json":        `{"active":"legacy","profiles":[{"id":"legacy","name":"Retained subscription","kind":"url","url":"https://example.invalid/sub?token=fixture-secret","file":"legacy.yaml"}]}`,
		"profiles/legacy.yaml": "mode: rule\nproxies: [{name: retained-node, type: http, server: 127.0.0.1, port: 9, password: fixture-secret}]\nproxy-groups: [{name: Route, type: select, proxies: [DIRECT, REJECT]}]\nrules: [MATCH,Route]\n",
		"overrides.json":       `{"mixedPort":17889,"mode":"rule","tunEnable":false}`,
	}
	for name, body := range seed {
		if err := os.WriteFile(filepath.Join(home, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	testBundleID := fmt.Sprintf("io.github.gopurr.roundtrip.x%d", time.Now().UnixNano())
	var offer atomic.Int32
	var corrupt atomic.Bool
	artifacts := filepath.Join(work, "artifacts")
	os.MkdirAll(artifacts, 0700)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := filepath.Base(r.URL.Path)
		if strings.HasPrefix(path, "update-") {
			version := "0.0.1"
			if path == manifestName && offer.Load() == 2 {
				version = "0.0.2"
			}
			b, e := os.ReadFile(filepath.Join(artifacts, version, manifestName))
			if e != nil {
				http.Error(w, e.Error(), 500)
				return
			}
			if corrupt.Load() {
				var m manifest
				_ = json.Unmarshal(b, &m)
				m.Signature = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
				b, _ = json.Marshal(m)
			}
			_, _ = w.Write(b)
			return
		}
		for _, v := range []string{"0.0.1", "0.0.2"} {
			p := filepath.Join(artifacts, v, path)
			if _, e := os.Stat(p); e == nil {
				http.ServeFile(w, r, p)
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	for _, version := range []string{"0.0.1", "0.0.2"} {
		app := filepath.Join(work, "build-"+version, "Gopurr.app")
		os.MkdirAll(filepath.Dir(app), 0700)
		runFixtureCommand(t, repo, "/bin/cp", "-R", source, app)
		plist := filepath.Join(app, "Contents/Info.plist")
		for field, value := range map[string]string{"CFBundleIdentifier": testBundleID, "CFBundleName": "Gopurr", "CFBundleDisplayName": "Gopurr", "CFBundleExecutable": "Gopurr", "CFBundleShortVersionString": version, "GopurrDataDirectory": "Gopurr", "LSMinimumSystemVersion": "12.0"} {
			runFixtureCommand(t, repo, "/usr/libexec/PlistBuddy", "-c", "Set :"+field+" "+value, plist)
		}
		if err := os.Rename(filepath.Join(app, "Contents/MacOS", sourceExecutable), filepath.Join(app, "Contents/MacOS/Gopurr")); err != nil {
			t.Fatal(err)
		}
		c := config{Name: "Gopurr", Slug: "gopurr", Version: version}
		// Only the in-memory test config permits loopback HTTP. The production
		// config reader and release builder continue to require HTTPS.
		c.Updates.URL = server.URL
		c.Updates.PublicKey = base64.StdEncoding.EncodeToString(pub)
		helper := filepath.Join(app, "Contents/Resources/gopurr-desktop")
		runFixtureCommand(t, repo, "go", "build", "-ldflags", linkFlags(c, testBundleID), "-o", helper, ".")
		marker := nativeBuild{Schema: 1, UI: "nsh-desktop", Backend: "desktop-v2", Version: version, UpdateFeed: feed(c), UpdatePublicKey: c.Updates.PublicKey, UpdateEnabled: true, DataDirectory: "Gopurr"}
		b, _ := json.Marshal(marker)
		os.WriteFile(filepath.Join(app, "Contents/Resources/native-build.json"), b, 0644)
		runFixtureCommand(t, repo, "/usr/bin/codesign", "--force", "--sign", "-", helper)
		runFixtureCommand(t, repo, "/usr/bin/codesign", "--force", "--sign", "-", app)
		o := options{app: app, out: filepath.Join(artifacts, version), bundleID: testBundleID, nshID: "io.github.gopurr.nsh.preview", production: true}
		if err := validateBundle(o, c, inspectors{buildinfo.ReadFile, verifyCodeSign}); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := packageBundle(o, c, key); err != nil {
			t.Fatal(err)
		}
	}
	installed := filepath.Join(work, "installed", "Gopurr.app")
	binary := filepath.Join(installed, "Contents/MacOS/Gopurr")
	os.MkdirAll(filepath.Dir(binary), 0700)
	fixtureSource := filepath.Join(work, "legacy.go")
	os.WriteFile(fixtureSource, []byte(legacyRoundtripFixture), 0600)
	flags := "-X github.com/egoist/mygo.production=1 -X github.com/egoist/mygo.packageUpdateFeed=" + server.URL + "/update-darwin-arm64.json -X github.com/egoist/mygo.packageUpdateKey=" + base64.StdEncoding.EncodeToString(pub)
	runFixtureCommand(t, repo, "go", "build", "-ldflags", flags, "-o", binary, fixtureSource)
	// A real app bundle is required by LaunchServices and the old installer.
	os.WriteFile(filepath.Join(installed, "Contents/Info.plist"), []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>`+testBundleID+`</string><key>CFBundleExecutable</key><string>Gopurr</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleShortVersionString</key><string>0.0.0</string></dict></plist>`), 0644)
	runFixtureCommand(t, repo, "/usr/bin/codesign", "--force", "--sign", "-", installed)
	logPath := filepath.Join(work, "host.log")
	logFile, _ := os.Create(logPath)
	defer logFile.Close()
	old := exec.Command(binary, "--gopurr-home", home)
	old.Stdout, old.Stderr = logFile, logFile
	if err := old.Start(); err != nil {
		t.Fatal(err)
	}
	defer old.Process.Kill()
	defer func() {
		if t.Failed() {
			b, _ := os.ReadFile(logPath)
			t.Logf("legacy updater output: %s", b)
			b, _ = exec.Command(binary, "--version").CombinedOutput()
			t.Logf("installed host after failure: %s", b)
			if s, e := findFixtureSession(home); e == nil {
				b, e := fixtureCall(s, "host.ping", nil)
				t.Logf("remaining fixture backend: %s (%v)", b, e)
			}
		}
	}()
	if err := old.Wait(); err != nil {
		t.Fatal("legacy install/relaunch", err)
	}
	var session fixtureSession
	defer func() {
		if session.socket == "" {
			session, _ = findFixtureSession(home)
		}
		if session.socket != "" {
			_, _ = fixtureCall(session, "host.shutdown", nil)
		}
	}()
	eventuallyFixture(t, 90*time.Second, func() bool {
		s, e := findFixtureSession(home)
		if e != nil {
			return false
		}
		b, e := fixtureCall(s, "host.ping", nil)
		if e == nil && bytes.Contains(b, []byte(`"0.0.1"`)) {
			session = s
			return true
		}
		return false
	})
	assertFixtureData(t, session, home, seed)
	t.Log("legacy updater installed and relaunched the actual NSH host; subscriptions, credentials, preferences and snapshot retained")
	// Bad signatures must not stage an update, stop the model or change the app.
	offer.Store(2)
	corrupt.Store(true)
	fixtureMustCall(t, session, "desktop.update.check", nil)
	waitFixturePhase(t, session, "available")
	fixtureMustCall(t, session, "desktop.update.install", nil)
	eventuallyFixture(t, 30*time.Second, func() bool {
		b, e := fixtureCall(session, "desktop.snapshot", map[string]any{"page": "settings"})
		var s struct{ Update struct{ Phase, Error string } }
		_ = json.Unmarshal(b, &s)
		return e == nil && s.Update.Phase == "available" && s.Update.Error != ""
	})
	out, err := exec.Command(binary, "--version").CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != "Gopurr 0.0.1" {
		t.Fatal("bad signature changed installed host", err, string(out))
	}
	corrupt.Store(false)
	fixtureMustCall(t, session, "desktop.update.check", nil)
	waitFixturePhase(t, session, "available")
	fixtureMustCall(t, session, "desktop.update.install", nil)
	waitFixturePhase(t, session, "ready")
	oldHost := session.hostPID
	fixtureMustCall(t, session, "desktop.update.restart", nil)
	eventuallyFixture(t, 90*time.Second, func() bool {
		s, e := findFixtureSession(home)
		if e != nil || s.hostPID == oldHost {
			return false
		}
		b, e := fixtureCall(s, "host.ping", nil)
		if e == nil && bytes.Contains(b, []byte(`"0.0.2"`)) {
			session = s
			return true
		}
		return false
	})
	assertFixtureData(t, session, home, seed)
	runFixtureCommand(t, repo, "/usr/bin/codesign", "--verify", "--deep", "--strict", installed)
	fixtureMustCall(t, session, "host.shutdown", nil)
	eventuallyFixture(t, 20*time.Second, func() bool { _, e := findFixtureSession(home); return e != nil })
	session = fixtureSession{}
	if after := fingerprint(t, source); after != before {
		t.Fatal("source app changed")
	}
	t.Log("NSH 0.0.1 -> 0.0.2 verified: bad signature rejected, whole bundle replaced, new host relaunched, original source unchanged")
}

type fixtureSession struct {
	socket, token string
	hostPID       int
}

func findFixtureSession(home string) (fixtureSession, error) {
	b, e := exec.Command("/bin/ps", "-axo", "ppid=,args=").Output()
	if e != nil {
		return fixtureSession{}, e
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		args := map[string]string{}
		for i := 1; i+1 < len(fields); i++ {
			if strings.HasPrefix(fields[i], "--") {
				args[fields[i]] = fields[i+1]
			}
		}
		if args["--home"] != home || args["--socket"] == "" {
			continue
		}
		token, e := nativehost.ReadToken(args["--token-file"])
		if e != nil {
			continue
		}
		pid, _ := strconv.Atoi(fields[0])
		return fixtureSession{args["--socket"], token, pid}, nil
	}
	return fixtureSession{}, fmt.Errorf("no fixture helper")
}
func fixtureCall(s fixtureSession, method string, params any) (json.RawMessage, error) {
	if params == nil {
		params = map[string]any{}
	}
	b, _ := json.Marshal(params)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	response, err := nativehost.Call(ctx, s.socket, s.token, method, b)
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Result json.RawMessage
		Error  *nativehost.Error
	}
	if err = json.Unmarshal(response, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, fmt.Errorf("%s: %s", method, envelope.Error.Message)
	}
	return envelope.Result, nil
}
func fixtureMustCall(t *testing.T, s fixtureSession, method string, params any) json.RawMessage {
	t.Helper()
	b, e := fixtureCall(s, method, params)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func eventuallyFixture(t *testing.T, timeout time.Duration, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatal("native roundtrip timed out")
}
func waitFixturePhase(t *testing.T, s fixtureSession, phase string) {
	t.Helper()
	eventuallyFixture(t, 90*time.Second, func() bool {
		b, e := fixtureCall(s, "desktop.snapshot", map[string]any{"page": "settings"})
		var v struct{ Update struct{ Phase string } }
		_ = json.Unmarshal(b, &v)
		return e == nil && v.Update.Phase == phase
	})
}
func assertFixtureData(t *testing.T, s fixtureSession, home string, seed map[string]string) {
	t.Helper()
	b := fixtureMustCall(t, s, "desktop.snapshot", map[string]any{"page": "subs"})
	var v struct {
		ActiveProfile string
		Profiles      []struct{ ID, Name string }
		Prefs         struct{ Appearance, Language string }
		Status        struct{ SystemProxy, Tun bool }
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if v.ActiveProfile != "legacy" || len(v.Profiles) != 1 || v.Profiles[0].Name != "Retained subscription" || v.Prefs.Appearance != "dark" || v.Prefs.Language != "en" || v.Status.SystemProxy || v.Status.Tun {
		t.Fatalf("legacy model not retained: %s", b)
	}
	for name, body := range seed {
		backup, e := os.ReadFile(filepath.Join(home, nativehost.DesktopMigration, name))
		if e != nil || string(backup) != body {
			t.Fatalf("initial snapshot changed: %s (%v)", name, e)
		}
	}
	for _, name := range []string{"profiles.json", "profiles/legacy.yaml", "overrides.json"} {
		b, e := os.ReadFile(filepath.Join(home, name))
		if e != nil || string(b) != seed[name] {
			t.Fatalf("durable state changed: %s (%v)", name, e)
		}
	}
}
func runFixtureCommand(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if b, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("%s failed: %v\n%s", name, e, b)
	}
}

const legacyRoundtripFixture = `package main
import("context";"fmt";"os";"time";"github.com/egoist/mygo";"gopurr/internal/update")
func main(){
 mygo.App.SetName("Gopurr");mygo.App.SetVersion("0.0.0");mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory);mygo.App.OnWindowAllClosed(func(){})
 mygo.App.WhenReady(func(){go func(){ctx,cancel:=context.WithTimeout(context.Background(),90*time.Second);defer cancel();backend:=update.Native{};release,err:=backend.Check(ctx);if err!=nil||release==nil{fmt.Fprintln(os.Stderr,"check",err);os.Exit(1)};if err:=release.Install(ctx,nil);err!=nil{fmt.Fprintln(os.Stderr,"install",err);os.Exit(1)};mygo.RunOnMain(mygo.App.Relaunch)}()})
 if err:=mygo.App.Run();err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}
}`
