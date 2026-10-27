//go:build !darwin && !windows

package autostart

import "fmt"

func set(bool, string) error { return fmt.Errorf("当前平台暂不支持登录自启") }
