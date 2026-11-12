//go:build !darwin && !windows

package shell

import "errors"

func OpenExternal(string) error   { return errors.New("opening files is not supported on this system") }
func ShowItem(string) error       { return errors.New("revealing files is not supported on this system") }
func WriteClipboard(string) error { return errors.New("clipboard is not supported on this system") }
func ChooseFile(bool, string, []string) (string, error) {
	return "", errors.New("file dialogs are not supported on this system")
}
