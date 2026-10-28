//go:build darwin

package service

import (
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Execute the actual installer with only privileged locations redirected and
// ownership/launchd calls stubbed. All filesystem mutations stay in t.TempDir.
func TestInstallerUsesProtectedCopyAndEscapesPaths(t *testing.T) {
	dir := t.TempDir()
	helperParent, plistDir := filepath.Join(dir, "Helpers"), filepath.Join(dir, "Daemons")
	if err := os.Mkdir(plistDir, 0700); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(dir, `user & <config> "quoted" 'apostrophe'`)
	if err := os.Mkdir(dataDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "runtime.yaml"), []byte("rules: [MATCH,DIRECT]"), 0600); err != nil {
		t.Fatal(err)
	}
	// The kernel would otherwise download this from GitHub on first start,
	// which fails exactly when the proxy is the thing being repaired.
	if err := os.Mkdir(filepath.Join(dataDir, "mihomo"), 0700); err != nil {
		t.Fatal(err)
	}
	geodata := []byte("fake geoip database")
	if err := os.WriteFile(filepath.Join(dataDir, "mihomo", "geoip.metadb"), geodata, 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "User App")
	if err := os.WriteFile(binary, []byte("original application"), 0700); err != nil {
		t.Fatal(err)
	}
	fixture := strings.ReplaceAll(installScript, "/Library/LaunchDaemons", plistDir)
	fixture = strings.ReplaceAll(fixture, "SERVICE_PARENT=/Library/PrivilegedHelperTools", "SERVICE_PARENT="+shellQuote(helperParent))
	preamble := "stat() { printf '0\\n'; }; chown() { :; }; launchctl() { :; };\n"
	run := func(action string) ([]byte, error) {
		return exec.Command("/bin/sh", "-c", preamble+fixture, "installer-test", action, binary, dataDir).CombinedOutput()
	}
	if out, err := run("install"); err != nil {
		t.Fatalf("install: %v %s", err, out)
	}
	target := filepath.Join(helperParent, Label, "Gopurr-core")
	if err := os.WriteFile(binary, []byte("replaced user application"), 0700); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != "original application" {
		t.Fatalf("service still depends on user application: %s %v", b, err)
	}
	b, err := os.ReadFile(filepath.Join(plistDir, Label+".plist"))
	if err != nil {
		t.Fatal(err)
	}
	decoder := xml.NewDecoder(strings.NewReader(string(b)))
	var values []string
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("invalid plist XML: %v\n%s", err, b)
		}
		if element, ok := token.(xml.StartElement); ok && element.Name.Local == "string" {
			var value string
			if err := decoder.DecodeElement(&value, &element); err != nil {
				t.Fatal(err)
			}
			values = append(values, value)
		}
	}
	has := func(want string) bool {
		for _, value := range values {
			if value == want {
				return true
			}
		}
		return false
	}
	if !has(target) || has(binary) || !has(filepath.Join(dataDir, "runtime.yaml")) || !has(filepath.Join(dataDir, "core.secret")) {
		t.Fatalf("incorrect service paths: %q", values)
	}
	if !has(filepath.Join(helperParent, Label, "core.log")) || !has(filepath.Join(helperParent, Label, "mihomo")) {
		t.Fatal("privileged logs/cache still use the user directory")
	}
	if info, _ := os.Stat(filepath.Join(helperParent, Label)); info.Mode().Perm() != 0700 {
		t.Fatal("service directory not protected")
	}
	if b, err := os.ReadFile(filepath.Join(helperParent, Label, "mihomo", "geoip.metadb")); err != nil || string(b) != string(geodata) {
		t.Fatalf("geodata not seeded for the service: %v", err)
	}
	if !strings.Contains(string(b), "<key>KeepAlive</key><true/>") {
		t.Fatal("daemon is not supervised after a crash")
	}
	if out, err := run("uninstall"); err != nil {
		t.Fatalf("uninstall: %v %s", err, out)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("uninstall left the protected executable")
	}
	if _, err := os.Stat(filepath.Join(dataDir, "runtime.yaml")); err != nil {
		t.Fatal("uninstall removed user configuration")
	}
}

func TestInstallerRejectsLinkedServiceDirectory(t *testing.T) {
	dir := t.TempDir()
	helperParent := filepath.Join(dir, "Helpers")
	if err := os.Symlink(t.TempDir(), helperParent); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data")
	os.Mkdir(data, 0700)
	os.WriteFile(filepath.Join(data, "runtime.yaml"), []byte("mode: rule"), 0600)
	fixture := strings.ReplaceAll(installScript, "SERVICE_PARENT=/Library/PrivilegedHelperTools", "SERVICE_PARENT="+shellQuote(helperParent))
	out, err := exec.Command("/bin/sh", "-c", fixture, "installer-test", "install", "/bin/sh", data).CombinedOutput()
	if err == nil || !strings.Contains(string(out), "服务目录不能是链接") {
		t.Fatalf("linked service path accepted: %v %s", err, out)
	}
}
