package autostart

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoginAgentEscapesExecutableAndRemovesEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents", "login.plist")
	executable := "/Applications/Gopurr & Friends.app/Contents/MacOS/Gopurr"
	if err := writeAgent(path, executable, true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc any
	if err := xml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Gopurr &amp; Friends") || !strings.Contains(string(b), "--autostart") || strings.Contains(string(b), "KeepAlive") {
		t.Fatalf("invalid login agent: %s", b)
	}
	if err := writeAgent(path, executable, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("entry survived disable")
	}
	if err := writeAgent(path, executable, false); err != nil {
		t.Fatal(err)
	}
}
