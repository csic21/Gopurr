//go:build ignore

// Derive Android layers from the shared rendered master and menu-bar vector.
package main

import (
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func save(path string, img image.Image) {
	must(os.MkdirAll(filepath.Dir(path), 0755))
	f, err := os.Create(path)
	must(err)
	must(png.Encode(f, img))
	must(f.Close())
}
func write(path, text string) {
	must(os.MkdirAll(filepath.Dir(path), 0755))
	must(os.WriteFile(path, []byte(text), 0644))
}
func main() {
	f, err := os.Open("internal/brand/icon-master.png")
	must(err)
	master, err := png.Decode(f)
	must(err)
	must(f.Close())
	silhouette := image.NewNRGBA(master.Bounds())
	bounds := image.Rectangle{}
	for y := 0; y < master.Bounds().Dy(); y++ {
		for x := 0; x < master.Bounds().Dx(); x++ {
			c := color.NRGBAModel.Convert(master.At(x, y)).(color.NRGBA)
			alpha := uint8((255 - (int(c.R)+int(c.G)+int(c.B))/3) * int(c.A) / 255)
			silhouette.SetNRGBA(x, y, color.NRGBA{A: alpha})
			if alpha > 8 {
				r := image.Rect(x, y, x+1, y+1)
				if bounds.Empty() {
					bounds = r
				} else {
					bounds = bounds.Union(r)
				}
			}
		}
	}
	if bounds.Empty() {
		panic("shared master has no cat silhouette")
	}
	cx, cy := float64(bounds.Min.X+bounds.Max.X)/2, float64(bounds.Min.Y+bounds.Max.Y)/2
	radius := 0.0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if silhouette.NRGBAAt(x, y).A > 8 {
				radius = math.Max(radius, math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy))
			}
		}
	}
	// All visible cat pixels fit inside the 66dp safe circle on a 108dp layer.
	scale := 130.0 / radius
	w, h := int(math.Round(float64(bounds.Dx())*scale)), int(math.Round(float64(bounds.Dy())*scale))
	foreground := image.NewNRGBA(image.Rect(0, 0, 432, 432))
	target := image.Rect((432-w)/2, (432-h)/2, (432-w)/2+w, (432-h)/2+h)
	draw.CatmullRom.Scale(foreground, target, silhouette, bounds, draw.Src, nil)
	root := "native/android/res"
	save(root+"/drawable-nodpi/app_foreground.png", foreground)
	adaptive := `<?xml version="1.0" encoding="utf-8"?>
<adaptive-icon xmlns:android="http://schemas.android.com/apk/res/android">
    <background android:drawable="@color/app_icon_background" />
    <foreground android:drawable="@drawable/app_foreground" />
%s</adaptive-icon>
`
	write(root+"/mipmap-anydpi-v26/app_icon.xml", fmt.Sprintf(adaptive, ""))
	write(root+"/mipmap-anydpi-v33/app_icon.xml", fmt.Sprintf(adaptive, "    <monochrome android:drawable=\"@drawable/app_foreground\" />\n"))
	write(root+"/values/icon_colors.xml", "<resources><color name=\"app_icon_background\">#ffffff</color></resources>\n")
	raw, err := os.ReadFile("internal/brand/tray.svg")
	must(err)
	var svg struct {
		ViewBox string `xml:"viewBox,attr"`
		Paths   []struct {
			Data string `xml:"d,attr"`
		} `xml:"path"`
	}
	must(xml.Unmarshal(raw, &svg))
	var x, y, vw, vh float64
	_, err = fmt.Sscan(svg.ViewBox, &x, &y, &vw, &vh)
	must(err)
	var paths strings.Builder
	for _, p := range svg.Paths {
		fmt.Fprintf(&paths, "        <path android:fillColor=\"#ffffffff\" android:fillType=\"evenOdd\" android:pathData=\"%s\" />\n", strings.Join(strings.Fields(p.Data), " "))
	}
	write(root+"/drawable/notification_icon.xml", fmt.Sprintf(`<vector xmlns:android="http://schemas.android.com/apk/res/android" android:width="24dp" android:height="24dp" android:viewportWidth="%g" android:viewportHeight="%g">
    <group android:translateX="%g" android:translateY="%g">
%s    </group>
</vector>
`, vw, vh, -x, -y, paths.String()))
	preview := image.NewNRGBA(image.Rect(0, 0, 600, 260))
	draw.Draw(preview, preview.Bounds(), image.NewUniform(color.NRGBA{R: 235, G: 237, B: 240, A: 255}), image.Point{}, draw.Src)
	for col, label := range []string{"Legacy bitmap source", "Adaptive circle", "Adaptive rounded"} {
		d := font.Drawer{Dst: preview, Src: image.NewUniform(color.Black), Face: basicfont.Face7x13, Dot: fixed.P(col*200+12, 22)}
		d.DrawString(label)
		for row, size := range []int{64, 96} {
			left, top := col*200+(200-size)/2, 40+row*100
			icon := image.NewNRGBA(image.Rect(0, 0, size, size))
			draw.Draw(icon, icon.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
			if col == 0 {
				draw.CatmullRom.Scale(icon, icon.Bounds().Inset(size/8), master, master.Bounds(), draw.Over, nil)
			} else {
				draw.CatmullRom.Scale(icon, icon.Bounds(), foreground, image.Rect(72, 72, 360, 360), draw.Over, nil)
			}
			for iy := 0; iy < size; iy++ {
				for ix := 0; ix < size; ix++ {
					dx, dy := math.Abs(float64(ix)+0.5-float64(size)/2), math.Abs(float64(iy)+0.5-float64(size)/2)
					inside := true
					if col == 1 {
						inside = dx*dx+dy*dy < float64(size*size)/4
					}
					if col == 2 {
						r := float64(size) * 0.23
						qx, qy := math.Max(0, dx-float64(size)/2+r), math.Max(0, dy-float64(size)/2+r)
						inside = qx*qx+qy*qy < r*r
					}
					if inside {
						preview.Set(left+ix, top+iy, icon.At(ix, iy))
					}
				}
			}
		}
	}
	save("native/android/icon-preview.png", preview)
	fmt.Printf("Android adaptive cat: %dx%d in a 432px layer; source bounds %v\n", w, h, bounds)
}
