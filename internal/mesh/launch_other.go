//go:build !windows

package mesh

import "gopurr/internal/localize"

import "errors"

func LaunchSSH(Device, Shortcut) error {
	return errors.New(localize.Text("此平台通过系统 SSH 链接打开客户端"))
}
