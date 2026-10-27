//go:build ignore

// Regenerate runtime sizes from the rendered master and menu-bar vector:
// make icons
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/draw"
)

func main() {
	renderMaster := flag.Bool("render-master", false, "render icon-master.png from the flat logo.svg before deriving runtime sizes")
	flag.Parse()
	if err := generate(*renderMaster); err != nil {
		log.Fatal(err)
	}
}

func generate(renderMaster bool) error {
	const dir = "internal/brand"
	if renderMaster {
		data, err := os.ReadFile(filepath.Join(dir, "logo.svg"))
		if err != nil {
			return err
		}
		logo, err := ui.ParseSVG(data)
		if err != nil {
			return err
		}
		if err := writePNG(filepath.Join(dir, "icon-master.png"), renderVector(logo, 1024)); err != nil {
			return err
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "icon-master.png"))
	if err != nil {
		return err
	}
	master, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return err
	}
	for _, output := range []struct {
		name string
		size int
	}{{"logo.png", 1024}, {"icon.png", 512}, {"sidebar.png", 128}} {
		if err := writePNG(filepath.Join(dir, output.name), resizeIcon(master, output.size)); err != nil {
			return err
		}
	}

	data, err = os.ReadFile(filepath.Join(dir, "tray.svg"))
	if err != nil {
		return err
	}
	template, err := ui.ParseSVG(data)
	if err != nil {
		return err
	}
	// Supersample with the native renderer and preserve the open G counter.
	return writePNG(filepath.Join(dir, "tray.png"), renderVector(template, 32))
}

func resizeIcon(src image.Image, size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	// Leave the same optical margin as other macOS icons; preserve source alpha.
	inset := max(1, size*3/100)
	draw.CatmullRom.Scale(dst, dst.Bounds().Inset(inset), src, src.Bounds(), draw.Src, nil)
	return dst
}

func renderVector(svg *ui.SVG, size int) *image.NRGBA {
	theme := ui.LightTheme()
	theme.Background = ui.Transparent
	rendered := ui.Render(func(c *ui.Context) {
		c.SetTheme(theme)
		ui.Image(c, svg).Size(float32(size), float32(size))
	}, size, size, 2)
	return resize(rendered, size)
}

func resize(src image.Image, size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return dst
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
