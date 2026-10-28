// Package applications discovers local programs without launching them.
package applications

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Application keeps the bundle and main executable separate: helpers belong to
// the bundle, never to every process sharing the main executable's name.
type Application struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Executable string `json:"executable"`
}

func (a Application) IsBundle() bool {
	return strings.HasPrefix(a.Path, "/") && strings.HasSuffix(strings.ToLower(a.Path), ".app")
}

// Discover reads the usual application folders. A user can select programs in
// other locations with Inspect. Missing optional folders are not errors.
func Discover(ctx context.Context) ([]Application, error) {
	if runtime.GOOS == "windows" {
		return discoverWindows(ctx)
	}
	if runtime.GOOS != "darwin" {
		return nil, nil
	}
	roots := []string{"/Applications", "/System/Applications", "/System/Library/CoreServices/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, filepath.Join(home, "Applications"))
	}
	return scanBundles(ctx, roots)
}

func scanBundles(ctx context.Context, roots []string) ([]Application, error) {
	var apps []Application
	var failures []error
	seen := map[string]bool{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				if !errors.Is(walkErr, fs.ErrNotExist) {
					failures = append(failures, fmt.Errorf("读取 %s：%w", path, walkErr))
				}
				return nil
			}
			if strings.HasSuffix(strings.ToLower(entry.Name()), ".app") {
				app, err := Inspect(ctx, path)
				if err == nil && !seen[app.Path] {
					seen[app.Path] = true
					apps = append(apps, app)
				}
				// Never present nested Helper.app bundles as independent apps.
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if path != root && entry.IsDir() {
				rel, _ := filepath.Rel(root, path)
				if strings.HasPrefix(entry.Name(), ".") || strings.Count(rel, string(filepath.Separator)) >= 5 {
					return filepath.SkipDir
				}
			}
			return nil
		})
		if err != nil {
			failures = append(failures, err)
			break
		}
	}
	sortApplications(apps)
	return apps, errors.Join(failures...)
}

func sortApplications(apps []Application) {
	sort.Slice(apps, func(i, j int) bool {
		left, right := strings.ToLower(apps[i].Name), strings.ToLower(apps[j].Name)
		if left == right {
			return apps[i].Path < apps[j].Path
		}
		return left < right
	})
}

// Inspect accepts an application bundle or a single executable. It resolves
// aliases implemented as symlinks so rules use the paths reported by the OS.
func Inspect(ctx context.Context, path string) (Application, error) {
	if err := ctx.Err(); err != nil {
		return Application{}, err
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return Application{}, err
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return Application{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return Application{}, err
	}
	if info.IsDir() && strings.HasSuffix(strings.ToLower(path), ".app") {
		return inspectBundle(ctx, path)
	}
	if !info.Mode().IsRegular() || (runtime.GOOS == "windows" && !strings.EqualFold(filepath.Ext(path), ".exe")) ||
		(runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0) {
		return Application{}, errors.New("请选择 .app 应用或可执行程序")
	}
	return Application{Name: filepath.Base(path), Path: path, Executable: path}, nil
}

func inspectBundle(ctx context.Context, path string) (Application, error) {
	plistPath := filepath.Join(path, "Contents", "Info.plist")
	data, err := os.ReadFile(plistPath)
	if err != nil {
		return Application{}, fmt.Errorf("无法读取应用信息：%w", err)
	}
	values := map[string]string{}
	if strings.HasPrefix(string(data), "bplist") {
		// plutil supports Apple's binary plist format; arguments never pass
		// through a shell. Discovery itself runs away from the UI thread.
		data, err = exec.CommandContext(ctx, "/usr/bin/plutil", "-convert", "json", "-o", "-", "--", plistPath).Output()
		if err != nil {
			return Application{}, fmt.Errorf("无法解析应用信息：%w", err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			return Application{}, err
		}
		for key, value := range doc {
			values[key], _ = value.(string)
		}
	} else {
		var doc struct {
			Dict struct {
				Items []struct {
					XMLName xml.Name
					Value   string `xml:",chardata"`
				} `xml:",any"`
			} `xml:"dict"`
		}
		if err := xml.Unmarshal(data, &doc); err != nil {
			return Application{}, err
		}
		for i := 0; i+1 < len(doc.Dict.Items); i++ {
			if doc.Dict.Items[i].XMLName.Local == "key" && doc.Dict.Items[i+1].XMLName.Local == "string" {
				values[doc.Dict.Items[i].Value] = doc.Dict.Items[i+1].Value
			}
		}
	}
	executable := values["CFBundleExecutable"]
	if executable == "" || executable == "." || executable == ".." || strings.ContainsAny(executable, "/\\\r\n") {
		return Application{}, errors.New("应用缺少有效的主程序")
	}
	exePath := filepath.Join(path, "Contents", "MacOS", executable)
	info, err := os.Stat(exePath)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return Application{}, errors.New("找不到应用的可执行主程序")
	}
	if resolved, err := filepath.EvalSymlinks(exePath); err == nil {
		exePath = resolved
	}
	name := values["CFBundleDisplayName"]
	if name == "" {
		name = values["CFBundleName"]
	}
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return Application{Name: name, Path: path, Executable: exePath}, nil
}

func discoverWindows(ctx context.Context) ([]Application, error) {
	// Start-menu shortcuts provide installed program names and exact targets,
	// without guessing that every .exe under Program Files is an application.
	const script = `$ErrorActionPreference = 'Stop'
$OutputEncoding = [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$wsh = New-Object -ComObject WScript.Shell
$roots = @((Join-Path $env:ProgramData 'Microsoft\Windows\Start Menu\Programs'), (Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs'))
$apps = @(foreach ($root in $roots) {
  if (Test-Path -LiteralPath $root) {
    Get-ChildItem -LiteralPath $root -Filter *.lnk -Recurse | ForEach-Object {
      $target = $wsh.CreateShortcut($_.FullName).TargetPath
      if ($target -and $target.EndsWith('.exe', [StringComparison]::OrdinalIgnoreCase) -and (Test-Path -LiteralPath $target)) {
        @{ name = $_.BaseName; path = $target; executable = $target }
      }
    }
  }
})
ConvertTo-Json -InputObject $apps -Compress`
	data, err := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		return nil, fmt.Errorf("读取开始菜单失败：%w", err)
	}
	var found []Application
	if err := json.Unmarshal(data, &found); err != nil {
		return nil, err
	}
	var apps []Application
	seen := map[string]bool{}
	for _, app := range found {
		key := strings.ToLower(app.Path)
		if !seen[key] {
			seen[key] = true
			apps = append(apps, app)
		}
	}
	sortApplications(apps)
	return apps, nil
}
