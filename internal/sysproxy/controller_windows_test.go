package sysproxy

import (
	"path/filepath"
	"testing"
)

func TestWindowsJournalReusesExistingMutex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recovery.json")
	first, err := lockWindowsJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	defer first()
	// CreateMutex returns a usable handle together with ERROR_ALREADY_EXISTS.
	// Re-entry on this OS thread is legal and must balance both acquisitions.
	second, err := lockWindowsJournal(path)
	if err != nil {
		t.Fatalf("existing journal mutex was rejected: %v", err)
	}
	defer second()
}
