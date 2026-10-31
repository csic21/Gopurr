//go:build ignore

// One-shot: regenerate Windows Graphite tray icons from tray.svg.
// Uniform scale + center only (no stretch). Optional filled circle behind
// the cat so the silhouette can sit larger without looking padded.
// macOS tray.png is never touched.
//
//	go run scripts/generate-windows-tray.go
//	go run scripts/generate-windows-tray.go -variant=A   # ivory disc
//	go run scripts/generate-windows-tray.go -variant=B   # soft gray disc
//	go run scripts/generate-windows-tray.go -variant=C   # inverted badge (default)
//	go run scripts/generate-windows-tray.go -variant=none
//	go run scripts/generate-windows-tray.go -preview     # all variants + sheets
package main

import (
	"bytes"
	"flag"
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
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

type pair struct {
	name        string
	label       string
	lightCircle color.NRGBA // A==0 → no circle
	darkCircle  color.NRGBA
	lightCat    color.NRGBA
	darkCat     color.NRGBA
	lightEdge   color.NRGBA
	darkEdge    color.NRGBA
	edgeRadius  int
}

var variants = []pair{
	{
		name: "none", label: "current (no circle)",
		lightCat:   color.NRGBA{R: 31, G: 41, B: 55, A: 255},
		darkCat:    color.NRGBA{R: 245, G: 245, B: 244, A: 255},
		lightEdge:  color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		darkEdge:   color.NRGBA{R: 15, G: 23, B: 42, A: 255},
		edgeRadius: 1,
	},
	{
		// Soft ivory disc + graphite cat / dark slate disc + ivory cat.
		name: "A", label: "A ivory disc + graphite cat",
		lightCircle: color.NRGBA{R: 245, G: 245, B: 244, A: 255}, // stone-100
		darkCircle:  color.NRGBA{R: 31, G: 41, B: 55, A: 255},    // graphite
		lightCat:    color.NRGBA{R: 31, G: 41, B: 55, A: 255},
		darkCat:     color.NRGBA{R: 245, G: 245, B: 244, A: 255},
	},
	{
		// Slightly cooler gray disc for light tray; near-black disc for dark.
		name: "B", label: "B soft-gray disc + graphite cat",
		lightCircle: color.NRGBA{R: 228, G: 228, B: 231, A: 255}, // zinc-200
		darkCircle:  color.NRGBA{R: 15, G: 23, B: 42, A: 255},    // slate-900
		lightCat:    color.NRGBA{R: 31, G: 41, B: 55, A: 255},
		darkCat:     color.NRGBA{R: 250, G: 250, B: 249, A: 255},
	},
	{
		// Inverted badge: disc always contrasts with the taskbar.
		name: "C", label: "C inverted badge (graphite/ivory swap)",
		lightCircle: color.NRGBA{R: 31, G: 41, B: 55, A: 255},    // graphite disc on light tray
		darkCircle:  color.NRGBA{R: 245, G: 245, B: 244, A: 255}, // ivory disc on dark tray
		lightCat:    color.NRGBA{R: 245, G: 245, B: 244, A: 255},
		darkCat:     color.NRGBA{R: 31, G: 41, B: 55, A: 255},
	},
}

type sized struct {
	name string
	p    pair
	lt   *image.NRGBA
	dk   *image.NRGBA
}

func main() {
	variant := flag.String("variant", "C", "pair to write into internal/brand: A|B|C|none")
	preview := flag.Bool("preview", false, "emit all variants + comparison sheets under docs/ and mockups/")
	mockups := flag.String("mockups", "/workspace/gopurr-tray-mockups", "directory for alternate PNGs")
	flag.Parse()

	src, err := renderSource()
	if err != nil {
		log.Fatal(err)
	}
	bbox := opaqueBounds(src, 12)
	if bbox.Empty() {
		log.Fatal("no opaque pixels in tray render")
	}

	chosen := findVariant(*variant)
	if chosen == nil {
		log.Fatalf("unknown -variant=%q (want A, B, C, or none)", *variant)
	}

	light, dark := renderPair(src, bbox, *chosen)
	dir := "internal/brand"
	if err := writePNG(filepath.Join(dir, "tray-windows-light.png"), light); err != nil {
		log.Fatal(err)
	}
	if err := writePNG(filepath.Join(dir, "tray-windows-dark.png"), dark); err != nil {
		log.Fatal(err)
	}
	fb := opaqueBounds(light, 8)
	fmt.Printf("default variant=%s content bbox=%v size=%dx%d\n", chosen.name, fb, fb.Dx(), fb.Dy())

	if !*preview {
		return
	}
	if err := os.MkdirAll(*mockups, 0o755); err != nil {
		log.Fatal(err)
	}
	var all []sized
	for _, v := range variants {
		lt, dk := renderPair(src, bbox, v)
		all = append(all, sized{v.name, v, lt, dk})
		base := filepath.Join(*mockups, "circle-"+v.name)
		_ = writePNG(base+"-light-32.png", lt)
		_ = writePNG(base+"-dark-32.png", dk)
		_ = writePNG(base+"-light-24.png", downNearest(lt, 24))
		_ = writePNG(base+"-dark-24.png", downNearest(dk, 24))
		_ = writePNG(base+"-light-16.png", downNearest(lt, 16))
		_ = writePNG(base+"-dark-16.png", downNearest(dk, 16))
		_ = writePNG(base+"-light-128.png", upNearest(lt, 4))
		_ = writePNG(base+"-dark-128.png", upNearest(dk, 4))
	}
	sheet := buildSheet(all)
	if err := writePNG("docs/tray-windows-circle-preview.png", sheet); err != nil {
		log.Fatal(err)
	}
	if err := writePNG(filepath.Join(*mockups, "windows-tray-circle-compare.png"), sheet); err != nil {
		log.Fatal(err)
	}
	rec := findVariant("C")
	strip := buildTrayStrip(src, bbox, *rec)
	if err := writePNG("docs/tray-windows-circle-sizes.png", strip); err != nil {
		log.Fatal(err)
	}
	if err := writePNG(filepath.Join(*mockups, "windows-tray-circle-recommended.png"), strip); err != nil {
		log.Fatal(err)
	}
	fmt.Println("preview sheets written; recommended default = C (inverted badge)")
}

func findVariant(name string) *pair {
	for i := range variants {
		if variants[i].name == name {
			return &variants[i]
		}
	}
	return nil
}

func renderSource() (*image.NRGBA, error) {
	const dir = "internal/brand"
	data, err := os.ReadFile(filepath.Join(dir, "tray.svg"))
	if err != nil {
		return nil, err
	}
	svg, err := ui.ParseSVG(data)
	if err != nil {
		return nil, err
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
	return src, nil
}

func renderPair(src *image.NRGBA, bbox image.Rectangle, v pair) (light, dark *image.NRGBA) {
	const out = 32
	useCircle := v.lightCircle.A > 0
	var mask *image.NRGBA
	if useCircle {
		mask = placeInCircle(src, bbox, out, 15.0, 2.0)
	} else {
		mask = placeInSquare(src, bbox, out, 2)
	}
	if v.edgeRadius > 0 {
		light = colorize(mask, v.lightCat, v.lightEdge, v.edgeRadius)
		dark = colorize(mask, v.darkCat, v.darkEdge, v.edgeRadius)
	} else {
		light = colorizeSolid(mask, v.lightCat)
		dark = colorizeSolid(mask, v.darkCat)
	}
	if useCircle {
		light = withCircle(light, v.lightCircle, out, 15.0)
		dark = withCircle(dark, v.darkCircle, out, 15.0)
	}
	return light, dark
}

func placeInSquare(src *image.NRGBA, bbox image.Rectangle, out, margin int) *image.NRGBA {
	inner := out - 2*margin
	bw, bh := bbox.Dx(), bbox.Dy()
	scale := math.Min(float64(inner)/float64(bw), float64(inner)/float64(bh))
	nw := int(math.Round(float64(bw) * scale))
	nh := int(math.Round(float64(bh) * scale))
	for (nw > inner || nh > inner) && scale > 0 {
		scale *= 0.99
		nw = int(math.Round(float64(bw) * scale))
		nh = int(math.Round(float64(bh) * scale))
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	return blitScaled(src, bbox, out, nw, nh)
}

func placeInCircle(src *image.NRGBA, bbox image.Rectangle, out int, r, margin float64) *image.NRGBA {
	// Fit into the inscribed square of the inner disc. The curled-cat
	// silhouette does not fill bbox corners, so this reads larger than a
	// strict diagonal fit while still leaving ~margin px inside the rim.
	inner := int(math.Floor(2*(r-margin) + 1e-6))
	if inner < 1 {
		inner = 1
	}
	bw, bh := bbox.Dx(), bbox.Dy()
	scale := math.Min(float64(inner)/float64(bw), float64(inner)/float64(bh))
	nw := int(math.Round(float64(bw) * scale))
	nh := int(math.Round(float64(bh) * scale))
	for (nw > inner || nh > inner) && scale > 0 {
		scale *= 0.99
		nw = int(math.Round(float64(bw) * scale))
		nh = int(math.Round(float64(bh) * scale))
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	return blitScaled(src, bbox, out, nw, nh)
}

func blitScaled(src *image.NRGBA, bbox image.Rectangle, out, nw, nh int) *image.NRGBA {
	cropped := image.NewNRGBA(image.Rect(0, 0, bbox.Dx(), bbox.Dy()))
	draw.Draw(cropped, cropped.Bounds(), src, bbox.Min, draw.Src)
	scaled := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), cropped, cropped.Bounds(), draw.Src, nil)
	mask := image.NewNRGBA(image.Rect(0, 0, out, out))
	ox := (out - nw) / 2
	oy := (out - nh) / 2
	draw.Draw(mask, image.Rect(ox, oy, ox+nw, oy+nh), scaled, image.Point{}, draw.Src)
	return mask
}

func withCircle(fg *image.NRGBA, circle color.NRGBA, out int, r float64) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, out, out))
	cx := float64(out) / 2
	cy := float64(out) / 2
	fillCircle(dst, cx, cy, r, circle)
	for y := 0; y < out; y++ {
		for x := 0; x < out; x++ {
			s := fg.NRGBAAt(x, y)
			if s.A == 0 {
				continue
			}
			// Soft-clip cat to the disc so bbox corners never poke past the rim.
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			cover := r + 0.5 - math.Hypot(dx, dy)
			if cover <= 0 {
				continue
			}
			if cover < 1 {
				s.A = uint8(float64(s.A)*cover + 0.5)
			}
			d := dst.NRGBAAt(x, y)
			dst.SetNRGBA(x, y, over(s, d))
		}
	}
	return dst
}

