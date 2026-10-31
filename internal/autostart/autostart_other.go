//go:build !darwin && !windows

package autostart

import (
	"fmt"

	"gopurr/internal/localize"
)

func set(bool, string) error {
	return fmt.Errorf("%s", localize.Text("当前平台暂不支持登录自启"))
}
