//go:build !windows

package mesh

import "errors"

func LaunchSSH(Device, Shortcut) error {
	return errors.New("此平台通过系统 SSH 链接打开客户端")
}
