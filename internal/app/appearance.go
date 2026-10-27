package app

const (
	AppearanceSystem = "system"
	AppearanceLight  = "light"
	AppearanceDark   = "dark"
)

var appearances = []string{AppearanceSystem, AppearanceLight, AppearanceDark}

func validAppearance(value string) bool {
	return value == AppearanceSystem || value == AppearanceLight || value == AppearanceDark
}

func appearanceIndex(value string) int {
	for i, appearance := range appearances {
		if value == appearance {
			return i
		}
	}
	return 0
}

// systemDark comes from the frame's native appearance, which mygo refreshes
// when the desktop changes. Explicit choices also work in headless fixtures.
func appearanceDark(value string, systemDark bool) bool {
	switch value {
	case AppearanceLight:
		return false
	case AppearanceDark:
		return true
	default:
		return systemDark
	}
}
