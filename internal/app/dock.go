package app

const (
	DockHidden       = "hide"
	DockFollowWindow = "follow-window"
	DockShown        = "show"
)

var dockVisibilities = []string{DockHidden, DockFollowWindow, DockShown}

func validDockVisibility(value string) bool {
	return value == DockHidden || value == DockFollowWindow || value == DockShown
}

func dockVisibilityIndex(value string) int {
	for i, visibility := range dockVisibilities {
		if value == visibility {
			return i
		}
	}
	return 2
}

// DockVisible keeps the Dock preference independent of light mode: releasing
// the hidden window must not remove the user's way to reopen it.
func (p Prefs) DockVisible(windowVisible bool) bool {
	switch p.DockVisibility {
	case DockHidden:
		return false
	case DockFollowWindow:
		return windowVisible
	default:
		return true
	}
}

func dockVisibilityDescription(value string) string {
	switch value {
	case DockHidden:
		return "从菜单栏「显示窗口」打开界面"
	case DockFollowWindow:
		return "打开窗口时显示，关闭窗口时隐藏"
	default:
		return "关窗后仍显示，点击图标重新打开窗口"
	}
}
