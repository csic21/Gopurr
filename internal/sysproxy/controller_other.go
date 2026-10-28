//go:build !darwin

package sysproxy

func NewController() Controller { return directController{} }

func NewControllerAt(string) Controller { return NewController() }

type directController struct{}

func (directController) Enable(port int, _ *State) error { return Enable(port) }
func (directController) Restore(_ int, previous *State) error {
	if previous != nil && previous.Enabled {
		return Restore(*previous)
	}
	return Disable()
}
func (directController) Close() error { return nil }

func HandleCommand([]string) (bool, error) { return false, nil }
