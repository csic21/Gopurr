package core_test

import (
	"golang.org/x/sys/windows"
	"testing"
)

func assertNoNamedPipe(t *testing.T, name string) {
	t.Helper()
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err == nil {
		windows.CloseHandle(h)
		t.Fatal("subscription opened a Windows named pipe")
	}
	if err != windows.ERROR_FILE_NOT_FOUND {
		t.Fatalf("unexpected named pipe result: %v", err)
	}
}
