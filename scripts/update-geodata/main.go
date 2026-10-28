// Command update-geodata refreshes the offline baseline from a single upstream
// release commit. Published apps continue updating their own runtime databases.
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopurr/internal/geodata"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ref := flag.String("ref", "", "upstream commit (default: resolve release branch)")
	flag.Parse()
	if *ref == "" {
		out, err := exec.Command("git", "ls-remote", "https://github.com/MetaCubeX/meta-rules-dat.git", "refs/heads/release").Output()
		if err != nil {
			return fmt.Errorf("resolve geodata release: %w", err)
		}
		fields := strings.Fields(string(out))
		if len(fields) != 2 {
			return fmt.Errorf("missing geodata release commit")
		}
		*ref = fields[0]
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	compressed := map[string][]byte{}
	manifest := struct {
		Repository string            `json:"repository"`
		Commit     string            `json:"commit"`
		SHA256     map[string]string `json:"sha256"`
	}{"https://github.com/MetaCubeX/meta-rules-dat", *ref, map[string]string{}}
	names := make([]string, 0, len(geodata.Files))
	for name := range geodata.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		source := "https://raw.githubusercontent.com/MetaCubeX/meta-rules-dat/" + *ref + "/" + geodata.Files[name]
		data, err := download(client, source)
		if err != nil {
			return err
		}
		if err := geodata.Validate(name, data); err != nil {
			return fmt.Errorf("invalid %s: %w", name, err)
		}
		var buf bytes.Buffer
		w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		if _, err := w.Write(data); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		compressed[name] = buf.Bytes()
		manifest.SHA256[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	// Download and validate everything before replacing any checked-in asset.
	dir := filepath.Join("internal", "geodata", "data")
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name+".gz"), compressed[name], 0o644); err != nil {
			return err
		}
		fmt.Printf("%s: %d compressed bytes\n", name, len(compressed[name]))
	}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	return os.WriteFile(filepath.Join(dir, "manifest.json"), append(data, '\n'), 0o644)
}

func download(client *http.Client, source string) ([]byte, error) {
	resp, err := client.Get(source)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", source, resp.StatusCode)
	}
	const limit = 64 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err == nil && len(data) > limit {
		err = fmt.Errorf("%s exceeds 64 MB", source)
	}
	return data, err
}
