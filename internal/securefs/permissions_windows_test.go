package securefs

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWindowsPrivateDACL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := PrivateDir(dir); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("directory inherits a broader DACL: %v", err)
	}
	file := filepath.Join(dir, "credentials")
	if err := WriteFile(file, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{dir, file} {
		sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
		if err != nil {
			t.Fatal(err)
		}
		aces := regexp.MustCompile(`\(A;[^)]*;;;([^;)]+)\)`).FindAllStringSubmatch(sd.String(), -1)
		dacl, _, err := sd.DACL()
		if err != nil || dacl == nil || len(aces) < 2 || len(aces) > 3 || len(aces) != int(dacl.AceCount) {
			t.Fatalf("unexpected DACL %s: %s", path, sd.String())
		}
		for _, ace := range aces {
			// SDDL may abbreviate a local administrator's SID as LA.
			aceSD, err := windows.SecurityDescriptorFromString("O:" + ace[1])
			if err != nil {
				t.Fatal(err)
			}
			sid, _, err := aceSD.Owner()
			if err != nil {
				t.Fatal(err)
			}
			if !sid.IsWellKnown(windows.WinLocalSystemSid) && !sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) && !sid.Equals(owner) {
				t.Fatalf("access granted to %s: %s", ace[1], sd.String())
			}
		}
	}
}

func TestWindowsServiceDirectoryOnlyAdministratorsAndSystem(t *testing.T) {
	if !windows.GetCurrentProcessToken().IsElevated() {
		t.Skip("requires the elevated Windows CI runner")
	}
	dir := filepath.Join(t.TempDir(), "Service")
	if err := AdministratorDir(dir); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	admin, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		t.Fatal(err)
	}
	if !owner.Equals(admin) {
		t.Fatalf("service owned by installing user: %s", owner)
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil || dacl.AceCount != 2 {
		t.Fatalf("unexpected service DACL: %s", sd.String())
	}
	for _, ace := range regexp.MustCompile(`\(A;[^)]*;;;([^;)]+)\)`).FindAllStringSubmatch(sd.String(), -1) {
		if ace[1] != "SY" && ace[1] != "BA" {
			t.Fatalf("unprivileged service access: %s", sd.String())
		}
	}
	// Newly staged service executables inherit the restricted ACL as well.
	if err := os.WriteFile(filepath.Join(dir, "core.exe"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
}
