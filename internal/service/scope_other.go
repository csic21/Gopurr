//go:build !darwin && !windows

package service

func ForDataDir(string) bool { return false }
