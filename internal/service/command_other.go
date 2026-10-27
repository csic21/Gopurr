//go:build !windows

package service

func HandleCommand([]string, func([]string) error) (bool, error) { return false, nil }
