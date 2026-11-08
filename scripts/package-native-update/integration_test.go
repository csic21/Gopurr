//go:build darwin

package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"debug/buildinfo"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Opt in with GOPURR_NATIVE_PACKAGE_TEST_APP pointing at a built NSH bundle.
// Nothing is launched or installed. A disposable copy receives a newly built
// helper and an ephemeral update key; the original bundle is only read.
func TestRealNativeBundlePackaging(t *testing.T) {
	source := os.Getenv("GOPURR_NATIVE_PACKAGE_TEST_APP")
	if source == "" {
		t.Skip("set GOPURR_NATIVE_PACKAGE_TEST_APP to validate a disposable real native bundle")
	}
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("native desktop distribution is darwin/arm64")
	}
	source, err := filepath.Abs(source)
	if err != nil {
		t.Fatal(err)
	}
	before := fingerprint(t, source)
	_, file, _, _ := runtime.Caller(0)
	repo := filepath.Dir(filepath.Dir(filepath.Dir(file)))
	c, err := readConfig(filepath.Join(repo, "mygo.json"))
	if err != nil {
		t.Fatal(err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c.Updates.PublicKey = base64.StdEncoding.EncodeToString(pub)
	dir := t.TempDir()
	app := filepath.Join(dir, filepath.Base(source))
	if err = exec.Command("/bin/cp", "-R", source, app).Run(); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(app, "Contents/Resources/gopurr-desktop")
	cmd := exec.Command("go", "build", "-ldflags", linkFlags(c, "io.github.gopurr.native"), "-o", helper, ".")
	cmd.Dir = repo
	if output, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("build disposable release helper: %v\n%s", e, output)
	}
	marker := nativeBuild{Schema: 1, UI: "nsh-desktop", Backend: "desktop-v2", Version: c.Version, UpdateFeed: feed(c), UpdatePublicKey: c.Updates.PublicKey, UpdateEnabled: true}
	b, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(app, "Contents/Resources/native-build.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{helper, app} {
		if output, e := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", path).CombinedOutput(); e != nil {
			t.Fatalf("sign disposable fixture: %v\n%s", e, output)
		}
	}
	b, err = json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(dir, "mygo.json")
	if err = os.WriteFile(cfg, b, 0o600); err != nil {
		t.Fatal(err)
	}
	err = run([]string{"-config", cfg, "-app", app, "-out", filepath.Join(dir, "artifacts"), "-sign", "-key-env", "EPHEMERAL_TEST_ONLY"}, func(name string) string {
		if name != "EPHEMERAL_TEST_ONLY" {
			t.Fatal("unexpected key lookup")
		}
		return base64.StdEncoding.EncodeToString(key)
	}, io.Discard, inspectors{buildinfo.ReadFile, verifyCodeSign})
	if err != nil {
		t.Fatal(err)
	}
	if after := fingerprint(t, source); before != after {
		t.Fatal("original application bundle changed")
	}
	var m manifest
	if err = readJSON(filepath.Join(dir, "artifacts", manifestName), &m); err != nil {
		t.Fatal(err)
	}
	if m.Size == 0 {
		t.Fatal("empty native release archive")
	}
	t.Logf("Complete disposable native archive signed and verified: %d bytes; original application unchanged", m.Size)
}
