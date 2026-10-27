package app

import (
	"github.com/egoist/mygo/ui"

	"gopurr/internal/brand"
)

var brandLogo = func() *ui.Bitmap {
	bitmap, err := ui.DecodeBitmap(brand.SidebarPNG)
	if err != nil {
		panic("invalid embedded Gopurr logo: " + err.Error())
	}
	return bitmap
}()