func fillCircle(img *image.NRGBA, cx, cy, r float64, c color.NRGBA) {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			d := math.Hypot(dx, dy)
			cover := r + 0.5 - d
			if cover <= 0 {
				continue
			}
			if cover > 1 {
				cover = 1
			}
			nc := c
			nc.A = uint8(float64(c.A)*cover + 0.5)
			img.SetNRGBA(x, y, nc)
		}
	}
}

func over(s, d color.NRGBA) color.NRGBA {
	as := float64(s.A) / 255
	ad := float64(d.A) / 255
	ao := as + ad*(1-as)
	if ao <= 0 {
		return color.NRGBA{}
	}
	ch := func(sc, dc uint8) uint8 {
		return uint8(((float64(sc)*as + float64(dc)*ad*(1-as)) / ao) + 0.5)
	}
	return color.NRGBA{R: ch(s.R, d.R), G: ch(s.G, d.G), B: ch(s.B, d.B), A: uint8(ao*255 + 0.5)}
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

func colorizeSolid(mask *image.NRGBA, fill color.NRGBA) *image.NRGBA {
	b := mask.Bounds()
	out := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			a := mask.NRGBAAt(x, y).A
			if a == 0 {
				continue
			}
			c := fill
			c.A = a
			out.SetNRGBA(x, y, c)
		}
	}
	return out
}

