package nativehost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"gopurr/internal/securefs"
)

func TestWindowsPrivateSessionAndAuthentication(t *testing.T) {
	dir := t.TempDir()
	if err := securefs.PrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	tokenPath, socket := filepath.Join(dir, "auth"), filepath.Join(dir, "rpc")
	if err := securefs.WriteFile(tokenPath, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadToken(tokenPath); err != nil || got != token {
		t.Fatalf("read token: %q %v", got, err)
	}
	calls := 0
	server, err := Listen(socket, token, func(context.Context, string, json.RawMessage) (any, error) { calls++; return "ok", nil })
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if body, err := Call(ctx, socket, "wrong", "echo", nil); err != nil || !strings.Contains(string(body), "unauthorized") || calls != 0 {
		t.Fatalf("auth: %s %v", body, err)
	}
	if body, err := Call(ctx, socket, token, "echo", nil); err != nil || !strings.Contains(string(body), "ok") || calls != 1 {
		t.Fatalf("call: %s %v", body, err)
	}
	if err := server.Close(); err != nil {
		t.Fatal(err)
	}
	if err := CleanupSession(socket, tokenPath, token); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(tokenPath); !os.IsNotExist(err) {
		t.Fatalf("token survived cleanup: %v", err)
	}
}

func TestWindowsRejectsPublicSessionWithoutChangingACL(t *testing.T) {
	dir := t.TempDir()
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	if err = windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil); err != nil {
		t.Fatal(err)
	}
	before, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if err := privateParent(filepath.Join(dir, "auth")); err == nil {
		t.Fatal("public Windows ACL accepted")
	}
	after, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if before.String() != after.String() {
		t.Fatal("validation changed caller ACL")
	}
}
