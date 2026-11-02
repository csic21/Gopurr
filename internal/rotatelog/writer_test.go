package rotatelog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotationBoundsOldAndNewLogs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.log")
	os.WriteFile(path, bytes.Repeat([]byte("old\n"), 1000), 0600)
	w, err := open(path, 128, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		if _, err = w.Write(bytes.Repeat([]byte{byte(i + 65)}, 90)); err != nil {
			t.Fatal(err)
		}
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(path + "*")
	if len(files) != 3 {
		t.Fatalf("files=%v", files)
	}
	for _, p := range files {
		st, _ := os.Stat(p)
		if st.Size() > 128 {
			t.Fatalf("unbounded file %s: %d", p, st.Size())
		}
	}
}
func TestDuplicateRecordsKeepCountAndLastDifferentError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "core.log")
	w, err := open(path, 1024, 3)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		w.Write([]byte("2026/11/02 20:00:00 connection canceled\n"))
	}
	w.Write([]byte("time=\"2026-11-02\" level=error changed error\n"))
	w.Close()
	data, _ := os.ReadFile(path)
	if strings.Count(string(data), "connection canceled") != 1 || !strings.Contains(string(data), "repeated 99 times") || !strings.Contains(string(data), "changed error") {
		t.Fatalf("lost diagnostic: %s", data)
	}
	if _, err = w.Write([]byte("closed")); err == nil {
		t.Fatal("write after close succeeded")
	}
}
func TestEmptyWrite(t *testing.T) {
	w, err := open(filepath.Join(t.TempDir(), "log"), 32, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err = w.Write(nil); err != nil {
		t.Fatal(err)
	}
}