func downNearest(src *image.NRGBA, size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.NearestNeighbor.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return dst
}

func upNearest(src *image.NRGBA, factor int) *image.NRGBA {
	w, h := src.Bounds().Dx()*factor, src.Bounds().Dy()*factor
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.NearestNeighbor.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
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

func buildSheet(all []sized) *image.NRGBA {
	const cell = 128
	const pad = 16
	const labelH = 18
	rows := len(all)
	width := pad + 220 + (cell+pad)*2 + 48*3 + 16 + 48*3 + pad
	height := pad + 36 + rows*(cell+labelH+pad) + 40
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), color.NRGBA{R: 24, G: 24, B: 27, A: 255})
	drawLabel(img, pad, pad+12, "Gopurr Windows tray — circle variants (4x nearest) + 16/24/32", color.NRGBA{R: 250, G: 250, B: 249, A: 255})

	y := pad + 36
	for _, it := range all {
		drawLabel(img, pad, y+14, it.p.label, color.NRGBA{R: 212, G: 212, B: 216, A: 255})
		x := pad + 220
		placeZoom(img, it.lt, x, y, cell, color.NRGBA{R: 228, G: 228, B: 231, A: 255})
		x += cell + pad
		placeZoom(img, it.dk, x, y, cell, color.NRGBA{R: 39, G: 39, B: 42, A: 255})
		x += cell + pad + 8
		trayBG := color.NRGBA{R: 243, G: 244, B: 246, A: 255}
		for _, sz := range []int{16, 24, 32} {
			placeSized(img, it.lt, x, y+cell-sz-8, sz, trayBG, 40)
			drawLabel(img, x, y+cell+2, fmt.Sprintf("%d", sz), color.NRGBA{R: 161, G: 161, B: 170, A: 255})
			x += 48
		}
		x += 8
		trayBG = color.NRGBA{R: 24, G: 24, B: 27, A: 255}
		for _, sz := range []int{16, 24, 32} {
			placeSized(img, it.dk, x, y+cell-sz-8, sz, trayBG, 40)
			drawLabel(img, x, y+cell+2, fmt.Sprintf("%d", sz), color.NRGBA{R: 161, G: 161, B: 170, A: 255})
			x += 48
		}
		y += cell + labelH + pad
	}
	drawLabel(img, pad, height-20, "Recommended default: C (inverted badge — disc contrasts with taskbar). Mac tray.png unchanged.", color.NRGBA{R: 161, G: 161, B: 170, A: 255})
	return img
}

