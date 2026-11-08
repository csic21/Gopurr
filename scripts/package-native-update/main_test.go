//go:build !windows

// These macOS bundle fixtures require POSIX executable permissions.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

type testRelease struct {
	options options
	config  config
	key     ed25519.PrivateKey
	inspect inspectors
}

func fixture(t *testing.T) testRelease {
	t.Helper()
	root := t.TempDir()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	c := config{Name: "Gopurr", Slug: "gopurr", Version: "1.2.3"}
	c.Updates.PublicKey = base64.StdEncoding.EncodeToString(pub)
	c.Updates.GitHub = "csic21/Gopurr"
	c.Updates.TagPrefix = "v"
	o := options{app: filepath.Join(root, "Gopurr Native.app"), out: filepath.Join(root, "dist"), configPath: filepath.Join(root, "mygo.json"), bundleID: "io.github.gopurr.native", nshID: "io.github.gopurr.nsh.preview"}
	write := func(name string, b []byte, mode os.FileMode) {
		t.Helper()
		path := filepath.Join(o.app, name)
		if e := os.MkdirAll(filepath.Dir(path), 0o755); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, b, mode); e != nil {
			t.Fatal(e)
		}
	}
	write("Contents/Info.plist", []byte(`<?xml version="1.0"?><plist><dict><key>CFBundleIdentifier</key><string>io.github.gopurr.native</string><key>CFBundleExecutable</key><string>Gopurr Native</string><key>CFBundleShortVersionString</key><string>1.2.3</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleURLTypes</key><array><dict><key>nested</key><string>ignored</string></dict></array></dict></plist>`), 0o644)
	write("Contents/MacOS/Gopurr Native", machO(6), 0o755)
	write("Contents/Resources/gopurr-desktop", machO(1), 0o755)
	write("Contents/Resources/nsh/app.json", []byte(`{"id":"io.github.gopurr.nsh.preview"}`), 0o644)
	write("Contents/Resources/nsh/capabilities.json", []byte(`{"host":["desktop.dispatch"]}`), 0o644)
	write("Contents/Resources/nsh/bundle.js", []byte(`"desktop-nav-overview";"desktop.dispatch";`), 0o644)
	write("Contents/Resources/nsh/bundle.nshc", []byte("temporary native bytecode fixture"), 0o644)
	marker, err := json.Marshal(nativeBuild{Schema: 1, UI: "nsh-desktop", Backend: "desktop-v2", Version: c.Version, UpdateFeed: feed(c), UpdatePublicKey: c.Updates.PublicKey, UpdateEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	write("Contents/Resources/native-build.json", marker, 0o644)
	write("Contents/Resources/TrayTemplate.png", []byte("tray fixture"), 0o644)
	write("Contents/Resources/LICENSE", []byte("license"), 0o644)
	write("Contents/Resources/THIRD_PARTY_NOTICES.md", []byte("third party"), 0o644)
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(o.configPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return testRelease{o, c, key, inspectors{func(string) (*debug.BuildInfo, error) { return helperInfo(c, o.bundleID), nil }, func(string) error { return nil }}}
}
func machO(platform uint32) []byte {
	// A minimal arm64 executable header with LC_BUILD_VERSION. These fixtures
	// are never run, signed, installed, or mistaken for production binaries.
	values := []uint32{0xfeedfacf, 0x0100000c, 0, 2, 1, 24, 0, 0, 0x32, 24, platform, 0x0f0000, 0x0f0000, 0}
	var b bytes.Buffer
	for _, v := range values {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	return b.Bytes()
}
func helperInfo(c config, id string) *debug.BuildInfo {
	return &debug.BuildInfo{Path: "gopurr", Settings: []debug.BuildSetting{{Key: "GOOS", Value: "darwin"}, {Key: "GOARCH", Value: "arm64"}, {Key: "-ldflags", Value: linkFlags(c, id)}}}
}
func args(f testRelease, more ...string) []string {
	return append([]string{"-config", f.options.configPath, "-app", f.options.app, "-out", f.options.out}, more...)
}

func TestDefaultValidatesWithoutReadingAnyKeyOrWritingArtifacts(t *testing.T) {
	f := fixture(t)
	var output bytes.Buffer
	reads := 0
	if err := run(args(f), func(string) string { reads++; t.Error("validation must not read environment keys"); return "" }, &output, f.inspect); err != nil {
		t.Fatal(err)
	}
	if reads != 0 {
		t.Fatal(reads)
	}
	if _, err := os.Stat(f.options.out); !os.IsNotExist(err) {
		t.Fatal("validation created output")
	}
	if !strings.Contains(output.String(), "no artifacts written") {
		t.Fatal(output.String())
	}
}

func TestProductionPackagePreservesDesktopTrustAndFeeds(t *testing.T) {
	f := fixture(t)
	plist := filepath.Join(f.options.app, "Contents/Info.plist")
	b, err := os.ReadFile(plist)
	if err != nil {
		t.Fatal(err)
	}
	b = bytes.ReplaceAll(b, []byte("io.github.gopurr.native"), []byte("io.github.gopurr.app"))
	b = bytes.ReplaceAll(b, []byte("<string>Gopurr Native</string>"), []byte("<string>Gopurr</string>"))
	b = bytes.Replace(b, []byte("</dict></plist>"), []byte("<key>GopurrDataDirectory</key><string>Gopurr</string><key>LSMinimumSystemVersion</key><string>12.0</string></dict></plist>"), 1)
	os.WriteFile(plist, b, 0644)
	os.Rename(filepath.Join(f.options.app, "Contents/MacOS/Gopurr Native"), filepath.Join(f.options.app, "Contents/MacOS/Gopurr"))
	var marker nativeBuild
	markerPath := filepath.Join(f.options.app, "Contents/Resources/native-build.json")
	if err := readJSON(markerPath, &marker); err != nil {
		t.Fatal(err)
	}
	marker.DataDirectory = "Gopurr"
	b, _ = json.Marshal(marker)
	os.WriteFile(markerPath, b, 0644)
	f.inspect.buildInfo = func(string) (*debug.BuildInfo, error) { return helperInfo(f.config, "io.github.gopurr.app"), nil }
	if err := run(args(f, "-production", "-sign", "-key-env", "TEST_KEY"), func(string) string { return base64.StdEncoding.EncodeToString(f.key) }, io.Discard, f.inspect); err != nil {
		t.Fatal(err)
	}
	legacy, err := os.ReadFile(filepath.Join(f.options.out, "update-darwin-arm64.json"))
	if err != nil {
		t.Fatal(err)
	}
	native, err := os.ReadFile(filepath.Join(f.options.out, manifestName))
	if err != nil || !bytes.Equal(legacy, native) {
		t.Fatal("migration feeds disagree", err)
	}
	var m manifest
	json.Unmarshal(legacy, &m)
	if !strings.HasSuffix(m.URL, "/gopurr-1.2.3-darwin-arm64.tar.gz") {
		t.Fatal("legacy artifact URL changed", m.URL)
	}
	marker.DataDirectory = "Gopurr Native"
	b, _ = json.Marshal(marker)
	os.WriteFile(markerPath, b, 0644)
	if err := run(args(f, "-production"), func(string) string { t.Fatal("validation read private key"); return "" }, io.Discard, f.inspect); err == nil {
		t.Fatal("production accepted an isolated preview home")
	}
}
func TestSignsCompleteBundleWithExistingTrustAndDoesNotChangeInput(t *testing.T) {
	f := fixture(t)
	if err := os.Symlink("LICENSE", filepath.Join(f.options.app, "Contents/Resources/license-alias")); err != nil {
		t.Fatal(err)
	}
	before := fingerprint(t, f.options.app)
	var output bytes.Buffer
	if err := run(args(f, "-sign", "-key-env", "TEST_TEMPORARY_SIGNING_KEY"), func(name string) string {
		if name != "TEST_TEMPORARY_SIGNING_KEY" {
			t.Fatal(name)
		}
		return base64.StdEncoding.EncodeToString(f.key)
	}, &output, f.inspect); err != nil {
		t.Fatal(err)
	}
	if after := fingerprint(t, f.options.app); before != after {
		t.Fatal("packaging modified the input bundle")
	}
	var m manifest
	if err := readJSON(filepath.Join(f.options.out, manifestName), &m); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(f.options.out, "gopurr-native-1.2.3-darwin-arm64.tar.gz")
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "1.2.3" || m.Size != int64(len(data)) || !ed25519.Verify(f.key.Public().(ed25519.PublicKey), hash[:], sig) {
		t.Fatal("manifest does not authenticate the exact archive")
	}
	if m.URL != "https://github.com/csic21/Gopurr/releases/download/v1.2.3/gopurr-native-1.2.3-darwin-arm64.tar.gz" {
		t.Fatal(m.URL)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	seen := map[string]*tar.Header{}
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		if !strings.HasPrefix(h.Name, "Gopurr Native.app/") {
			t.Fatalf("unexpected archive root %q", h.Name)
		}
		copy := *h
		seen[h.Name] = &copy
		if h.Uid != 0 || h.Gid != 0 {
			t.Fatal("nonportable ownership")
		}
	}
	for _, name := range []string{"Contents/Resources/TrayTemplate.png", "Contents/MacOS/Gopurr Native", "Contents/Resources/gopurr-desktop", "Contents/Resources/nsh/bundle.nshc", "Contents/Resources/nsh/capabilities.json", "Contents/Info.plist"} {
		if seen["Gopurr Native.app/"+name] == nil {
			t.Fatalf("incomplete update: %s", name)
		}
	}
	if h := seen["Gopurr Native.app/Contents/Resources/gopurr-desktop"]; h.Mode&0o111 == 0 {
		t.Fatal("helper executable permission lost")
	}
	if h := seen["Gopurr Native.app/Contents/Resources/license-alias"]; h.Typeflag != tar.TypeSymlink || h.Linkname != "LICENSE" {
		t.Fatal("safe relative symlink not preserved")
	}
	data[len(data)/2] ^= 1
	tampered := sha256.Sum256(data)
	if ed25519.Verify(f.key.Public().(ed25519.PublicKey), tampered[:], sig) {
		t.Fatal("tampered archive authenticated")
	}
}
func fingerprint(t *testing.T, app string) string {
	t.Helper()
	h := sha256.New()
	if e := walkBundle(app, func(path string, info os.FileInfo, link string) error {
		rel, _ := filepath.Rel(app, path)
		fmt.Fprintf(h, "%s:%s:%s\n", rel, info.Mode(), link)
		if info.Mode().IsRegular() {
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			h.Write(b)
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func TestSigningIsExplicitAndNeverFallsBackToAKeyFile(t *testing.T) {
	f := fixture(t)
	for _, more := range [][]string{{"-sign"}, {"-key-env", "TEST_KEY"}} {
		if e := run(args(f, more...), func(string) string { t.Fatal("key should not be read"); return "" }, io.Discard, f.inspect); e == nil {
			t.Fatal("implicit signing accepted")
		}
	}
	if e := run(args(f, "-sign", "-key-env", "TEST_EMPTY_KEY"), func(string) string { return "" }, io.Discard, f.inspect); e == nil || !strings.Contains(e.Error(), "explicit key environment") {
		t.Fatalf("missing key unexpectedly accepted: %v", e)
	}
	if _, err := os.Stat(f.options.out); !os.IsNotExist(err) {
		t.Fatal("failed signing wrote output")
	}
}
func TestRejectsAChangedPublicKeyBeforeCreatingOutput(t *testing.T) {
	f := fixture(t)
	_, wrong, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	err = run(args(f, "-sign", "-key-env", "TEST_WRONG_KEY"), func(string) string { return base64.StdEncoding.EncodeToString(wrong) }, io.Discard, f.inspect)
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatal(err)
	}
	if _, e := os.Stat(f.options.out); !os.IsNotExist(e) {
		t.Fatal("failed key match created output")
	}
}
func TestRejectsLegacyHostMobileUIAndMissingHelper(t *testing.T) {
	for _, scenario := range []string{"legacy", "mobile", "helper", "identity", "version"} {
		t.Run(scenario, func(t *testing.T) {
			f := fixture(t)
			switch scenario {
			case "legacy":
				os.WriteFile(filepath.Join(f.options.app, "Contents/MacOS/Gopurr Native"), machO(1), 0o755)
			case "mobile":
				os.WriteFile(filepath.Join(f.options.app, "Contents/Resources/nsh/capabilities.json"), []byte(`{"host":["engine.dispatch"]}`), 0o644)
			case "helper":
				os.Remove(filepath.Join(f.options.app, "Contents/Resources/gopurr-desktop"))
			case "identity":
				f.options.bundleID = "io.github.gopurr.app"
			case "version":
				f.config.Version = "2.0.0"
			}
			if e := validateBundle(f.options, f.config, f.inspect); e == nil {
				t.Fatal("invalid native update accepted")
			}
		})
	}
}
func TestRejectsHelperWithOldFeedWrongKeyOrDevelopmentMetadata(t *testing.T) {
	for _, scenario := range []string{"feed", "key", "version", "identifier", "development", "suffix", "production", "override"} {
		t.Run(scenario, func(t *testing.T) {
			f := fixture(t)
			bi := helperInfo(f.config, f.options.bundleID)
			flags := bi.Settings[2].Value
			switch scenario {
			case "feed":
				flags = strings.ReplaceAll(flags, manifestName, "update-darwin-arm64.json")
			case "key":
				flags = strings.ReplaceAll(flags, f.config.Updates.PublicKey, "another-key")
			case "version":
				flags = strings.ReplaceAll(flags, "=1.2.3", "=1.0.0")
			case "identifier":
				flags = strings.ReplaceAll(flags, "=io.github.gopurr.native", "=io.github.gopurr.app")
			case "development":
				flags = ""
			case "production":
				flags = strings.ReplaceAll(flags, "github.com/egoist/mygo.production=1", "github.com/egoist/mygo.production=0")
			case "override":
				flags += " -X github.com/egoist/mygo.production=0"
			case "suffix":
				flags = strings.ReplaceAll(flags, f.config.Updates.PublicKey, f.config.Updates.PublicKey+"extra")
			}
			bi.Settings[2].Value = flags
			if e := validateHelper(bi, f.config, f.options.bundleID); e == nil {
				t.Fatal("untrusted helper settings accepted")
			}
		})
	}
}
func TestRejectsEscapingSymlinksAndOutputInsideBundle(t *testing.T) {
	f := fixture(t)
	outside := filepath.Join(filepath.Dir(f.options.app), "private")
	os.WriteFile(outside, []byte("not bundled"), 0o600)
	if e := os.Symlink("../../../private", filepath.Join(f.options.app, "Contents/Resources/escape")); e != nil {
		t.Fatal(e)
	}
	if e := validateBundle(f.options, f.config, f.inspect); e == nil {
		t.Fatal("escaping symlink accepted")
	}
	os.Remove(filepath.Join(f.options.app, "Contents/Resources/escape"))
	f.options.out = filepath.Join(f.options.app, "Contents/output")
	if _, _, _, e := packageBundle(f.options, f.config, f.key); e == nil {
		t.Fatal("output inside input app accepted")
	}
	alias := filepath.Join(filepath.Dir(f.options.app), "output-alias")
	if e := os.Symlink(filepath.Join(f.options.app, "Contents/Resources"), alias); e != nil {
		t.Fatal(e)
	}
	f.options.out = filepath.Join(alias, "new-artifacts")
	if _, _, _, e := packageBundle(f.options, f.config, f.key); e == nil {
		t.Fatal("output symlink into app accepted")
	}
	if _, e := os.Stat(filepath.Join(f.options.app, "Contents/Resources/new-artifacts")); !os.IsNotExist(e) {
		t.Fatal("rejected output changed input app")
	}
}

func TestRejectsMissingOrMismatchedNativeBuildMarker(t *testing.T) {
	for _, scenario := range []string{"missing", "disabled", "schema", "ui", "backend", "feed", "key", "version"} {
		t.Run(scenario, func(t *testing.T) {
			f := fixture(t)
			path := filepath.Join(f.options.app, "Contents/Resources/native-build.json")
			var marker nativeBuild
			if e := readJSON(path, &marker); e != nil {
				t.Fatal(e)
			}
			if scenario == "missing" {
				os.Remove(path)
			} else {
				switch scenario {
				case "disabled":
					marker.UpdateEnabled = false
				case "schema":
					marker.Schema = 2
				case "ui":
					marker.UI = "mygo"
				case "backend":
					marker.Backend = "engine-v1"
				case "feed":
					marker.UpdateFeed = "https://example.com/update-darwin-arm64.json"
				case "key":
					marker.UpdatePublicKey = "new-key"
				case "version":
					marker.Version = "0.0.0"
				}
				b, e := json.Marshal(marker)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(path, b, 0o644); e != nil {
					t.Fatal(e)
				}
			}
			if e := validateBundle(f.options, f.config, f.inspect); e == nil {
				t.Fatal("untrusted build marker accepted")
			}
		})
	}
}
func TestKeepsExistingArtifactsAndDoesNotOverwrite(t *testing.T) {
	f := fixture(t)
	if _, _, _, e := packageBundle(f.options, f.config, f.key); e != nil {
		t.Fatal(e)
	}
	manifestPath := filepath.Join(f.options.out, manifestName)
	old, e := os.ReadFile(manifestPath)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, _, e = packageBundle(f.options, f.config, f.key); e == nil {
		t.Fatal("existing release overwritten")
	}
	now, e := os.ReadFile(manifestPath)
	if e != nil || !bytes.Equal(now, old) {
		t.Fatal("existing manifest changed")
	}
}
func TestCodeSignatureFailurePreventsPackaging(t *testing.T) {
	f := fixture(t)
	f.inspect.codeSign = func(string) error { return fmt.Errorf("signature rejected") }
	if e := run(args(f, "-sign", "-key-env", "TEST_KEY"), func(string) string { t.Fatal("read key before bundle validation"); return "" }, io.Discard, f.inspect); e == nil || e.Error() != "signature rejected" {
		t.Fatal(e)
	}
}
func TestFeedFormatsAndPrintedFlagsNeverReadSecrets(t *testing.T) {
	f := fixture(t)
	f.config.Updates.TagPrefix = "native-v"
	want := "https://github.com/csic21/Gopurr/releases/download/native-v{version}/" + manifestName
	if got := feed(f.config); got != want {
		t.Fatal(got)
	}
	f.config.Updates.GitHub = ""
	f.config.Updates.URL = "https://downloads.example.com/gopurr/"
	if got := feed(f.config); got != "https://downloads.example.com/gopurr/"+manifestName {
		t.Fatal(got)
	}
	var out bytes.Buffer
	if e := run([]string{"-config", f.options.configPath, "-print-link-flags"}, func(string) string { t.Fatal("printed flags read secret"); return "" }, &out, f.inspect); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), manifestName) || !strings.Contains(out.String(), f.config.Updates.PublicKey) {
		t.Fatal(out.String())
	}
}
