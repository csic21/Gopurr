package nativehost

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopurr/internal/securefs"
)

const DesktopMigration = "migration-nsh-v1"

// The release deliberately keeps the old data location and schema. Moving the
// directory would break absolute local-profile paths, kernel/provider caches,
// recovery journals and mesh identity. Before the first NSH model opens it,
// preserve a private, atomic snapshot of durable state. Caller holds LockHome
// and the legacy single-instance lock; runtime sockets/logs are not migrated.
func PrepareDesktopMigration(home string) error {
	if !filepath.IsAbs(home) {
		return errors.New("absolute desktop home required")
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return err
	}
	defer root.Close()
	if info, err := root.Lstat(DesktopMigration); err == nil {
		if !info.IsDir() {
			return errors.New("migration snapshot is not a directory")
		}
		b, err := root.ReadFile(filepath.Join(DesktopMigration, "manifest.json"))
		var m migrationManifest
		if err != nil {
			return err
		}
		if json.Unmarshal(b, &m) != nil || m.Schema != 1 || (m.Home != home && !sameDirectory(m.Home, home)) || m.Files == nil {
			return errors.New("invalid desktop migration snapshot")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	stage, err := os.MkdirTemp(home, ".migration-nsh-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := securefs.PrivateDir(stage); err != nil {
		return err
	}
	m := migrationManifest{Schema: 1, Home: home, Created: time.Now().UTC(), Files: map[string]string{}}
	// These contain subscriptions and credentials, overrides (including the
	// template library), preferences, remote access and mesh identity. Proxy
	// recovery records stay at their original paths and are also backed up.
	for _, name := range []string{"prefs.json", "overrides.json", "profiles.json", "local.yaml", "remote.json", "mesh.json", "sysproxy.json", "sysproxy-recovery.json", "profiles", "tailnet"} {
		info, err := root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("migration refuses linked data: %s", name)
		}
		err = fs.WalkDir(root.FS(), name, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return os.MkdirAll(filepath.Join(stage, path), 0700)
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("migration refuses nonregular data: %s", path)
			}
			b, err := root.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.HasSuffix(path, ".json") && !json.Valid(b) {
				return fmt.Errorf("invalid JSON in desktop data: %s", path)
			}
			if err := securefs.WriteFile(filepath.Join(stage, path), b, 0600); err != nil {
				return err
			}
			sum := sha256.Sum256(b)
			m.Files[filepath.ToSlash(path)] = hex.EncodeToString(sum[:])
			return nil
		})
		if err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err = securefs.WriteFile(filepath.Join(stage, "manifest.json"), append(b, '\n'), 0600); err != nil {
		return err
	}
	return os.Rename(stage, filepath.Join(home, DesktopMigration))
}

type migrationManifest struct {
	Schema  int               `json:"schema"`
	Home    string            `json:"home"`
	Created time.Time         `json:"created"`
	Files   map[string]string `json:"files"`
}
