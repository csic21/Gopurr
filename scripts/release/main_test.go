package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyUpdateRejectsTamperedArchive(t *testing.T) {
	dir := t.TempDir()
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	name := "gopurr-1.2.3-windows-amd64.tar.gz"
	archive := []byte("a signed release archive")
	sum := sha256.Sum256(archive)
	manifest := map[string]any{
		"version":   "1.2.3",
		"url":       "https://github.com/csic21/Gopurr/releases/download/v1.2.3/" + name,
		"size":      len(archive),
		"signature": base64.StdEncoding.EncodeToString(ed25519.Sign(private, sum[:])),
	}
	body, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(dir, name), archive, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "update-windows-amd64.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	key := base64.StdEncoding.EncodeToString(public)
	if err := verifyUpdate(dir, "1.2.3", "windows-amd64", key); err != nil {
		t.Fatal(err)
	}
	archive[0] ^= 1
	if err := os.WriteFile(filepath.Join(dir, name), archive, 0o600); err != nil {
		t.Fatal(err)
	}
	if verifyUpdate(dir, "1.2.3", "windows-amd64", key) == nil {
		t.Fatal("tampered archive accepted")
	}
}
