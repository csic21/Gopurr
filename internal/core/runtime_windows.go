package core

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func checkTunRoutes(index int) error {
	var table *windows.MibIpForwardTable2
	if err := windows.GetIpForwardTable2(windows.AF_UNSPEC, &table); err != nil {
		return fmt.Errorf("读取 TUN 路由失败: %w", err)
	}
	defer windows.FreeMibTable(unsafe.Pointer(table))
	for _, row := range table.Rows() {
		if row.InterfaceIndex == uint32(index) && row.Protocol != windows.MIB_IPPROTO_LOCAL && row.Loopback == 0 {
			return nil
		}
	}
	return fmt.Errorf("TUN 自动路由未就绪")
}
