//go:build !windows

package mesh

import (
	"os"
	"testing"
)

func assertPrivateMeshFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("private state %s: %v %v", path, info, err)
	}
}
