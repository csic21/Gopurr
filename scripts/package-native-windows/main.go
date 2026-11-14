// package-native-windows validates an existing NSH installation and signs the
// complete directory with the existing Ed25519 updater trust. No key files,
// installation, release uploads or production feed changes are performed.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"debug/buildinfo"
	"debug/pe"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

const target = "windows-amd64"
const nativeManifest = "update-native-" + target + ".json"

type config struct {
	Name, Slug, Version string
	Updates             struct{ PublicKey, GitHub, URL, TagPrefix string }
}
type marker struct {
	Schema                                                                     int
	UI, Backend, Platform, Version, UpdateFeed, UpdatePublicKey, DataDirectory string
	UpdateEnabled                                                              bool
}
type manifest struct {
	Version   string `json:"version"`
	Notes     string `json:"notes,omitempty"`
	Date      string `json:"date"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	Signature string `json:"signature"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	flags := flag.NewFlagSet("package-native-windows", flag.ContinueOnError)
	app := flags.String("app", "", "existing NSH Windows installation directory")
	out := flags.String("out", "dist/native-windows-amd64", "new artifact directory")
	configPath := flags.String("config", "mygo.json", "existing version and update trust")
	sign := flags.Bool("sign", false, "explicitly sign a complete update")
	keyEnv := flags.String("key-env", "", "explicit Ed25519 private-key environment variable")
	notes := flags.String("notes", "", "optional release notes file")
	if err := flags.Parse(args); err != nil {
		return err
	}
	var c config
	if err := readJSON(*configPath, &c); err != nil {
		return err
	}
	if !regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`).MatchString(c.Version) {
		return errors.New("invalid release version")
	}
	if c.Name != "Gopurr" || c.Slug != "gopurr" {
		return errors.New("preserve the established desktop identity")
	}
	if c.Updates.TagPrefix == "" {
		c.Updates.TagPrefix = "v"
	}
	if (c.Updates.GitHub == "") == (c.Updates.URL == "") {
		return errors.New("exactly one update source is required")
	}
	if c.Updates.GitHub != "" && !regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`).MatchString(c.Updates.GitHub) {
		return errors.New("invalid update repository")
	}
	if c.Updates.URL != "" {
		u, err := url.Parse(c.Updates.URL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("update directory must use HTTPS")
		}
	}
	public, err := base64.StdEncoding.DecodeString(c.Updates.PublicKey)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return errors.New("invalid existing update public key")
	}
	if *app == "" {
		return errors.New("an existing NSH installation is required")
	}
	root, err := filepath.Abs(*app)
	if err != nil {
		return err
	}
	if err = validate(root, c); err != nil {
		return err
	}
	if !*sign {
		fmt.Println("Validated NSH Windows host, backend, data identity and update trust.")
		return nil
	}
	if *keyEnv == "" {
		return errors.New("signing requires an explicit -key-env")
	}
	key, err := signingKey(os.Getenv(*keyEnv), c.Updates.PublicKey)
	if err != nil {
		return err
	}
	var releaseNotes []byte
	if *notes != "" {
		releaseNotes, err = os.ReadFile(*notes)
		if err != nil {
			return err
		}
	}
	destination, err := filepath.Abs(*out)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, destination)
	if err != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return errors.New("output cannot be inside the installation")
	}
	return packageDirectory(root, destination, c, key, string(releaseNotes))
}
func readJSON(path string, value any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, value)
}
func feed(c config) string {
	if c.Updates.URL != "" {
		return strings.TrimRight(c.Updates.URL, "/") + "/" + nativeManifest
	}
	base := "https://github.com/" + c.Updates.GitHub + "/releases/"
	if c.Updates.TagPrefix != "v" {
		return base + "download/" + url.PathEscape(c.Updates.TagPrefix) + "{version}/" + nativeManifest
	}
	return base + "latest/download/" + nativeManifest
}
func signingKey(value, public string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil || len(b) != ed25519.PrivateKeySize {
		return nil, errors.New("explicit environment must contain a base64 64-byte Ed25519 private key")
	}
	key := ed25519.PrivateKey(b)
	if !key.Equal(ed25519.NewKeyFromSeed(key.Seed())) || base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)) != public {
		return nil, errors.New("signing key does not match existing update trust")
	}
	return key, nil
}
func validate(root string, c config) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("installation must be a real directory")
	}
	required := []string{"Gopurr.exe", "gopurr-desktop.exe", "GopurrBridge.dll", "Uninstall.exe", "Gopurr.ico", "tray-windows-light.ico", "tray-windows-dark.ico", "native-build.json", "nsh/bundle.nshc", "nsh/app.json", "nsh/capabilities.json", "LICENSE", "THIRD_PARTY_NOTICES.md"}
	for _, name := range required {
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("missing regular native resource: %s", name)
		}
	}
	if err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !d.Type().IsRegular() {
			return fmt.Errorf("nonregular native resource: %s", path)
		}
		return nil
	}); err != nil {
		return err
	}
	for _, name := range []string{"Gopurr.exe", "gopurr-desktop.exe", "GopurrBridge.dll"} {
		binary, err := pe.Open(filepath.Join(root, name))
		if err != nil {
			return err
		}
		machine := binary.FileHeader.Machine
		isDLL := binary.FileHeader.Characteristics&pe.IMAGE_FILE_DLL != 0
		binary.Close()
		if machine != pe.IMAGE_FILE_MACHINE_AMD64 || isDLL != (strings.HasSuffix(name, ".dll")) {
			return fmt.Errorf("invalid Windows amd64 executable: %s", name)
		}
	}
	uninstaller, err := pe.Open(filepath.Join(root, "Uninstall.exe"))
	if err != nil {
		return err
	}
	validUninstaller := uninstaller.FileHeader.Machine == pe.IMAGE_FILE_MACHINE_I386 && uninstaller.FileHeader.Characteristics&pe.IMAGE_FILE_DLL == 0
	uninstaller.Close()
	if !validUninstaller {
		return errors.New("missing executable NSIS uninstaller in the complete update")
	}
	host, err := os.ReadFile(filepath.Join(root, "Gopurr.exe"))
	if err != nil {
		return err
	}
	if resourceString(host, "FileDescription") != "Gopurr NSH Desktop" || resourceString(host, "ProductVersion") != c.Version {
		return errors.New("frontend NSH identity/version does not match the backend release")
	}

	var m marker
	if err := readJSON(filepath.Join(root, "native-build.json"), &m); err != nil {
		return err
	}
	if m.Schema != 1 || m.UI != "nsh-desktop" || m.Backend != "desktop-v2" || m.Platform != target || m.Version != c.Version || m.DataDirectory != "Gopurr" || !m.UpdateEnabled || m.UpdateFeed != feed(c) || m.UpdatePublicKey != c.Updates.PublicKey {
		return errors.New("NSH build marker does not preserve release identity and trust")
	}
	var app struct {
		ID string `json:"id"`
	}
	if err = readJSON(filepath.Join(root, "nsh/app.json"), &app); err != nil {
		return err
	}
	if app.ID != "io.github.gopurr.nsh.preview" {
		return errors.New("unexpected desktop NSH bundle identity")
	}
	bi, err := buildinfo.ReadFile(filepath.Join(root, "gopurr-desktop.exe"))
	if err != nil {
		return err
	}
	settings := map[string]string{}
	for _, s := range bi.Settings {
		settings[s.Key] = s.Value
	}
	if bi.Path != "gopurr" || settings["GOOS"] != "windows" || settings["GOARCH"] != "amd64" {
		return errors.New("backend is not the Gopurr Windows amd64 application")
	}
	symbols, err := linkSymbols(settings["-ldflags"])
	if err != nil {
		return err
	}
	for _, pair := range [][2]string{{"github.com/egoist/mygo.production", "1"}, {"gopurr/internal/update.feed", feed(c)}, {"gopurr/internal/update.publicKey", c.Updates.PublicKey}, {"github.com/egoist/mygo.packageIdentifier", "io.github.gopurr.app"}, {"github.com/egoist/mygo.packageVersion", c.Version}, {"gopurr/internal/buildinfo.Version", c.Version}} {
		if symbols[pair[0]] != pair[1] {
			return fmt.Errorf("missing trusted helper linker setting %s", pair[0])
		}
	}
	return nil
}
func linkSymbols(input string) (map[string]string, error) {
	var tokens []string
	for len(strings.TrimSpace(input)) > 0 {
		input = strings.TrimSpace(input)
		if input[0] == '"' {
			i := 1
			for ; i < len(input); i++ {
				if input[i] == '\\' {
					i++
					continue
				}
				if input[i] == '"' {
					break
				}
			}
			if i >= len(input) {
				return nil, errors.New("unterminated linker flag")
			}
			value, err := strconv.Unquote(input[:i+1])
			if err != nil {
				return nil, err
			}
			tokens = append(tokens, value)
			input = input[i+1:]
		} else {
			word, rest, _ := strings.Cut(input, " ")
			tokens = append(tokens, word)
			input = rest
		}
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
	return symbols, nil
}
func packageDirectory(root, out string, c config, key ed25519.PrivateKey, notes string) error {
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	name := "gopurr-" + c.Version + "-" + target + ".tar.gz"
	for _, name := range []string{name, nativeManifest, "update-" + target + ".json"} {
		if _, err := os.Lstat(filepath.Join(out, name)); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("refusing to overwrite %s", name)
		}
	}
	file, err := os.CreateTemp(out, ".native-windows-*.tar.gz")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	hash := sha256.New()
	gzipWriter := gzip.NewWriter(io.MultiWriter(file, hash))
	tw := tar.NewWriter(gzipWriter)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return errors.New("native payload changed during packaging")
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relative)
		header.Uid, header.Gid = 0, 0
		header.Uname, header.Gname = "", ""
		if err = tw.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		_, err = io.Copy(tw, input)
		closeErr := input.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	for _, close := range []func() error{tw.Close, gzipWriter.Close, file.Close} {
		if e := close(); err == nil {
			err = e
		}
	}
	if err != nil {
		return err
	}
	info, err := os.Stat(file.Name())
	if err != nil {
		return err
	}
	if info.Size() <= 0 || info.Size() > 1<<30 {
		return errors.New("invalid update archive size")
	}
	archiveURL := strings.TrimRight(c.Updates.URL, "/") + "/" + name
	if c.Updates.GitHub != "" {
		archiveURL = "https://github.com/" + c.Updates.GitHub + "/releases/download/" + url.PathEscape(c.Updates.TagPrefix+c.Version) + "/" + name
	}
	m := manifest{Version: c.Version, Notes: notes, Date: time.Now().UTC().Format(time.RFC3339), URL: archiveURL, Size: info.Size(), Signature: base64.StdEncoding.EncodeToString(ed25519.Sign(key, hash.Sum(nil)))}
	encoded, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err = os.Rename(file.Name(), filepath.Join(out, name)); err != nil {
		return err
	}
	for _, name := range []string{nativeManifest, "update-" + target + ".json"} {
		if err = os.WriteFile(filepath.Join(out, name), encoded, 0644); err != nil {
			return err
		}
	}
	fmt.Printf("Signed complete NSH Windows update (%d bytes); legacy and native feeds are identical.\n", info.Size())
	return nil
}

// Read the UTF-16 string-table values emitted by winresource in the PE resource
// section. Release versions are ASCII semver; embedded NULs are never accepted.
func resourceString(data []byte, key string) string {
	words := utf16.Encode([]rune(key + "\x00"))
	pattern := make([]byte, len(words)*2)
	for i, w := range words {
		binary.LittleEndian.PutUint16(pattern[i*2:], w)
	}
	offset := bytes.Index(data, pattern)
	if offset < 0 {
		return ""
	}
	offset += len(pattern)
	// VERSIONINFO aligns values to a DWORD after the key's terminating NUL.
	offset = (offset + 3) &^ 3
	var value []uint16
	for offset+1 < len(data) && len(value) < 256 {
		w := binary.LittleEndian.Uint16(data[offset:])
		if w == 0 {
			return string(utf16.Decode(value))
		}
		value = append(value, w)
		offset += 2
	}
	return ""
}
