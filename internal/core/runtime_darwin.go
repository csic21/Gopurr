package core

import (
	"fmt"
	"syscall"

	"golang.org/x/net/route"
)

func checkTunRoutes(index int) error {
	rib, err := route.FetchRIB(syscall.AF_UNSPEC, route.RIBTypeRoute, 0)
	if err != nil {
		return fmt.Errorf("读取 TUN 路由失败: %w", err)
	}
	messages, err := route.ParseRIB(route.RIBTypeRoute, rib)
	if err != nil {
		return fmt.Errorf("读取 TUN 路由失败: %w", err)
	}
	for _, message := range messages {
		if r, ok := message.(*route.RouteMessage); ok && r.Index == index && r.Flags&syscall.RTF_UP != 0 && r.Flags&syscall.RTF_HOST == 0 {
			return nil
		}
	}
	return fmt.Errorf("TUN 自动路由未就绪")
}
