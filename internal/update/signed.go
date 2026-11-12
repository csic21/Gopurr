package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
)

// Linker flags from the release build. Empty in development, which disables
// self-update. The manifest format and Ed25519 key stay compatible with
// already published archives.
var (
	feed      string
	publicKey string
)

var (
	startExe, _ = os.Executable()
	pending     atomic.Pointer[stagedUpdate]
)

type stagedUpdate struct {
	work, bundle, target string
	helper               *exec.Cmd
}

type manifest struct {
	Version   string `json:"version"`
	Notes     string `json:"notes,omitempty"`
	Date      string `json:"date,omitempty"`
	URL       string `json:"url"`
	Size      int64  `json:"size"`
	Signature string `json:"signature"`
}

const maxArchiveSize = 1 << 30

func (Native) Enabled() bool {
	if feed == "" || publicKey == "" {
		return false
	}
	target, err := installTarget()
	if err != nil {
		return false
	}
	probe, err := os.MkdirTemp(filepath.Dir(target), ".gopurr-update-")
	if err != nil {
		return false
	}
	os.Remove(probe)
	return true
}

func (Native) Check(ctx context.Context) (*Release, error) {
	if !(Native{}).Enabled() {
		return nil, errors.New("updates are not enabled for this build")
	}
	ctx = requestContext(ctx)
	body, err := httpGet(ctx, feed)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var m manifest
	if err = json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&m); err != nil {
		return nil, err
	}
	if m.Version == "" || m.URL == "" || m.Signature == "" {
		return nil, errors.New("incomplete update manifest")
	}
	if Compare(m.Version, currentVersion()) <= 0 {
		return nil, nil
	}
	key := publicKey
	return &Release{Version: m.Version, Notes: m.Notes, Install: func(ctx context.Context, progress func(int64, int64)) error {
		return installArchive(requestContext(ctx), m, key, progress)
	}}, nil
}

func currentVersion() string {
	// Imported here would cycle through buildinfo only; the variable is set
	// by the same linker step. Callers in this package read it via a hook
	// assigned from version.go's init in the main module... use the feed
	// comparison value stored below.
	if versionHook != nil {
		if v := versionHook(); v != "" && v != "dev" {
			return v
		}
	}
	return "0.0.0"
}

// SetVersionSource supplies the packaged version. The main module sets it
// from buildinfo so this package does not import the daemon.
var versionHook func() string

func SetVersionSource(fn func() string) { versionHook = fn }

func installArchive(ctx context.Context, m manifest, key string, progress func(int64, int64)) error {
	target, err := installTarget()
	if err != nil {
		return err
	}
	pub, err := parseKey(key)
	if err != nil {
		return err
	}
	if m.Size <= 0 || m.Size > maxArchiveSize {
		return fmt.Errorf("invalid update size %d", m.Size)
	}
	work, err := os.MkdirTemp(filepath.Dir(target), "."+filepath.Base(target)+".update-")
	if err != nil {
		return fmt.Errorf("cannot write next to the app: %w", err)
	}
	staged := false
	defer func() {
		if !staged {
			os.RemoveAll(work)
		}
	}()
	archive := filepath.Join(work, "update.tar.gz")
	if err = downloadVerified(ctx, m.URL, m.Size, m.Signature, pub, archive, progress); err != nil {
		return err
	}
	unpacked := filepath.Join(work, "app")
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	err = extractArchive(f, unpacked)
	f.Close()
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		if err = stageBundle(work, unpacked, target); err != nil {
			return err
		}
		staged = true
		return nil
	}
	return swapFiles(unpacked, target)
}

func installTarget() (string, error) {
	exe := startExe
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	if runtime.GOOS == "darwin" {
		bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
		if !strings.HasSuffix(bundle, ".app") {
			return "", errors.New("only app bundles can update themselves")
		}
		return bundle, nil
	}
	return filepath.Dir(exe), nil
}

func parseKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("invalid update public key")
	}
	return ed25519.PublicKey(b), nil
}

