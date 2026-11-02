package service

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
)

func ForDataDir(dir string) bool {
	raw, err := os.ReadFile(PlistPath())
	if err != nil {
		return false
	}
	var path strings.Builder
	_ = xml.EscapeText(&path, []byte(filepath.Join(dir, "runtime.yaml")))
	return strings.Contains(string(raw), "<string>"+path.String()+"</string>")
}
