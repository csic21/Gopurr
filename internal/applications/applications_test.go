package applications

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func testBundle(t *testing.T, path, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, "Contents", "MacOS"), 0o755); err != nil {
		t.Fatal(err)
	}
	plist := `<?xml version="1.0"?><plist version="1.0"><dict><key>CFBundleDisplayName</key><string>` + name + `</string><key>CFBundleExecutable</key><string>Main</string><key>Nested</key><dict><key>CFBundleExecutable</key><string>Wrong</string></dict></dict></plist>`
	if err := os.WriteFile(filepath.Join(path, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "Contents", "MacOS", "Main"), []byte("fixture, never executed"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryKeepsBundleBoundaries(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "Suite", "Browser.app")
	testBundle(t, first, "Browser &amp; Work")
	testBundle(t, filepath.Join(first, "Contents", "Frameworks", "Browser Helper.app"), "Browser Helper")
	testBundle(t, filepath.Join(root, "Other.app"), "Other")
	if err := os.Symlink(first, filepath.Join(root, "Alias.app")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "Broken.app"), 0o755); err != nil {
		t.Fatal(err)
	}
	apps, err := scanBundles(context.Background(), []string{root, filepath.Join(root, "Missing")})
	if err != nil || len(apps) != 2 {
		t.Fatalf("discovery = %+v, %v", apps, err)
	}
	wantPath, _ := filepath.EvalSymlinks(first)
	if apps[0].Name != "Browser & Work" || apps[0].Path != wantPath || apps[0].Executable != filepath.Join(wantPath, "Contents", "MacOS", "Main") {
		t.Fatalf("metadata or symlink deduplication failed: %+v", apps[0])
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := scanBundles(ctx, []string{root}); err == nil {
		t.Fatal("cancelled discovery succeeded")
	}
}

func TestInspectBinaryPlistAndExecutable(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Apple binary plist conversion")
	}
	path := filepath.Join(t.TempDir(), "应用, [Test].app")
	testBundle(t, path, "应用")
	if data, err := exec.Command("/usr/bin/plutil", "-convert", "binary1", filepath.Join(path, "Contents", "Info.plist")).CombinedOutput(); err != nil {
		t.Fatalf("fixture plist: %s, %v", data, err)
	}
	app, err := Inspect(context.Background(), path)
	if err != nil || app.Name != "应用" || !app.IsBundle() {
		t.Fatalf("binary bundle = %+v, %v", app, err)
	}
	single, err := Inspect(context.Background(), app.Executable)
	if err != nil || single.IsBundle() || single.Path != app.Executable {
		t.Fatalf("single helper selection = %+v, %v", single, err)
	}
	if _, err := Inspect(context.Background(), filepath.Join(path, "Contents", "Info.plist")); err == nil {
		t.Fatal("accepted a non-executable file")
	}
}
