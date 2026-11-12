package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCompleteArchiveUsesExistingTrustAndIdenticalFeeds(t *testing.T) {
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var c config
	c.Name, c.Slug, c.Version = "Gopurr", "gopurr", "0.6.24"
	c.Updates.GitHub = "csic21/Gopurr"
	c.Updates.TagPrefix = "v"
	c.Updates.PublicKey = base64.StdEncoding.EncodeToString(public)
	root, out := t.TempDir(), t.TempDir()
	files := map[string]string{"Gopurr.exe": "NSH host", "GopurrBridge.dll": "bridge", "gopurr-desktop.exe": "backend", "Uninstall.exe": "graceful NSH uninstaller", "nsh/bundle.nshc": "UI"}
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := packageDirectory(root, out, c, key, "Migration acceptance"); err != nil {
		t.Fatal(err)
	}
	native, err := os.ReadFile(filepath.Join(out, nativeManifest))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := os.ReadFile(filepath.Join(out, "update-windows-amd64.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(native, legacy) {
		t.Fatal("legacy clients receive a different update")
	}
	var m manifest
	if err := json.Unmarshal(native, &m); err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(filepath.Join(out, "gopurr-0.6.24-windows-amd64.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	signature, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive)
	if m.Version != c.Version || m.Size != int64(len(archive)) || !ed25519.Verify(public, sum[:], signature) || m.URL != "https://github.com/csic21/Gopurr/releases/download/v0.6.24/gopurr-0.6.24-windows-amd64.tar.gz" {
		t.Fatalf("invalid signed release: %+v", m)
	}
	zipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer zipped.Close()
	tr := tar.NewReader(zipped)
	seen := map[string]bool{}
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag == tar.TypeDir {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if files[header.Name] != string(body) {
			t.Fatalf("changed or unexpected archive resource: %s", header.Name)
		}
		seen[header.Name] = true
	}
	if len(seen) != len(files) {
		t.Fatalf("incomplete update: %v", seen)
	}
	if err := packageDirectory(root, out, c, key, ""); err == nil {
		t.Fatal("overwrote existing published artifacts")
	}
	after, err := os.ReadFile(filepath.Join(out, nativeManifest))
	if err != nil || !bytes.Equal(after, native) {
		t.Fatal("existing feed changed on refused overwrite")
	}
}
func TestSigningRejectsKeyRotationAndMalformedKeys(t *testing.T) {
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(key)
	if _, err := signingKey(encoded, base64.StdEncoding.EncodeToString(public)); err != nil {
		t.Fatal(err)
	}
	if _, err := signingKey(encoded, base64.StdEncoding.EncodeToString(other)); err == nil {
		t.Fatal("accepted a rotated public key")
	}
	if _, err := signingKey("", base64.StdEncoding.EncodeToString(public)); err == nil {
		t.Fatal("accepted missing key")
	}
	key[len(key)-1] ^= 1
	if _, err := signingKey(base64.StdEncoding.EncodeToString(key), base64.StdEncoding.EncodeToString(public)); err == nil {
		t.Fatal("accepted noncanonical private key")
	}
}
func TestReadActualGoLinkerSettings(t *testing.T) {
	symbols, err := linkSymbols(`-s -w -H=windowsgui -X "github.com/egoist/mygo.packageName=Gopurr" -X gopurr/internal/update.feed=https://github.com/csic21/Gopurr/releases/latest/download/update-native-windows-amd64.json`)
	if err != nil {
		t.Fatal(err)
	}
	if symbols["github.com/egoist/mygo.packageName"] != "Gopurr" || symbols["gopurr/internal/update.feed"] != "https://github.com/csic21/Gopurr/releases/latest/download/update-native-windows-amd64.json" {
		t.Fatal(symbols)
	}
	if _, err := linkSymbols(`-X "unterminated`); err == nil {
		t.Fatal("accepted invalid linker metadata")
	}
}
