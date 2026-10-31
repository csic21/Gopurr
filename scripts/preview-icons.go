//go:build ignore

// Produce a reproducible brand and navigation icon review sheet:
// go run scripts/preview-icons.go
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/draw"
	"gopurr/internal/brand"
)

func main() {
	for _, asset := range []struct {
		name string
		data []byte
		size int
	}{{"icon", brand.IconPNG, 512}, {"sidebar", brand.SidebarPNG, 128}, {"tray", brand.TrayPNG, 32},
		{"tray-windows-light", brand.TrayWindowsLightPNG, 32}, {"tray-windows-dark", brand.TrayWindowsDarkPNG, 32}} {
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
		if asset.name != "tray" && asset.name != "tray-windows-light" && asset.name != "tray-windows-dark" {
			continue
		}
		for y := 0; y < asset.size; y++ {
			for x := 0; x < asset.size; x++ {
				r, g, b, a := img.At(x, y).RGBA()
				if a > 0 && (r != g || g != b) {
					log.Fatal(asset.name + ": expected neutral monochrome pixels")
				}
				if asset.name == "tray" && a > 0 && (r != 0 || g != 0 || b != 0) {
					log.Fatal("tray: template must contain only black ink")
				}
			}
		}
		if asset.name != "tray" {
			continue
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
	for _, size := range [][2]int{{1060, 720}, {840, 560}} {
		previewWindows(size[0], size[1])
	}
}

func previewWindows(width, height int) {
	img := ui.Render(func(c *ui.Context) {
		c.SetTheme(ui.LightTheme())
		ui.Column(c).Fill().Padding(24).Gap(20).Background(ui.Hex("#ffffff")).Children(func() {
			ui.Text(c, "Gopurr · Windows 托盘 · C").FontSize(22).FontWeight(600).TextColor(ui.Hex("#000000"))
			ui.Text(c, "圆底与双耳剪影 · 实际运行 PNG · 随任务栏明暗切换").FontSize(12).TextColor(ui.Hex("#555555"))
			ui.Row(c).FillWidth().Grow(1).Gap(20).Children(func() {
				for _, dark := range []bool{false, true} {
					bg, fg, muted, data, label := ui.Hex("#f3f3f3"), ui.Hex("#000000"), ui.Hex("#555555"), brand.TrayWindowsLightPNG, "浅色任务栏"
					if dark {
						bg, fg, muted, data, label = ui.Hex("#202020"), ui.Hex("#ffffff"), ui.Hex("#bfbfbf"), brand.TrayWindowsDarkPNG, "深色任务栏"
					}
					ui.Column(c).Grow(1).FillHeight().Padding(20).Gap(20).Background(bg).Children(func() {
						ui.Text(c, label).FontSize(14).FontWeight(600).TextColor(fg)
						ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
							ui.Image(c, previewBitmap(data, 32, 4)).Size(128, 128)
						})
						ui.Text(c, "实际尺寸 / px").FontSize(12).TextColor(muted)
						ui.Row(c).FillWidth().Justify(ui.SpaceBetween).AlignItems(ui.End).Children(func() {
							for _, size := range []int{16, 20, 24, 32} {
								ui.Column(c).AlignItems(ui.Center).Gap(10).Children(func() {
									ui.Image(c, previewBitmap(data, size, 1)).Size(float32(size), float32(size))
									ui.Text(c, fmt.Sprint(size)).FontSize(11).TextColor(muted)
								})
							}
						})
						ui.Row(c).AlignItems(ui.Center).Gap(16).Children(func() {
							ui.Image(c, previewBitmap(data, 16, 4)).Size(64, 64)
							ui.Text(c, "16 px · 4 倍像素放大").FontSize(11).TextColor(muted)
						})
					})
				}
			})
			ui.Text(c, "资源渲染预览；Windows 通知区域的实际显示仍需实机验收。").FontSize(11).TextColor(ui.Hex("#555555"))
		})
	}, width, height, 1)
	f, err := os.Create(fmt.Sprintf("docs/tray-windows-preview-%dx%d.png", width, height))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
}

func previewBitmap(data []byte, size, zoom int) *ui.Bitmap {
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		log.Fatal(err)
	}
	small := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(small, small.Bounds(), src, src.Bounds(), draw.Src, nil)
	large := image.NewNRGBA(image.Rect(0, 0, size*zoom, size*zoom))
	draw.NearestNeighbor.Scale(large, large.Bounds(), small, small.Bounds(), draw.Src, nil)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, large); err != nil {
		log.Fatal(err)
	}
	bitmap, err := ui.DecodeBitmap(encoded.Bytes())
	if err != nil {
		log.Fatal(err)
	}
	return bitmap
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
