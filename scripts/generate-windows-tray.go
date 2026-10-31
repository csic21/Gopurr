//go:build ignore

// One-shot: regenerate Windows Graphite tray icons with a tighter crop so the
// cat silhouette fills more of the 32×32 canvas (macOS tray.png unchanged).
package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/draw"
)

func main() {
	const dir = "internal/brand"
	data, err := os.ReadFile(filepath.Join(dir, "tray.svg"))
	if err != nil {
		log.Fatal(err)
	}
	svg, err := ui.ParseSVG(data)
	if err != nil {
		log.Fatal(err)
	}
	const srcSize = 256
	theme := ui.LightTheme()
	theme.Background = ui.Transparent
	rendered := ui.Render(func(c *ui.Context) {
		c.SetTheme(theme)
		ui.Image(c, svg).Size(float32(srcSize), float32(srcSize))
	}, srcSize, srcSize, 2)
	src := image.NewNRGBA(image.Rect(0, 0, srcSize, srcSize))
	draw.CatmullRom.Scale(src, src.Bounds(), rendered, rendered.Bounds(), draw.Src, nil)

	bbox := opaqueBounds(src, 12)
	if bbox.Empty() {
		log.Fatal("no opaque pixels in tray render")
	}
	// Target: nearly fill 32×32 with ~1px optical margin (bigger than previous ~3–4px).
	const out = 32
	const margin = 1
	inner := out - 2*margin
	bw, bh := bbox.Dx(), bbox.Dy()
	scale := float64(inner) / float64(max(bw, bh))
	// Prefer filling height a bit more aggressively for tray readability.
	if float64(bh)*scale < float64(inner)*0.92 {
		scale = float64(inner) * 0.96 / float64(bh)
	}
	nw := int(math.Round(float64(bw) * scale))
	nh := int(math.Round(float64(bh) * scale))
	if nw > inner {
		nw = inner
	}
	if nh > inner {
		nh = inner
	}
	cropped := image.NewNRGBA(image.Rect(0, 0, bw, bh))
	draw.Draw(cropped, cropped.Bounds(), src, bbox.Min, draw.Src)
	scaled := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), cropped, cropped.Bounds(), draw.Src, nil)

	mask := image.NewNRGBA(image.Rect(0, 0, out, out))
	ox := (out - nw) / 2
	oy := (out - nh) / 2
	draw.Draw(mask, image.Rect(ox, oy, ox+nw, oy+nh), scaled, image.Point{}, draw.Src)

	light := colorize(mask, color.NRGBA{R: 31, G: 41, B: 55, A: 255}, color.NRGBA{R: 255, G: 255, B: 255, A: 255}, 1)
	dark := colorize(mask, color.NRGBA{R: 245, G: 245, B: 244, A: 255}, color.NRGBA{R: 15, G: 23, B: 42, A: 255}, 1)
	if err := writePNG(filepath.Join(dir, "tray-windows-light.png"), light); err != nil {
		log.Fatal(err)
	}
	if err := writePNG(filepath.Join(dir, "tray-windows-dark.png"), dark); err != nil {
		log.Fatal(err)
	}
	fb := opaqueBounds(mask, 8)
	fmt.Printf("content fill ~%.0f%% bbox=%v size=%dx%d\n", float64(fb.Dx()*fb.Dy())/float64(out*out)*100, fb, fb.Dx(), fb.Dy())
}

func opaqueBounds(img *image.NRGBA, minA uint8) image.Rectangle {
	b := img.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X-1, b.Min.Y-1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.NRGBAAt(x, y).A >= minA {
				if x < minX {
					minX = x
				}
				if y < minY {
					minY = y
				}
				if x > maxX {
					maxX = x
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if maxX < minX {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX+1, maxY+1)
}

func colorize(mask *image.NRGBA, fill, edge color.NRGBA, radius int) *image.NRGBA {
	b := mask.Bounds()
	out := image.NewNRGBA(b)
	// Edge: any neighbor within radius of an opaque mask pixel.
	opaque := func(x, y int) bool {
		if x < b.Min.X || y < b.Min.Y || x >= b.Max.X || y >= b.Max.Y {
			return false
		}
		return mask.NRGBAAt(x, y).A >= 96
	}
	near := func(x, y int) bool {
		for dy := -radius; dy <= radius; dy++ {
			for dx := -radius; dx <= radius; dx++ {
				if dx*dx+dy*dy > radius*radius {
					continue
				}
				if opaque(x+dx, y+dy) {
					return true
				}
			}
		}
		return false
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			a := mask.NRGBAAt(x, y).A
			if a >= 96 {
				c := fill
				c.A = a
				out.SetNRGBA(x, y, c)
				continue
			}
			if near(x, y) {
				// Soft edge outside the fill.
				ea := uint8(180)
				if a > ea {
					ea = a
				}
				c := edge
				c.A = ea
				out.SetNRGBA(x, y, c)
			}
		}
	}
	return out
}

func writePNG(path string, img image.Image) error {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return err
	}
	fmt.Printf("%s (%d × %d)\n", path, img.Bounds().Dx(), img.Bounds().Dy())
	return nil
}
