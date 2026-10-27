// Package brand contains Gopurr's curled cat / G identity. Runtime assets are
// embedded so the standalone executable never needs files beside it.
package brand

import _ "embed"

// LogoSVG is the flat companion to the rendered application icon master.
//
//go:embed logo.svg
var LogoSVG []byte

// IconPNG is the full-color application icon, used by the Dock and taskbar.
//
//go:embed icon.png
var IconPNG []byte

// SidebarPNG is a small derivative of the rendered master, decoded once by the UI.
//
//go:embed sidebar.png
var SidebarPNG []byte

// TrayPNG is the flat silhouette with an open G counter. The OS tints this
// template for a light or dark menu bar.
//
//go:embed tray.png
var TrayPNG []byte
