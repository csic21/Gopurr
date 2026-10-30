package mesh

import (
	"golang.org/x/sys/windows"
	"regexp"
	"testing"
)

// Windows privacy is enforced by securefs DACLs, not POSIX mode bits. Check
// the effective file ACL without changing it, including inherited private ACEs.
func assertPrivateMeshFile(t *testing.T, path string) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	dacl, _, err := sd.DACL()
	aces := regexp.MustCompile(`\(A;[^)]*;;;([^;)]+)\)`).FindAllStringSubmatch(sd.String(), -1)
	if err != nil || dacl == nil || len(aces) < 2 || len(aces) > 3 || len(aces) != int(dacl.AceCount) {
		t.Fatalf("unexpected private file DACL %s: %s", path, sd.String())
	}
	for _, ace := range aces {
		aceSD, err := windows.SecurityDescriptorFromString("O:" + ace[1])
		if err != nil {
			t.Fatal(err)
		}
		sid, _, err := aceSD.Owner()
		if err != nil {
			t.Fatal(err)
		}
		if !sid.IsWellKnown(windows.WinLocalSystemSid) && !sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) && !sid.Equals(owner) {
			t.Fatalf("access granted to %s for %s: %s", ace[1], path, sd.String())
		}
	}
}
