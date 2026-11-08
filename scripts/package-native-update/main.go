// Command package-native-update validates and optionally signs an existing NSH
// desktop bundle. It never builds, installs, uploads, or reads a key file.
package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/macho"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

const manifestName = "update-native-darwin-arm64.json"
const maxArchive = 1 << 30 // mygo internal/update.MaxArchiveSize

type config struct {
	Name, Slug, Version string
	Updates             struct{ PublicKey, GitHub, TagPrefix, URL string }
}
type manifest struct {
	Version   string `json:"version"`
	Notes     string `json:"notes,omitempty"`
	Date      string `json:"date,omitempty"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	Signature string `json:"signature"`
}
type nativeBuild struct {
	Schema          int    `json:"schema"`
	UI              string `json:"ui"`
	Backend         string `json:"backend"`
	Version         string `json:"version"`
	UpdateFeed      string `json:"updateFeed"`
	UpdatePublicKey string `json:"updatePublicKey"`
	UpdateEnabled   bool   `json:"updateEnabled"`
	DataDirectory   string `json:"dataDirectory,omitempty"`
}
type options struct {
	app, out, configPath, bundleID, nshID, keyEnv, notes string
	sign, printFlags                                     bool
	production                                           bool
}
type inspectors struct {
	buildInfo func(string) (*debug.BuildInfo, error)
	codeSign  func(string) error
}

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout, inspectors{buildinfo.ReadFile, verifyCodeSign}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string, getenv func(string) string, out io.Writer, inspect inspectors) error {
	f := flag.NewFlagSet("package-native-update", flag.ContinueOnError)
	var o options
	f.StringVar(&o.app, "app", "", "existing NSH .app to validate; never modified")
	f.StringVar(&o.out, "out", "dist/native-darwin-arm64", "directory for new artifacts")
	f.StringVar(&o.configPath, "config", "mygo.json", "version and existing public update key")
	f.StringVar(&o.bundleID, "bundle-id", "io.github.gopurr.native", "required NSH host bundle identifier")
	f.StringVar(&o.nshID, "nsh-id", "io.github.gopurr.nsh.preview", "required embedded NSH app identifier")
	f.BoolVar(&o.sign, "sign", false, "explicitly sign and write the complete update archive and manifest")
	f.StringVar(&o.keyEnv, "key-env", "", "environment variable holding the base64 Ed25519 private key; required with -sign")
	f.StringVar(&o.notes, "notes", "", "optional release notes text file")
	f.BoolVar(&o.printFlags, "print-link-flags", false, "print release helper link flags and exit without reading any key")
	f.BoolVar(&o.production, "production", false, "require the established desktop identity and data location; emit the legacy feed alias")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	c, err := readConfig(o.configPath)
	if err != nil {
		return err
	}
	if o.production {
		o.bundleID = "io.github.gopurr.app"
	}
	if o.printFlags {
		fmt.Fprintln(out, linkFlags(c, o.bundleID))
		return nil
	}
	if o.app == "" {
		return errors.New("-app is required; default operation only validates the bundle")
	}
	if o.sign != (o.keyEnv != "") {
		return errors.New("signing requires both -sign and an explicit -key-env; no key files are read")
	}
	app, err := filepath.Abs(o.app)
	if err != nil {
		return err
	}
	o.app = app
	if err := validateBundle(o, c, inspect); err != nil {
		return err
	}
	if !o.sign {
		fmt.Fprintln(out, "Validated NSH bundle and existing update trust; no artifacts written.")
		return nil
	}
	key, err := privateKey(getenv(o.keyEnv), c.Updates.PublicKey)
	if err != nil {
		return err
	}
	m, archivePath, manifestPath, err := packageBundle(o, c, key)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Verified signed native update (%d bytes):\n%s\n%s\n", m.Size, archivePath, manifestPath)
	return nil
}
func readConfig(path string) (config, error) {
	var c config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err = json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if !versionPattern.MatchString(c.Version) || !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`).MatchString(c.Slug) {
		return c, errors.New("invalid version or slug in config")
	}
	pub, err := base64.StdEncoding.DecodeString(strings.TrimSpace(c.Updates.PublicKey))
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return c, errors.New("invalid updates.publicKey")
	}
	c.Updates.PublicKey = strings.TrimSpace(c.Updates.PublicKey)
	if c.Updates.TagPrefix == "" {
		c.Updates.TagPrefix = "v"
	}
	if (c.Updates.GitHub == "") == (c.Updates.URL == "") {
		return c, errors.New("config must have exactly one of updates.github and updates.url")
	}
	if c.Updates.GitHub != "" && !repoPattern.MatchString(c.Updates.GitHub) {
		return c, errors.New("invalid updates.github")
	}
	if c.Updates.URL != "" {
		u, e := url.Parse(c.Updates.URL)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return c, errors.New("updates.url must be an HTTPS directory URL")
		}
	}
	return c, nil
}
func feed(c config) string {
	if c.Updates.URL != "" {
		return strings.TrimRight(c.Updates.URL, "/") + "/" + manifestName
	}
	base := "https://github.com/" + c.Updates.GitHub + "/releases/"
	if c.Updates.TagPrefix != "v" {
		return base + "download/" + url.PathEscape(c.Updates.TagPrefix) + "{version}/" + manifestName
	}
	return base + "latest/download/" + manifestName
}
func archiveURL(c config, name string) string {
	if c.Updates.URL != "" {
		return strings.TrimRight(c.Updates.URL, "/") + "/" + url.PathEscape(name)
	}
	return "https://github.com/" + c.Updates.GitHub + "/releases/download/" + url.PathEscape(c.Updates.TagPrefix+c.Version) + "/" + url.PathEscape(name)
}
func linkFlags(c config, id string) string {
	values := [][2]string{{"github.com/egoist/mygo.production", "1"}, {"github.com/egoist/mygo.packageUpdateFeed", feed(c)}, {"github.com/egoist/mygo.packageUpdateKey", c.Updates.PublicKey}, {"github.com/egoist/mygo.packageName", c.Name}, {"github.com/egoist/mygo.packageVersion", c.Version}, {"github.com/egoist/mygo.packageIdentifier", id}, {"github.com/egoist/mygo.packageURLSchemes", "gopurr,clash"}, {"gopurr/internal/buildinfo.Version", c.Version}}
	var parts []string
	for _, p := range values {
		v := p[0] + "=" + p[1]
		if strings.ContainsAny(v, " \t\r\n\"'") {
			v = strconv.Quote(v)
		}
		parts = append(parts, "-X", v)
	}
	return strings.Join(parts, " ")
}
func privateKey(value, public string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(b) != ed25519.PrivateKeySize {
		return nil, errors.New("explicit key environment variable must contain a base64 64-byte Ed25519 private key")
	}
	k := ed25519.PrivateKey(b)
	canonical := ed25519.NewKeyFromSeed(k.Seed())
	if !k.Equal(canonical) {
		return nil, errors.New("invalid Ed25519 private key")
	}
	if base64.StdEncoding.EncodeToString(k.Public().(ed25519.PublicKey)) != public {
		return nil, errors.New("signing key does not match the existing updates.publicKey")
	}
	return k, nil
}
func validateBundle(o options, c config, inspect inspectors) error {
	info, err := os.Lstat(o.app)
	if err != nil {
		return err
	}
	if !info.IsDir() || !strings.HasSuffix(info.Name(), ".app") {
		return errors.New("input must be an actual .app directory, not a symlink")
	}
	plist, err := readPlist(filepath.Join(o.app, "Contents/Info.plist"))
	if err != nil {
		return err
	}
	if plist["CFBundleIdentifier"] != o.bundleID || plist["CFBundleShortVersionString"] != c.Version || plist["CFBundlePackageType"] != "APPL" {
		return errors.New("bundle identifier, version or package type does not match the native release")
	}
	var marker nativeBuild
	if err := readJSON(filepath.Join(o.app, "Contents/Resources/native-build.json"), &marker); err != nil {
		return fmt.Errorf("read native build marker: %w", err)
	}
	if marker.Schema != 1 || marker.UI != "nsh-desktop" || marker.Backend != "desktop-v2" || marker.Version != c.Version || !marker.UpdateEnabled || marker.UpdateFeed != feed(c) || marker.UpdatePublicKey != c.Updates.PublicKey {
		return errors.New("native build marker does not match the enabled native release and existing update trust")
	}
	if o.production && (marker.DataDirectory != "Gopurr" || plist["GopurrDataDirectory"] != "Gopurr" || plist["CFBundleExecutable"] != "Gopurr") {
		return errors.New("production NSH must preserve the desktop executable and data directory")
	}
	if o.production && plist["LSMinimumSystemVersion"] != "12.0" {
		return errors.New("production migration must retain the macOS 12 deployment floor")
	}
	exe := plist["CFBundleExecutable"]
	if exe == "" || filepath.Base(exe) != exe {
		return errors.New("invalid host executable name")
	}
	main := filepath.Join(o.app, "Contents/MacOS", exe)
	helper := filepath.Join(o.app, "Contents/Resources/gopurr-desktop")
	if err := validateMachO(main, true); err != nil {
		return fmt.Errorf("NSH host: %w", err)
	}
	if err := validateMachO(helper, false); err != nil {
		return fmt.Errorf("desktop helper: %w", err)
	}
	bi, err := inspect.buildInfo(helper)
	if err != nil {
		return fmt.Errorf("read desktop helper build metadata: %w", err)
	}
	if err := validateHelper(bi, c, o.bundleID); err != nil {
		return err
	}
	resources := filepath.Join(o.app, "Contents/Resources/nsh")
	var app struct {
		ID string `json:"id"`
	}
	if err := readJSON(filepath.Join(resources, "app.json"), &app); err != nil {
		return err
	}
	if app.ID != o.nshID {
		return errors.New("embedded NSH app identity mismatch")
	}
	var caps struct {
		Host []string `json:"host"`
	}
	if err := readJSON(filepath.Join(resources, "capabilities.json"), &caps); err != nil {
		return err
	}
	found := false
	for _, v := range caps.Host {
		found = found || v == "desktop.dispatch"
	}
	if !found {
		return errors.New("NSH bundle lacks the desktop.dispatch capability; refusing a mobile or legacy UI")
	}
	js, err := os.ReadFile(filepath.Join(resources, "bundle.js"))
	if err != nil {
		return err
	}
	if !strings.Contains(string(js), "desktop-nav-") || !strings.Contains(string(js), "desktop.dispatch") {
		return errors.New("NSH bundle is not the desktop UI")
	}
	for _, name := range []string{"Contents/Resources/nsh/bundle.nshc", "Contents/Resources/LICENSE", "Contents/Resources/THIRD_PARTY_NOTICES.md"} {
		fi, e := os.Lstat(filepath.Join(o.app, name))
		if e != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
			return fmt.Errorf("missing native release resource: %s", name)
		}
	}
	if err := walkBundle(o.app, func(string, fs.FileInfo, string) error { return nil }); err != nil {
		return err
	}
	return inspect.codeSign(o.app)
}
func validateHelper(bi *debug.BuildInfo, c config, id string) error {
	if bi.Path != "gopurr" {
		return errors.New("desktop helper is not the Gopurr backend")
	}
	settings := map[string]string{}
	for _, s := range bi.Settings {
		settings[s.Key] = s.Value
	}
	if settings["GOOS"] != "darwin" || settings["GOARCH"] != "arm64" {
		return errors.New("desktop helper is not darwin/arm64")
	}
	// Go records the complete linker invocation in build metadata even in stripped binaries.
	tokens, err := splitFlags(settings["-ldflags"])
	if err != nil {
		return err
	}
	symbols := map[string]string{}
	for i, t := range tokens {
		if t == "-X" && i+1 < len(tokens) {
			key, value, ok := strings.Cut(tokens[i+1], "=")
			if ok {
				symbols[key] = value
			}
		}
	}
	for _, pair := range [][2]string{{"github.com/egoist/mygo.production", "1"}, {"github.com/egoist/mygo.packageUpdateFeed", feed(c)}, {"github.com/egoist/mygo.packageUpdateKey", c.Updates.PublicKey}, {"github.com/egoist/mygo.packageVersion", c.Version}, {"github.com/egoist/mygo.packageIdentifier", id}, {"gopurr/internal/buildinfo.Version", c.Version}} {
		if symbols[pair[0]] != pair[1] {
			return fmt.Errorf("desktop helper is missing the trusted release linker setting %s", pair[0])
		}
	}
	return nil
}
func splitFlags(s string) ([]string, error) {
	var out []string
	for len(strings.TrimSpace(s)) > 0 {
		s = strings.TrimSpace(s)
		if s[0] == '"' {
			i := 1
			for ; i < len(s); i++ {
				if s[i] == '\\' {
					i++
					continue
				}
				if s[i] == '"' {
					break
				}
			}
			if i >= len(s) {
				return nil, errors.New("invalid quoted helper linker flags")
			}
			v, e := strconv.Unquote(s[:i+1])
			if e != nil {
				return nil, e
			}
			out = append(out, v)
			s = s[i+1:]
		} else {
			v, rest, ok := strings.Cut(s, " ")
			out = append(out, v)
			if !ok {
				break
			}
			s = rest
		}
	}
	return out, nil
}
func validateMachO(path string, catalyst bool) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
		return errors.New("expected a regular executable")
	}
	f, err := macho.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if f.Cpu != macho.CpuArm64 || f.Type != macho.TypeExec {
		return errors.New("expected an arm64 Mach-O executable")
	}
	if catalyst {
		found := false
		for _, load := range f.Loads {
			raw := load.Raw()
			if len(raw) >= 16 && f.ByteOrder.Uint32(raw[:4]) == 0x32 && f.ByteOrder.Uint32(raw[8:12]) == 6 {
				found = true
				if f.ByteOrder.Uint32(raw[12:16]) > 0x0f0000 {
					return errors.New("Catalyst host exceeds the iOSMac 15 / macOS 12 deployment floor")
				}
			}
		}
		if !found {
			return errors.New("expected the NSH Mac Catalyst host; refusing the legacy AppKit UI")
		}
	}
	return nil
}

