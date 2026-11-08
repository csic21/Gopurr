package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var user32 = windows.NewLazySystemDLL("user32.dll")
var gdi32 = windows.NewLazySystemDLL("gdi32.dll")

func hostWindow(pid uint32) uintptr {
	var result uintptr
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var windowPID uint32
		_, _ = windows.GetWindowThreadProcessId(windows.HWND(hwnd), &windowPID)
		if windowPID == pid {
			var title [128]uint16
			_, _, _ = user32.NewProc("GetWindowTextW").Call(hwnd, uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)))
			if windows.UTF16ToString(title[:]) == "Gopurr" {
				result = hwnd
				return 0
			}
		}
		return 1
	})
	_ = windows.EnumWindows(callback, nil)
	return result
}
func windowsHostVisible(pid uint32) bool {
	hwnd := hostWindow(pid)
	if hwnd == 0 {
		return false
	}
	visible, _, _ := user32.NewProc("IsWindowVisible").Call(hwnd)
	return visible != 0
}

type windowRect struct{ Left, Top, Right, Bottom int32 }

func resizeHost(t *testing.T, hwnd uintptr, width, height int32) {
	t.Helper()
	dpi, _, _ := user32.NewProc("GetDpiForWindow").Call(hwnd)
	if dpi == 0 {
		dpi = 96
	}
	rect := windowRect{Right: width * int32(dpi) / 96, Bottom: height * int32(dpi) / 96}
	style, _, _ := user32.NewProc("GetWindowLongW").Call(hwnd, ^uintptr(15))
	exstyle, _, _ := user32.NewProc("GetWindowLongW").Call(hwnd, ^uintptr(19))
	ok, _, err := user32.NewProc("AdjustWindowRectExForDpi").Call(uintptr(unsafe.Pointer(&rect)), style, 0, exstyle, dpi)
	if ok == 0 {
		t.Fatal(err)
	}
	ok, _, err = user32.NewProc("SetWindowPos").Call(hwnd, 0, 0, 0, uintptr(rect.Right-rect.Left), uintptr(rect.Bottom-rect.Top), 0x0002|0x0004|0x0010)
	if ok == 0 {
		t.Fatal(err)
	}
}
func captureHost(t *testing.T, hwnd uintptr, path string) {
	t.Helper()
	var rect windowRect
	ok, _, err := user32.NewProc("GetClientRect").Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	if ok == 0 {
		t.Fatal(err)
	}
	width, height := int(rect.Right), int(rect.Bottom)
	if width < 840 || height < 560 {
		t.Fatalf("undersized actual native window: %dx%d", width, height)
	}
	dc, _, _ := user32.NewProc("GetDC").Call(hwnd)
	if dc == 0 {
		t.Fatal("window DC unavailable")
	}
	defer user32.NewProc("ReleaseDC").Call(hwnd, dc)
	memory, _, _ := gdi32.NewProc("CreateCompatibleDC").Call(dc)
	if memory == 0 {
		t.Fatal("memory DC unavailable")
	}
	defer gdi32.NewProc("DeleteDC").Call(memory)
	type bitmapInfo struct {
		Size                        uint32
		Width, Height               int32
		Planes, BitCount            uint16
		Compression, SizeImage      uint32
		XPels, YPels                int32
		ColorsUsed, ColorsImportant uint32
	}
	info := bitmapInfo{Size: 40, Width: int32(width), Height: -int32(height), Planes: 1, BitCount: 32}
	var pixels unsafe.Pointer
	bitmap, _, _ := gdi32.NewProc("CreateDIBSection").Call(dc, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&pixels)), 0, 0)
	if bitmap == 0 || pixels == nil {
		t.Fatal("capture bitmap unavailable")
	}
	defer gdi32.NewProc("DeleteObject").Call(bitmap)
	original, _, _ := gdi32.NewProc("SelectObject").Call(memory, bitmap)
	defer gdi32.NewProc("SelectObject").Call(memory, original)
	ok, _, err = user32.NewProc("PrintWindow").Call(hwnd, memory, 3)
	if ok == 0 {
		t.Fatal("PrintWindow failed", err)
	}
	raw := unsafe.Slice((*byte)(pixels), width*height*4)
	frame := image.NewRGBA(image.Rect(0, 0, width, height))
	colors := map[color.RGBA]bool{}
	for i := 0; i < len(raw); i += 4 {
		frame.Pix[i], frame.Pix[i+1], frame.Pix[i+2], frame.Pix[i+3] = raw[i+2], raw[i+1], raw[i], 255
		if i%104 == 0 {
			colors[color.RGBA{raw[i+2], raw[i+1], raw[i], 255}] = true
		}
	}
	if len(colors) < 5 {
		t.Fatalf("actual NSH host looks blank (%d colors)", len(colors))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := png.Encode(file, frame); err != nil {
		t.Fatal(err)
	}
}
func assertWindowsPage(t *testing.T, hwnd uintptr, page, repo string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(repo, "scripts/assert-native-windows-page.ps1"))
	cmd.Env = append(os.Environ(), fmt.Sprintf("GOPURR_ACCEPTANCE_HWND=%d", hwnd), "GOPURR_ACCEPTANCE_PAGE="+page)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native UI navigation: %s %v", output, err)
	}
}

