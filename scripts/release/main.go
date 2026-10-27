// Command release builds the supported installers and signed update archives.
// It uses mygo.json as the single version source for the UI, core and package.
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z]+([.-][0-9A-Za-z]+)*)?$`)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	platform := flag.String("platform", runtime.GOOS+"/"+runtime.GOARCH, "darwin/arm64 or windows/amd64")
	version := flag.String("version", "", "release version, with or without v")
	release := flag.Bool("release", false, "require a signing key and stage release artifacts in dist")
	output := flag.String("out", "bin", "build output directory")
	skipDMG := flag.Bool("skip-dmg", false, "build the local macOS app bundle only")
	flag.Parse()
	if *platform != "darwin/arm64" && *platform != "windows/amd64" {
		return fmt.Errorf("unsupported release platform %q", *platform)
	}
	original, err := os.ReadFile("mygo.json")
	if err != nil {
		return err
	}
	var config map[string]any
	if err := json.Unmarshal(original, &config); err != nil {
		return err
	}
	current, _ := config["version"].(string)
	if *version != "" {
		current = strings.TrimPrefix(*version, "v")
	}
	if !versionPattern.MatchString(current) {
		return fmt.Errorf("invalid release version %q", current)
	}
	config["version"] = current
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile("mygo.json", append(encoded, '\n'), 0o644); err != nil {
		return err
	}
	defer func() {
		if err := os.WriteFile("mygo.json", original, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "restore mygo.json:", err)
		}
	}()
	env := os.Environ()
	flags := strings.Fields(os.Getenv("GOFLAGS"))
	hasTags := false
	for i, flag := range flags {
		if tags, ok := strings.CutPrefix(flag, "-tags="); ok {
			hasTags = true
			if !strings.Contains(","+tags+",", ",with_gvisor,") {
				flags[i] = "-tags=" + tags + ",with_gvisor"
			}
		}
	}
	if !hasTags {
		flags = append(flags, "-tags=with_gvisor")
	}
	env = append(env, "GOFLAGS="+strings.Join(flags, " "))
	if os.Getenv("MYGO_UPDATER_PRIVATE_KEY") == "" {
		if key, err := os.ReadFile(".secrets/mygo-update.key"); err == nil {
			env = append(env, "MYGO_UPDATER_PRIVATE_KEY="+strings.TrimSpace(string(key)))
		} else if *release {
			return errors.New("release needs the MYGO_UPDATER_PRIVATE_KEY GitHub Actions secret")
		}
	}
	args := []string{"run", "github.com/egoist/mygo/cmd/mygo", "build", "-platform", *platform, "-skip-notarize", "-o", *output}
	if *skipDMG {
		args = append(args, "-skip-dmg")
	}
	cmd := exec.Command("go", args...)
	cmd.Env, cmd.Stdout, cmd.Stderr = env, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	if !*release {
		return nil
	}
	target := strings.ReplaceAll(*platform, "/", "-")
	stage := filepath.Join(*output, target)
	installer := "gopurr-" + current + "-arm64.dmg"
	if *platform == "windows/amd64" {
		installer = "gopurr-setup-" + current + "-amd64.exe"
	}
	names := []string{installer, "gopurr-" + current + "-" + target + ".tar.gz", "update-" + target + ".json"}
	destination := filepath.Join("dist", target)
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return err
	}
	for _, name := range names {
		if err := copyFile(filepath.Join(stage, name), filepath.Join(destination, name)); err != nil {
			return fmt.Errorf("missing release artifact %s: %w", name, err)
		}
	}
	updates, _ := config["updates"].(map[string]any)
	publicKey, _ := updates["publicKey"].(string)
	if err := verifyUpdate(destination, current, target, publicKey); err != nil {
		return err
	}
	fmt.Printf("Release artifacts: %s\n", destination)
	return nil
}

func verifyUpdate(dir, version, target, key string) error {
	var manifest struct {
		Version, URL, Signature string
		Size                    int64
	}
	body, err := os.ReadFile(filepath.Join(dir, "update-"+target+".json"))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return err
	}
	name := "gopurr-" + version + "-" + target + ".tar.gz"
	if manifest.Version != version || !strings.HasSuffix(manifest.URL, "/releases/download/v"+version+"/"+name) {
		return errors.New("update manifest version or download URL does not match release")
	}
	archive, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer archive.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, archive)
	if err != nil {
		return err
	}
	public, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(public) != ed25519.PublicKeySize {
		return errors.New("invalid update public key")
	}
	signature, err := base64.StdEncoding.DecodeString(manifest.Signature)
	if err != nil || size != manifest.Size || !ed25519.Verify(ed25519.PublicKey(public), hash.Sum(nil), signature) {
		return errors.New("release update archive failed size or Ed25519 signature verification")
	}
	fmt.Printf("Verified %s update signature (%d bytes).\n", target, size)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		return errors.New("empty artifact")
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	closeErr := out.Close()
	if err != nil {
		return err
	}
	return closeErr
}
