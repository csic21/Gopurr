package service

import "path/filepath"

func windowsServiceLogPath() string {
	path, err := serviceBinary()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(path), "core.log")
}
