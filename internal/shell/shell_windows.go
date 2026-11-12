//go:build windows

package shell

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

func OpenExternal(raw string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

func ShowItem(path string) error {
	cmd := exec.Command("explorer", "/select,"+path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

func WriteClipboard(text string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-Command", "Set-Clipboard -Value $input")
	cmd.Stdin = strings.NewReader(text)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

func ChooseFile(save bool, name string, extensions []string) (string, error) {
	filter := strings.Join(extensions, ";*.")
	if filter != "" {
		filter = "*." + filter
	} else {
		filter = "*.*"
	}
	kind := "OpenFileDialog"
	extra := "$d.CheckFileExists = $true"
	if save {
		kind = "SaveFileDialog"
		extra = fmt.Sprintf("$d.FileName = %q", name)
	}
	script := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.%s
$d.Filter = "Gopurr|%s"
%s
if ($d.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) { Write-Output $d.FileName }
`, kind, filter, extra)
	cmd := exec.Command("powershell", "-NoProfile", "-STA", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(out)), nil
}
