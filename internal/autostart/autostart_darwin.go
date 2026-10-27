package autostart

import (
	"bytes"
	"encoding/xml"
	"errors"
	"os"
	"path/filepath"
)

func launchAgent(executable string) []byte {
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(executable))
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>app.gopurr.login</string>
<key>ProgramArguments</key><array><string>` + escaped.String() + `</string><string>--autostart</string></array>
<key>RunAtLoad</key><true/>
</dict></plist>
`)
}

func set(enabled bool, executable string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, "Library", "LaunchAgents", "app.gopurr.login.plist")
	return writeAgent(path, executable, enabled)
}

func writeAgent(path, executable string, enabled bool) error {
	if !enabled {
		err := os.Remove(path)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".gopurr-login-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(launchAgent(executable)); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
