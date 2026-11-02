//go:build !windows

package service

import "errors"

// launchd keeps its registered daemon running; Ensure only waits for readiness.
func EnsureRunning() error { return nil }
func StopRunning() error   { return errors.New("守护进程由系统管理，应用无法直接停止") }
func Restart(string) error { return errors.New("请重新安装系统服务以恢复内核") }
