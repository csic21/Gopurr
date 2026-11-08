#pragma once
// Catalyst hosts AppKit windows, but UIKit does not expose close-to-tray or
// Dock policy. Keep this small native adapter outside the portable NSH UI.
void GopurrDesktopSetDock(int visible);
int GopurrDesktopInstallWindowHooks(void (^visibility)(int));
void GopurrDesktopShowWindow(void);
void GopurrDesktopHideWindow(void);
void GopurrDesktopLayoutChrome(void);
double GopurrDesktopTitlebarInset(void);

// The foreground Catalyst process owns the menu-bar status item.
int GopurrDesktopInstallTray(const char *imagePath, void (^action)(const char *));
void GopurrDesktopShowTrayMenu(const char *json);
int GopurrDesktopTrayVisible(void);