func buildTrayStrip(src *image.NRGBA, bbox image.Rectangle, v pair) *image.NRGBA {
	lt, dk := renderPair(src, bbox, v)
	const pad = 20
	width := pad*2 + 280*2
	height := pad*2 + 160
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	fillRect(img, image.Rect(0, 0, width/2, height), color.NRGBA{R: 243, G: 244, B: 246, A: 255})
	fillRect(img, image.Rect(width/2, 0, width, height), color.NRGBA{R: 24, G: 24, B: 27, A: 255})
	drawLabel(img, pad, pad+8, "Light taskbar — recommended C", color.NRGBA{R: 39, G: 39, B: 42, A: 255})
	drawLabel(img, width/2+pad, pad+8, "Dark taskbar — recommended C", color.NRGBA{R: 228, G: 228, B: 231, A: 255})
	x := pad
	for _, sz := range []int{16, 24, 32} {
		var zoom *image.NRGBA
		if sz == 32 {
			zoom = upNearest(lt, 4)
		} else {
			zoom = upNearest(downNearest(lt, sz), 4)
		}
		draw.Draw(img, image.Rect(x, 50, x+zoom.Bounds().Dx(), 50+zoom.Bounds().Dy()), zoom, image.Point{}, draw.Over)
		drawLabel(img, x, 50+zoom.Bounds().Dy()+14, fmt.Sprintf("%dpx -> 4x", sz), color.NRGBA{R: 82, G: 82, B: 91, A: 255})
		x += zoom.Bounds().Dx() + 16
	}
	x = width/2 + pad
	for _, sz := range []int{16, 24, 32} {
		var zoom *image.NRGBA
		if sz == 32 {
			zoom = upNearest(dk, 4)
		} else {
			zoom = upNearest(downNearest(dk, sz), 4)
		}
		draw.Draw(img, image.Rect(x, 50, x+zoom.Bounds().Dx(), 50+zoom.Bounds().Dy()), zoom, image.Point{}, draw.Over)
		drawLabel(img, x, 50+zoom.Bounds().Dy()+14, fmt.Sprintf("%dpx -> 4x", sz), color.NRGBA{R: 161, G: 161, B: 170, A: 255})
		x += zoom.Bounds().Dx() + 16
	}
	return img
}

func placeZoom(dst *image.NRGBA, src *image.NRGBA, x, y, cell int, bg color.NRGBA) {
	fillRect(dst, image.Rect(x, y, x+cell, y+cell), bg)
	z := upNearest(src, cell/src.Bounds().Dx())
	ox := x + (cell-z.Bounds().Dx())/2
	oy := y + (cell-z.Bounds().Dy())/2
	draw.Draw(dst, image.Rect(ox, oy, ox+z.Bounds().Dx(), oy+z.Bounds().Dy()), z, image.Point{}, draw.Over)
}

func placeSized(dst *image.NRGBA, src *image.NRGBA, x, y, size int, bg color.NRGBA, box int) {
	top := y - (box-size)/2
	fillRect(dst, image.Rect(x, top, x+box, top+box), bg)
	s := src
	if size != src.Bounds().Dx() {
		s = downNearest(src, size)
	}
	ox := x + (box-size)/2
	oy := y
	draw.Draw(dst, image.Rect(ox, oy, ox+size, oy+size), s, image.Point{}, draw.Over)
}

func fillRect(img *image.NRGBA, r image.Rectangle, c color.NRGBA) {
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
}

func drawLabel(img *image.NRGBA, x, y int, s string, c color.NRGBA) {
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(c),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, y),
	}
	d.DrawString(s)
}