func downloadVerified(ctx context.Context, raw string, size int64, signature string, pub ed25519.PublicKey, path string, progress func(int64, int64)) error {
	body, err := httpGet(ctx, raw)
	if err != nil {
		return err
	}
	defer body.Close()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	var got int64
	buf := make([]byte, 32*1024)
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			got += int64(n)
			if got > size || got > maxArchiveSize {
				f.Close()
				return errors.New("update is larger than its manifest")
			}
			if _, err = f.Write(buf[:n]); err != nil {
				f.Close()
				return err
			}
			if _, err = hash.Write(buf[:n]); err != nil {
				f.Close()
				return err
			}
			if progress != nil {
				progress(got, size)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	if err = f.Close(); err != nil {
		return err
	}
	if got != size {
		return fmt.Errorf("update size %d, manifest says %d", got, size)
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil || !ed25519.Verify(pub, hash.Sum(nil), sig) {
		return errors.New("the update is not signed with the app's key")
	}
	return nil
}

func httpGet(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		return nil, fmt.Errorf("refusing to download %s: updates need HTTPS", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "gopurr-updater")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	return resp.Body, nil
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Compare matches the published updater: numeric fields, and a prerelease
// sorts before its release.
func Compare(a, b string) int {
	a, b = strings.TrimPrefix(a, "v"), strings.TrimPrefix(b, "v")
	a, _, _ = strings.Cut(a, "+")
	b, _, _ = strings.Cut(b, "+")
	ar, apre, _ := strings.Cut(a, "-")
	br, bpre, _ := strings.Cut(b, "-")
	if c := compareFields(ar, br); c != 0 {
		return c
	}
	switch {
	case apre == bpre:
		return 0
	case apre == "":
		return 1
	case bpre == "":
		return -1
	}
	return compareFields(apre, bpre)
}

func compareFields(a, b string) int {
	af, bf := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(af) || i < len(bf); i++ {
		var x, y string
		if i < len(af) {
			x = af[i]
		}
		if i < len(bf) {
			y = bf[i]
		}
		xn, xerr := strconv.Atoi(x)
		yn, yerr := strconv.Atoi(y)
		switch {
		case x == y:
			continue
		case x == "":
			return -1
		case y == "":
			return 1
		case xerr == nil && yerr == nil:
			if xn < yn {
				return -1
			}
			if xn > yn {
				return 1
			}
		case xerr == nil:
			return -1
		case yerr == nil:
			return 1
		default:
			return strings.Compare(x, y)
		}
	}
	return 0
}

const installAfterExit = `pid=$1 target=$2 new=$3 work=$4
shift 4
trap '' HUP
while kill -0 "$pid" 2>/dev/null; do sleep 0.05; done
if mv "$target" "$work/old"; then
	mv "$new" "$target" || mv "$work/old" "$target"
fi
relaunch=
if [ -e "$work/relaunch" ]; then relaunch=1; fi
rm -rf "$work"
if [ -n "$relaunch" ]; then exec /usr/bin/open "$@"; fi
`

func stageBundle(work, dir, target string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".app") {
		return errors.New("the update holds no app bundle")
	}
	p := &stagedUpdate{work: work, bundle: filepath.Join(dir, entries[0].Name()), target: target}
	args := []string{"-c", installAfterExit, "gopurr-update", strconv.Itoa(os.Getpid()), p.target, p.bundle, p.work, p.target}
	if len(os.Args) > 1 {
		args = append(append(args, "--args"), os.Args[1:]...)
	}
	p.helper = exec.Command("/bin/sh", args...)
	if err = p.helper.Start(); err != nil {
		return err
	}
	if old := pending.Swap(p); old != nil && old.helper != nil && old.helper.Process != nil {
		_ = old.helper.Process.Kill()
		_ = old.helper.Wait()
		_ = os.RemoveAll(old.work)
	}
	return nil
}

func swapFiles(dir, target string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return errors.New("the update is empty")
	}
	suffix := ".old-" + strconv.Itoa(os.Getpid())
	var moved [][2]string
	undo := func() {
		for i := len(moved) - 1; i >= 0; i-- {
			_ = os.RemoveAll(moved[i][0])
			_ = os.Rename(moved[i][1], moved[i][0])
		}
	}
	for _, e := range entries {
		dst := filepath.Join(target, e.Name())
		if _, err := os.Lstat(dst); err == nil {
			aside := filepath.Join(target, "."+e.Name()+suffix)
			if err = os.Rename(dst, aside); err != nil {
				undo()
				return err
			}
			moved = append(moved, [2]string{dst, aside})
		}
		if err = os.Rename(filepath.Join(dir, e.Name()), dst); err != nil {
			undo()
			return err
		}
	}
	for _, m := range moved {
		_ = os.RemoveAll(m[1])
	}
	return nil
}

// Relaunch starts the updated app. On macOS the staged helper swaps the
// bundle after this process exits and opens it through Launch Services.
func Relaunch() error {
	return ActivateRelaunch(startExe, os.Args[1:]...)
}

// ActivateRelaunch finishes a staged update, or starts the named executable.
// A staged macOS update exits immediately so the shell helper can swap the
// bundle and open it. Windows exits after starting the replacement so mapped
// files can be renamed.
func ActivateRelaunch(exe string, args ...string) error {
	if p := pending.Load(); p != nil {
		if err := os.WriteFile(filepath.Join(p.work, "relaunch"), nil, 0o644); err != nil {
			return err
		}
		os.Exit(0)
	}
	if exe == "" {
		exe = startExe
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir, _ = os.Getwd()
	configureDetached(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	if runtime.GOOS == "windows" {
		os.Exit(0)
	}
	return nil
}

func extractArchive(r io.Reader, dir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(hdr.Name)
		if !fs.ValidPath(name) || name == "." {
			return fmt.Errorf("invalid path in update archive: %q", hdr.Name)
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		mode := fs.FileMode(hdr.Mode).Perm()
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err = os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode|0o200)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		case tar.TypeSymlink:
			resolved := path.Join(path.Dir(name), hdr.Linkname)
			if path.IsAbs(hdr.Linkname) || !fs.ValidPath(resolved) {
				return fmt.Errorf("update archive link %q points outside the app", hdr.Name)
			}
			if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err = os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported entry in update archive: %q", hdr.Name)
		}
	}
}