type plistStrings map[string]string

func (p *plistStrings) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	*p = map[string]string{}
	key := ""
	for {
		t, e := d.Token()
		if e != nil {
			return e
		}
		switch v := t.(type) {
		case xml.StartElement:
			if v.Name.Local == "key" {
				if e = d.DecodeElement(&key, &v); e != nil {
					return e
				}
			} else if v.Name.Local == "string" {
				var value string
				if e = d.DecodeElement(&value, &v); e != nil {
					return e
				}
				(*p)[key] = value
				key = ""
			} else {
				if e = d.Skip(); e != nil {
					return e
				}
				key = ""
			}
		case xml.EndElement:
			if v.Name == start.Name {
				return nil
			}
		}
	}
}
func readPlist(path string) (plistStrings, error) {
	var doc struct {
		Dict plistStrings `xml:"dict"`
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	if e = xml.Unmarshal(b, &doc); e != nil {
		return nil, fmt.Errorf("read XML Info.plist: %w", e)
	}
	return doc.Dict, nil
}
func readJSON(path string, v any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func verifyCodeSign(app string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("native release packaging requires macOS codesign verification")
	}
	b, e := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", app).CombinedOutput()
	if e != nil {
		return fmt.Errorf("native bundle code signature is invalid: %s", strings.TrimSpace(string(b)))
	}
	return nil
}
func walkBundle(app string, visit func(string, fs.FileInfo, string) error) error {
	realApp, err := filepath.EvalSymlinks(app)
	if err != nil {
		return err
	}
	return filepath.WalkDir(app, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() && !info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("unsupported bundle entry %s", path)
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, e = os.Readlink(path)
			if e != nil {
				return e
			}
			target := filepath.Clean(filepath.Join(filepath.Dir(path), link))
			rel, e := filepath.Rel(app, target)
			if e != nil || filepath.IsAbs(link) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return errors.New("bundle symlink points outside the app")
			}
			real, e := filepath.EvalSymlinks(path)
			if e != nil {
				return e
			}
			rel, e = filepath.Rel(realApp, real)
			if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return errors.New("bundle symlink resolves outside the app")
			}
		}
		return visit(path, info, link)
	})
}
func packageBundle(o options, c config, key ed25519.PrivateKey) (manifest, string, string, error) {
	var m manifest
	out, err := destinationPath(o.out)
	if err != nil {
		return m, "", "", err
	}
	realApp, err := filepath.EvalSymlinks(o.app)
	if err != nil {
		return m, "", "", err
	}
	rel, err := filepath.Rel(realApp, out)
	if err != nil {
		return m, "", "", err
	}
	if rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return m, "", "", errors.New("output directory must be outside the input bundle")
	}
	if err = os.MkdirAll(out, 0o755); err != nil {
		return m, "", "", err
	}
	work, err := os.MkdirTemp(out, ".native-update-")
	if err != nil {
		return m, "", "", err
	}
	defer os.RemoveAll(work)
	name := c.Slug + "-native-" + c.Version + "-darwin-arm64.tar.gz"
	if o.production {
		name = c.Slug + "-" + c.Version + "-darwin-arm64.tar.gz"
	}
	archive := filepath.Join(work, name)
	f, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return m, "", "", err
	}
	hash := sha256.New()
	counter := &countWriter{w: io.MultiWriter(f, hash)}
	err = writeArchive(counter, o.app)
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return m, "", "", err
	}
	if counter.n > maxArchive {
		return m, "", "", errors.New("archive exceeds mygo 1 GiB limit")
	}
	m = manifest{Version: c.Version, Date: time.Now().UTC().Format(time.RFC3339), URL: archiveURL(c, name), Size: counter.n, Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, hash.Sum(nil)))}
	if o.notes != "" {
		b, e := os.ReadFile(o.notes)
		if e != nil {
			return m, "", "", e
		}
		if len(b) > 512<<10 {
			return m, "", "", errors.New("release notes exceed 512 KiB")
		}
		m.Notes = string(b)
	}
	pub, _ := base64.StdEncoding.DecodeString(c.Updates.PublicKey)
	signature, _ := base64.StdEncoding.DecodeString(m.Signature)
	if !ed25519.Verify(pub, hash.Sum(nil), signature) {
		return m, "", "", errors.New("generated archive signature failed verification")
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return m, "", "", err
	}
	if err = os.WriteFile(filepath.Join(work, manifestName), append(data, '\n'), 0o644); err != nil {
		return m, "", "", err
	}
	finalArchive, finalManifest := filepath.Join(out, name), filepath.Join(out, manifestName)
	if err = os.Link(archive, finalArchive); err != nil {
		return m, "", "", err
	}
	if err = os.Link(filepath.Join(work, manifestName), finalManifest); err != nil {
		os.Remove(finalArchive)
		return m, "", "", err
	}
	if o.production {
		// Existing installed versions request this name. Both manifests point
		// at the same complete NSH archive and retain the original public key.
		if err = os.Link(filepath.Join(work, manifestName), filepath.Join(out, "update-darwin-arm64.json")); err != nil {
			os.Remove(finalArchive)
			os.Remove(finalManifest)
			return m, "", "", err
		}
	}
	return m, finalArchive, finalManifest, nil
}

