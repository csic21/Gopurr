//go:build windows

package main

import (
	"log"
	"os"
	"sync"

	"golang.org/x/sys/windows"
)

const (
	mygoAppWindowClass = "MyGoApp"
	// internal/windows/win32.go: wmAppTray = wmApp + 3, wmApp = 0x8000.
	mygoTrayMessage = 0x8000 + 3
	wmLButtonDblClk = 0x0203
	traySubclassID  = 1
)

var (
	comctl32             = windows.NewLazySystemDLL("comctl32.dll")
	setWindowSubclass    = comctl32.NewProc("SetWindowSubclass")
	defSubclassProc      = comctl32.NewProc("DefSubclassProc")
	traySubclassOnce     sync.Once
	traySubclassCallback uintptr
	trayOpenWindow       func()
)

// installWindowsTrayDoubleClick opens the window on a notification-area
// double click. mygo only forwards WM_LBUTTONUP, so a double click — which
// Windows delivers as WM_LBUTTONDBLCLK and may not also deliver the button-up
// the app is listening for — never reached the show-window action.
func installWindowsTrayDoubleClick(openWindow func()) {
	trayOpenWindow = openWindow
	traySubclassOnce.Do(func() {
		traySubclassCallback = windows.NewCallback(func(hwnd uintptr, message uint32, wParam, lParam, _, _ uintptr) uintptr {
			if message == mygoTrayMessage && uint16(lParam) == wmLButtonDblClk {
				if open := trayOpenWindow; open != nil {
					open()
				}
			}
			result, _, _ := defSubclassProc.Call(hwnd, uintptr(message), wParam, lParam)
			return result
		})
	})
	hwnd := findMyGoAppWindow()
	if hwnd == 0 {
		log.Printf("tray double click: mygo application window not found")
		return
	}
	if ok, _, err := setWindowSubclass.Call(hwnd, traySubclassCallback, traySubclassID, 0); ok == 0 {
		log.Printf("tray double click: SetWindowSubclass failed: %v", err)
	}
}

func findMyGoAppWindow() uintptr {
	var found uintptr
	callback := windows.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var name [64]uint16
		n, err := windows.GetClassName(windows.HWND(hwnd), &name[0], int32(len(name)))
		if err != nil || windows.UTF16ToString(name[:n]) != mygoAppWindowClass {
			return 1
		}
		var pid uint32
		if _, err := windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid); err != nil {
			return 1
		}
		if pid == uint32(os.Getpid()) {
			found = hwnd
			return 0
		}
		return 1
	})
	_ = windows.EnumWindows(callback, nil)
	return found
}