func verifyWindowsAutoLaunch(t *testing.T, s windowsSession, frontend string) {
	t.Helper()
	key, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	if _, _, err := key.GetStringValue("Gopurr"); err != registry.ErrNotExist {
		t.Fatal("autostart acceptance requires a disposable user without an existing Gopurr login item")
	}
	defer func() { _ = key.DeleteValue("Gopurr") }()
	windowsMust(t, s, "desktop.prefs.save", map[string]any{"prefs": map[string]any{"autoLaunch": true}})
	value, _, err := key.GetStringValue("Gopurr")
	if err != nil || value != `"`+frontend+`" --autostart` {
		t.Fatalf("login entry did not select the Rust frontend: %q %v", value, err)
	}
	windowsMust(t, s, "desktop.prefs.save", map[string]any{"prefs": map[string]any{"autoLaunch": false}})
	if _, _, err := key.GetStringValue("Gopurr"); err != registry.ErrNotExist {
		t.Fatalf("login entry was not removed: %v", err)
	}
}

func verifyWindowsHostUI(t *testing.T, s windowsSession, repo string) {
	t.Helper()
	waitWindows(t, 20*time.Second, func() bool { return windowsHostVisible(s.FrontendPID) })
	hwnd := hostWindow(s.FrontendPID)
	for _, size := range [][2]int32{{1060, 720}, {840, 560}} {
		resizeHost(t, hwnd, size[0], size[1])
		for _, theme := range []string{"light", "dark"} {
			windowsMust(t, s, "desktop.prefs.save", map[string]any{"prefs": map[string]any{"appearance": theme}})
			for _, page := range []string{"overview", "subs", "proxies", "conns", "mesh", "rules", "logs", "settings"} {
				windowsMust(t, s, "desktop.page.set", map[string]any{"page": page})
				windowsMust(t, s, "host.activate", nil)
				time.Sleep(900 * time.Millisecond)
				assertWindowsPage(t, hwnd, page, repo)
				captureHost(t, hwnd, filepath.Join(repo, "native/ui/screenshots", fmt.Sprintf("windows-host-%s-%s-%dx%d.png", page, theme, size[0], size[1])))
			}
		}
	}
	windowsMust(t, s, "desktop.prefs.save", map[string]any{"prefs": map[string]any{"appearance": "dark"}})
	// First-close confirmation stays in the real UI. The subsequent acknowledged
	// close must hide the NSH window while leaving the same Go backend alive.
	_, _, _ = user32.NewProc("SendMessageW").Call(hwnd, 0x0010, 0, 0)
	time.Sleep(time.Second)
	if !windowsHostVisible(s.FrontendPID) {
		t.Fatal("first close bypassed the background-operation confirmation")
	}
	windowsMust(t, s, "desktop.prefs.save", map[string]any{"prefs": map[string]any{"closeToTrayAcknowledged": true, "lightMode": true, "lightDelaySeconds": 1}})
	time.Sleep(time.Second)
	_, _, _ = user32.NewProc("SendMessageW").Call(hwnd, 0x0010, 0, 0)
	waitWindows(t, 5*time.Second, func() bool { return !windowsHostVisible(s.FrontendPID) })
	time.Sleep(2 * time.Second)
	windowsMust(t, s, "host.ping", nil)
	windowsMust(t, s, "host.activate", nil)
	waitWindows(t, 10*time.Second, func() bool { return windowsHostVisible(s.FrontendPID) })
	t.Log("Actual NSH Win32 host: first-close confirmation, background hide, backend continuity, light-mode reopen and 32 native-window screenshots generated; screenshots require visual review")
}
