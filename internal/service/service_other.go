//go:build !darwin && !windows

package service

import (
	"errors"

	"gopurr/internal/localize"
)

func Available() bool { return false }
func Installed() bool { return false }
func Install(string, string) error {
	return errors.New(localize.Text("当前平台不支持安装 TUN 服务"))
}
func Uninstall(string) error {
	return errors.New(localize.Text("当前平台不支持卸载 TUN 服务"))
}
