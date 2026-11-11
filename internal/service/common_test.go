//go:build !windows

package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Mihomo logs this on every network change and keeps running. Quoting it as the
// reason a core "did not start" sends users after the wrong problem.
func TestLogTailSkipsBenignTUNNoise(t *testing.T) {
	dir := t.TempDir()
	body := `time="2026-11-04T11:50:30.608364000+08:00" level=info msg="Initial configuration complete, total time: 106ms"
time="2026-11-04T11:50:30.608364000+08:00" level=error msg="[TUN] default interface lost by monitor"
`
	if err := os.WriteFile(filepath.Join(dir, "service.log"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := LogTail(dir)
	if strings.Contains(entry.Text, "default interface lost") {
		t.Fatalf("benign TUN noise reported as the failure: %q", entry.Text)
	}
	if !strings.Contains(entry.Text, "Initial configuration") {
		t.Fatalf("no usable line left: %q", entry.Text)
	}
	if entry.At.IsZero() || entry.At.Year() != 2026 {
		t.Fatalf("log time not parsed: %v", entry.At)
	}
}

func TestLogTailPrefersLastCoreError(t *testing.T) {
	dir := t.TempDir()
	body := `time="2026-11-11T10:00:00+08:00" level=info msg="start"
time="2026-11-11T10:00:01+08:00" level=warning msg="[TCP] dial timeout"
time="2026-11-11T10:00:02+08:00" level=error msg="bind: address already in use"
time="2026-11-11T10:00:03+08:00" level=info msg="later"
`
	if err := os.WriteFile(filepath.Join(dir, "service.log"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	entry := LogTail(dir)
	if !strings.Contains(entry.Text, "address already in use") {
		t.Fatalf("last core error not reported: %q", entry.Text)
	}
	newest := time.Date(2026, 11, 11, 10, 0, 3, 0, time.FixedZone("", 8*60*60))
	if !entry.At.Equal(newest) {
		t.Fatalf("freshness = %v, want %v", entry.At, newest)
	}
}

func TestLogTailReportsMissingLog(t *testing.T) {
	entry := LogTail(t.TempDir())
	if entry.Text != "" || !entry.At.IsZero() {
		t.Fatalf("missing log produced %+v", entry)
	}
}
