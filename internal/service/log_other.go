//go:build !windows

package service

func windowsServiceLogPath() string { return "" }
