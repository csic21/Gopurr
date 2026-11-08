package nativehost

import (
	"os"
	"path/filepath"
)

// IsCanonicalHome compares directory identities rather than spelling. Windows
// shortcuts can use another case or an 8.3 path for the same roaming directory;
// those paths must still acquire the legacy singleton before migrating data.
func IsCanonicalHome(home string) bool {
	config, err := os.UserConfigDir()
	if err != nil {
		return false
	}
	current, err := os.Stat(home)
	if err != nil || !current.IsDir() {
		return false
	}
	canonical, err := os.Stat(filepath.Join(config, "Gopurr"))
	return err == nil && canonical.IsDir() && os.SameFile(current, canonical)
}
