//go:build ignore

// Produce a reproducible brand and navigation icon review sheet:
// go run scripts/preview-icons.go
package main

import (
	"bytes"
	"image/png"
	"log"
	"os"

	"github.com/egoist/mygo/ui"
	"gopurr/internal/brand"
)

func main() {
	for _, asset := range []struct {
		name string
		data []byte
		size int
	}{{"icon", brand.IconPNG, 512}, {"sidebar", brand.SidebarPNG, 128}, {"tray", brand.TrayPNG, 32}} {
		img, err := png.Decode(bytes.NewReader(asset.data))
		if err != nil {
			log.Fatal(err)
		}
		if img.Bounds().Dx() != asset.size || img.Bounds().Dy() != asset.size {
			log.Fatal(asset.name + ": incorrect dimensions")
		}
		_, _, _, corner := img.At(0, 0).RGBA()
		if corner != 0 {
			log.Fatal(asset.name + ": expected a transparent corner")
		}
		if asset.name != "tray" {
			continue
		}
		for y := 0; y < asset.size; y++ {
			for x := 0; x < asset.size; x++ {
				r, g, b, a := img.At(x, y).RGBA()
				if a > 0 && (r != 0 || g != 0 || b != 0) {
					log.Fatal("tray: template must contain only black ink")
				}
			}
		}
		// Sample the separation between the tucked head, body and forepaws.
		_, _, _, counter := img.At(16, 18).RGBA()
		if counter > 5000 {
			log.Fatal("tray: the space beside the tucked head must remain transparent")
		}
	}

	logo, err := ui.DecodeBitmap(brand.IconPNG)
	if err != nil {
		log.Fatal(err)
	}
	trayData, err := os.ReadFile("internal/brand/tray.svg")
	if err != nil {
		log.Fatal(err)
	}
	tray := ui.MustParseSVG(trayData)

	img := ui.Render(func(c *ui.Context) {
		ui.Row(c).Fill().Padding(24).Gap(20).Background(ui.Hex("#e5e7ed")).Children(func() {
			for _, dark := range []bool{true, false} {
				bg, fg, muted, line := ui.Hex("#fafbfe"), ui.Hex("#18243e"), ui.Hex("#626a7c"), ui.Hex("#dce0ea")
				ink := ui.Hex("#000000")
				if dark {
					bg, fg, muted, line = ui.Hex("#17181e"), ui.Hex("#f0f1f5"), ui.Hex("#b0b4c1"), ui.Hex("#30333e")
					ink = ui.Hex("#ffffff")
				}
				ui.Column(c).Grow(1).FillHeight().Padding(24).Radius(16).Background(bg).Gap(14).Children(func() {
					ui.Row(c).AlignItems(ui.Center).Gap(16).Children(func() {
						ui.Image(c, logo).Size(112, 112)
						ui.Column(c).Gap(6).Children(func() {
							ui.Text(c, "Gopurr").FontSize(24).FontWeight(600).TextColor(fg)
							ui.Text(c, "自然蜷睡 · 纯黑与纯白").FontSize(12).TextColor(muted)
						})
					})
					ui.Box(c).FillWidth().Height(1).Background(line)
					ui.Text(c, "应用图标 / 侧栏").FontSize(12).TextColor(muted)
					ui.Row(c).AlignItems(ui.End).Gap(16).Children(func() {
						for _, size := range []float32{64, 32, 24, 16} {
							ui.Column(c).Width(68).AlignItems(ui.Center).Gap(8).Children(func() {
								ui.Image(c, logo).Size(size, size)
								ui.Text(c, sizeLabel(size)).FontSize(11).TextColor(muted)
							})
						}
					})
					ui.Box(c).FillWidth().Height(1).Background(line)
					ui.Text(c, "菜单栏 · 单色轮廓").FontSize(12).TextColor(muted)
					ui.Row(c).AlignItems(ui.End).Gap(16).Children(func() {
						for _, size := range []float32{16, 18, 24, 32} {
							ui.Column(c).Width(68).AlignItems(ui.Center).Gap(10).Children(func() {
								ui.Icon(c, tray).Size(size, size).TextColor(ink)
								ui.Text(c, sizeLabel(size)).FontSize(11).TextColor(muted)
							})
						}
					})
				})
			}
		})
	}, 920, 500, 2)
	f, err := os.Create("docs/logo-preview.png")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
}

func sizeLabel(size float32) string {
	switch size {
	case 16:
		return "16 px"
	case 18:
		return "18 px"
	case 24:
		return "24 px"
	case 32:
		return "32 px"
	case 36:
		return "36 px"
	default:
		return "64 px"
	}
}
