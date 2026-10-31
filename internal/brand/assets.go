// Package brand contains Gopurr's curled cat / G identity. Runtime assets are
// embedded so the standalone executable never needs files beside it.
package brand

import _ "embed"

// LogoSVG is the flat companion to the rendered application icon master.
//
//go:embed logo.svg
var LogoSVG []byte

// IconPNG is the application icon, used by the Dock and taskbar.
//
//go:embed icon.png
var IconPNG []byte

// SidebarPNG is a small derivative of the rendered master, decoded once by the UI.
//
//go:embed sidebar.png
var SidebarPNG []byte

// TrayPNG is the flat sleeping-cat silhouette with transparent cutouts.
// macOS (and Windows template mode) tints this black ink for the menu bar.
//
//go:embed tray.png
var TrayPNG []byte

// TrayWindowsLightPNG is the Windows notification-area icon for a light
// taskbar: Graphite disc with an ivory cat. Not a template.
//
//go:embed tray-windows-light.png
var TrayWindowsLightPNG []byte

// TrayWindowsDarkPNG is the Windows notification-area icon for a dark
// taskbar: ivory disc with a Graphite cat. Not a template.
//
//go:embed tray-windows-dark.png
var TrayWindowsDarkPNG []byte
