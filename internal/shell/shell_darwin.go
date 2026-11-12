//go:build darwin

package shell

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func OpenExternal(raw string) error {
	return exec.Command("/usr/bin/open", raw).Run()
}

func ShowItem(path string) error {
	return exec.Command("/usr/bin/open", "-R", path).Run()
}

func WriteClipboard(text string) error {
	cmd := exec.Command("/usr/bin/pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func ChooseFile(save bool, name string, extensions []string) (string, error) {
	var script string
	if save {
		script = fmt.Sprintf(`POSIX path of (choose file name with prompt "Gopurr" default name %q)`, name)
	} else {
		types := make([]string, len(extensions))
		for i, ext := range extensions {
			types[i] = strconvQuote(ext)
		}
		script = fmt.Sprintf(`POSIX path of (choose file with prompt "Gopurr" of type {%s})`, strings.Join(types, ", "))
	}
	cmd := exec.Command("/usr/bin/osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		// Cancel is a normal empty result.
		return "", nil
	}
	return strings.TrimSpace(string(bytes.TrimSpace(out))), nil
}

func strconvQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, ``) + `"`
}