// Resolve existing ancestors before creating output, so a Finder alias-style
// symlink cannot accidentally put release artifacts inside the input app.
func destinationPath(path string) (string, error) {
	current, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var missing []string
	for {
		_, err = os.Lstat(current)
		if err == nil {
			real, e := filepath.EvalSymlinks(current)
			if e != nil {
				return "", e
			}
			parts := append([]string{real}, missing...)
			return filepath.Join(parts...), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = append([]string{filepath.Base(current)}, missing...)
		current = parent
	}
}

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(b []byte) (int, error) {
	n, e := c.w.Write(b)
	c.n += int64(n)
	return n, e
}
func writeArchive(w io.Writer, app string) error {
	gz, e := gzip.NewWriterLevel(w, gzip.BestCompression)
	if e != nil {
		return e
	}
	tw := tar.NewWriter(gz)
	e = walkBundle(app, func(path string, info fs.FileInfo, link string) error {
		hdr, e := tar.FileInfoHeader(info, link)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(filepath.Dir(app), path)
		if e != nil {
			return e
		}
		hdr.Name = filepath.ToSlash(rel)
		if info.IsDir() {
			hdr.Name += "/"
		}
		hdr.Uid, hdr.Gid, hdr.Uname, hdr.Gname = 0, 0, "", ""
		if e = tw.WriteHeader(hdr); e != nil {
			return e
		}
		if info.Mode().IsRegular() {
			f, e := os.Open(path)
			if e != nil {
				return e
			}
			_, e = io.Copy(tw, f)
			f.Close()
			return e
		}
		return nil
	})
	if e != nil {
		return e
	}
	if e = tw.Close(); e != nil {
		return e
	}
	return gz.Close()
}
