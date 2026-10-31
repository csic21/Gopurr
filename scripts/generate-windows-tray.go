//go:build ignore

// Regenerate Windows tray icons.
// Default: simplified cat silhouette from tray-face.svg on black/white badge C
// (not a crop of the curled body). Optional layouts still render the curled
// tray.svg (contain/bold/cover/head crop) for comparison.
// Uniform scale + center only (no stretch). macOS tray.png is never touched.
//
//	go run scripts/generate-windows-tray.go
//	go run scripts/generate-windows-tray.go -variant=A   # ivory disc
//	go run scripts/generate-windows-tray.go -variant=B   # soft gray disc
//	go run scripts/generate-windows-tray.go -variant=C   # inverted badge (default)
//	go run scripts/generate-windows-tray.go -variant=none
//	go run scripts/generate-windows-tray.go -layout=face -preview
//	go run scripts/generate-windows-tray.go -layout=head -preview  # prior crop
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

// The selected C badge uses a half-pixel outer margin and a wide, faceless
// cat at 75% scale. Preserve its ears rather than clipping to an inner disc.
// The older body/crop layouts remain available for comparison.
const (
	outSize      = 32
	circleRadius = 15.5
	catMargin    = 1.75 // fit padding so face sits inside disc
	boldBlend    = 0.20 // contain→cover lerp for face/head/bold
	rimKeep      = 2.50 // guaranteed disc ring (px) so art is not jammed to the rim
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
		name: "A", label: "A ivory disc + graphite cat",
		lightCircle: color.NRGBA{R: 245, G: 245, B: 244, A: 255},
		darkCircle:  color.NRGBA{R: 31, G: 41, B: 55, A: 255},
		lightCat:    color.NRGBA{R: 31, G: 41, B: 55, A: 255},
		darkCat:     color.NRGBA{R: 245, G: 245, B: 244, A: 255},
	},
	{
		name: "B", label: "B soft-gray disc + graphite cat",
		lightCircle: color.NRGBA{R: 228, G: 228, B: 231, A: 255},
		darkCircle:  color.NRGBA{R: 15, G: 23, B: 42, A: 255},
		lightCat:    color.NRGBA{R: 31, G: 41, B: 55, A: 255},
		darkCat:     color.NRGBA{R: 250, G: 250, B: 249, A: 255},
	},
	{
		name: "C", label: "C black/white silhouette badge",
		lightCircle: color.NRGBA{A: 255},
		darkCircle:  color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		lightCat:    color.NRGBA{R: 255, G: 255, B: 255, A: 255},
		darkCat:     color.NRGBA{A: 255},
	},
}

type layoutMode string

const (
	layoutContain layoutMode = "contain"
	layoutBold    layoutMode = "bold" // contain→cover blend; keeps badge ring
	layoutCover   layoutMode = "cover"
	layoutHead    layoutMode = "head" // prior: crop ears+head from curled tray.svg
	layoutFace    layoutMode = "face" // wide, faceless tray-face.svg (default)
)

type sized struct {
	name string
	p    pair
	lt   *image.NRGBA
	dk   *image.NRGBA
}

type candidate struct {
	id     string
	label  string
	layout layoutMode
	r      float64
	margin float64
	stroke bool
}

func main() {
	variant := flag.String("variant", "C", "pair to write into internal/brand: A|B|C|none")
	layoutFlag := flag.String("layout", "face", "silhouette: face (default, tray-face.svg) | head (crop) | contain|bold|cover")
	preview := flag.Bool("preview", false, "emit all variants + comparison sheets under docs/ and mockups/")
	mockups := flag.String("mockups", ".cache/tray-mockups", "directory for alternate PNGs")
	flag.Parse()

	layout := layoutMode(*layoutFlag)
	switch layout {
	case layoutContain, layoutBold, layoutCover, layoutHead, layoutFace:
	default:
		log.Fatalf("unknown -layout=%q (want face, head, contain, bold, or cover)", *layoutFlag)
	}

	src, err := renderSource(layout)
	if err != nil {
		log.Fatal(err)
	}
	fullBBox := opaqueBounds(src, 12)
	if fullBBox.Empty() {
		log.Fatal("no opaque pixels in tray render")
	}

	chosen := findVariant(*variant)
	if chosen == nil {
		log.Fatalf("unknown -variant=%q (want A, B, C, or none)", *variant)
	}

	geom := candidate{
		id:     "recommended",
		label:  fmt.Sprintf("recommended C + %s (cat silhouette) r=%.2f m=%.2f", layout, circleRadius, catMargin),
		layout: layout,
		r:      circleRadius,
		margin: catMargin,
	}

	light, dark := renderPairGeom(src, fullBBox, *chosen, geom)
	dir := "internal/brand"
	if err := writePNG(filepath.Join(dir, "tray-windows-light.png"), light); err != nil {
		log.Fatal(err)
	}
	if err := writePNG(filepath.Join(dir, "tray-windows-dark.png"), dark); err != nil {
		log.Fatal(err)
	}
	fb := opaqueBounds(light, 8)
	fmt.Printf("default variant=%s layout=%s r=%.2f margin=%.2f content bbox=%v size=%dx%d\n",
		chosen.name, layout, circleRadius, catMargin, fb, fb.Dx(), fb.Dy())

	if !*preview {
		return
	}
	if err := os.MkdirAll(*mockups, 0o755); err != nil {
		log.Fatal(err)
	}

	var all []sized
	for _, v := range variants {
		lt, dk := renderPairGeom(src, fullBBox, v, geom)
		all = append(all, sized{v.name, v, lt, dk})
		base := filepath.Join(*mockups, "circle-"+v.name)
		_ = writePNG(base+"-light-32.png", lt)
		_ = writePNG(base+"-dark-32.png", dk)
		_ = writePNG(base+"-light-24.png", downsample(lt, 24))
		_ = writePNG(base+"-dark-24.png", downsample(dk, 24))
		_ = writePNG(base+"-light-16.png", downsample(lt, 16))
		_ = writePNG(base+"-dark-16.png", downsample(dk, 16))
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
	strip := buildTrayStrip(src, fullBBox, *rec, geom)
	if err := writePNG("docs/tray-windows-circle-sizes.png", strip); err != nil {
		log.Fatal(err)
	}
	if err := writePNG(filepath.Join(*mockups, "windows-tray-circle-recommended.png"), strip); err != nil {
		log.Fatal(err)
	}

	bodySrc, err := renderSource(layoutBold)
	if err != nil {
		log.Fatal(err)
	}
	bodyBBox := opaqueBounds(bodySrc, 12)
	faceSrc, err := renderSource(layoutFace)
	if err != nil {
		log.Fatal(err)
	}
	faceBBox := opaqueBounds(faceSrc, 12)

	cands := []candidate{
		{id: "body-bold", label: "Previous: curled cat", layout: layoutBold, r: circleRadius, margin: 1.50},
		{id: "head-crop", label: "Previous: head crop", layout: layoutHead, r: circleRadius, margin: catMargin},
		{id: "face", label: "C: silhouette (selected)", layout: layoutFace, r: circleRadius, margin: catMargin},
		{id: "face-tight", label: "C: tighter padding", layout: layoutFace, r: circleRadius, margin: 1.25},
		{id: "face-stroke", label: "C: rim stroke", layout: layoutFace, r: circleRadius, margin: catMargin, stroke: true},
	}
	cmp := buildCandidateCompareMulti(bodySrc, bodyBBox, faceSrc, faceBBox, *rec, cands, *mockups)
	if err := writePNG("docs/tray-windows-size-compare.png", cmp); err != nil {
		log.Fatal(err)
	}
	if err := writePNG(filepath.Join(*mockups, "windows-tray-size-compare.png"), cmp); err != nil {
		log.Fatal(err)
	}

	bodyFace := buildCropVsNewFace(bodySrc, bodyBBox, faceSrc, faceBBox, *rec)
	if err := writePNG("docs/tray-windows-body-vs-face.png", bodyFace); err != nil {
		log.Fatal(err)
	}
	if err := writePNG(filepath.Join(*mockups, "windows-tray-body-vs-face.png"), bodyFace); err != nil {
		log.Fatal(err)
	}
	if err := writePNG("docs/tray-windows-crop-vs-face.png", bodyFace); err != nil {
		log.Fatal(err)
	}
	if err := writePNG(filepath.Join(*mockups, "windows-tray-crop-vs-face.png"), bodyFace); err != nil {
		log.Fatal(err)
	}

	fmt.Println("preview sheets written; recommended default = C inverted badge + face (tray-face.svg)")
}

func findVariant(name string) *pair {
	for i := range variants {
		if variants[i].name == name {
			return &variants[i]
		}
	}
	return nil
}

func renderSource(layout layoutMode) (*image.NRGBA, error) {
	const dir = "internal/brand"
	name := "tray.svg"
	if layout == layoutFace {
		name = "tray-face.svg"
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
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

func renderPairGeom(src *image.NRGBA, fullBBox image.Rectangle, v pair, g candidate) (light, dark *image.NRGBA) {
	out := outSize
	useCircle := v.lightCircle.A > 0
	if useCircle && g.layout == layoutFace {
		return renderFaceBadge(src, v, g)
	}
	bbox := fullBBox
	if g.layout == layoutHead {
		bbox = headCrop(fullBBox)
	}
	var mask *image.NRGBA
	if useCircle {
		switch g.layout {
		case layoutCover:
			mask = placeInCircleCover(src, bbox, out, g.r, g.margin)
		case layoutHead, layoutFace:
			// Face asset or head crop + mild bold blend; rimKeep preserves disc ring.
			mask = placeInCircleBlend(src, bbox, out, g.r, g.margin, boldBlend)
		case layoutBold:
			mask = placeInCircleBlend(src, bbox, out, g.r, g.margin, boldBlend)
		default:
			mask = placeInCircleContain(src, bbox, out, g.r, g.margin)
		}
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
		light = withCircle(light, v.lightCircle, out, g.r)
		dark = withCircle(dark, v.darkCircle, out, g.r)
		if g.stroke {
			light = withRim(light, v.lightCat, out, g.r, 0.9)
			dark = withRim(dark, v.darkCat, out, g.r, 0.9)
		}
	}
	return light, dark
}

func renderFaceBadge(src *image.NRGBA, v pair, g candidate) (*image.NRGBA, *image.NRGBA) {
	const scale = 4
	const size = outSize * scale
	// Match the selected design study: 24x24 viewport at (4,3) on a 32px
	// canvas. The slight upward offset balances the broad lower cheeks.
	side := float64(outSize)*0.75 + 2*(catMargin-g.margin)
	x := (float64(outSize) - side) / 2
	y := x - 1
	mask := image.NewNRGBA(image.Rect(0, 0, size, size))
	rect := image.Rect(int(math.Round(x*scale)), int(math.Round(y*scale)),
		int(math.Round((x+side)*scale)), int(math.Round((y+side)*scale)))
	draw.CatmullRom.Scale(mask, rect, src, src.Bounds(), draw.Src, nil)
	compose := func(cat, disc color.NRGBA) *image.NRGBA {
		img := image.NewNRGBA(mask.Bounds())
		fillCircle(img, size/2, size/2, g.r*scale, disc)
		fg := colorizeSolid(mask, cat)
		draw.Draw(img, img.Bounds(), fg, image.Point{}, draw.Over)
		out := image.NewNRGBA(image.Rect(0, 0, outSize, outSize))
		draw.CatmullRom.Scale(out, out.Bounds(), img, img.Bounds(), draw.Src, nil)
		if g.stroke {
			out = withRim(out, cat, outSize, g.r, 0.9)
		}
		return out
	}
	return compose(v.lightCat, v.lightCircle), compose(v.darkCat, v.darkCircle)
}

func headCrop(b image.Rectangle) image.Rectangle {
	// Curled cat faces right: ears + head live in the upper-right of the
	// full-body silhouette. Drop most of the left-side body/back so the
	// tray disc reads as a cat face (ears+cheek) at 16–24px.
	dx, dy := b.Dx(), b.Dy()
	c := image.Rect(
		b.Min.X+dx*38/100,
		b.Min.Y,
		b.Max.X,
		b.Min.Y+dy*68/100,
	)
	return c.Intersect(b)
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

func placeInCircleContain(src *image.NRGBA, bbox image.Rectangle, out int, r, margin float64) *image.NRGBA {
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

func placeInCircleCover(src *image.NRGBA, bbox image.Rectangle, out int, r, margin float64) *image.NRGBA {
	inner := 2 * (r - margin)
	if inner < 1 {
		inner = 1
	}
	bw, bh := float64(bbox.Dx()), float64(bbox.Dy())
	scale := math.Max(inner/bw, inner/bh)
	nw := int(math.Round(bw * scale))
	nh := int(math.Round(bh * scale))
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	return blitScaled(src, bbox, out, nw, nh)
}

func placeInCircleBlend(src *image.NRGBA, bbox image.Rectangle, out int, r, margin, blend float64) *image.NRGBA {
	// Lerp contain→cover so the cat grows without erasing the disc ring that
	// gives the inverted badge its Steam-like footprint on the taskbar.
	if blend <= 0 {
		return placeInCircleContain(src, bbox, out, r, margin)
	}
	if blend >= 1 {
		return placeInCircleCover(src, bbox, out, r, margin)
	}
	inner := 2 * (r - margin)
	if inner < 1 {
		inner = 1
	}
	bw, bh := float64(bbox.Dx()), float64(bbox.Dy())
	scContain := math.Min(inner/bw, inner/bh)
	scCover := math.Max(inner/bw, inner/bh)
	scale := scContain*(1-blend) + scCover*blend
	nw := int(math.Round(bw * scale))
	nh := int(math.Round(bh * scale))
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
	return withCircleRing(fg, circle, out, r, rimKeep)
}

func withCircleRing(fg *image.NRGBA, circle color.NRGBA, out int, r, keep float64) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, out, out))
	cx := float64(out) / 2
	cy := float64(out) / 2
	fillCircle(dst, cx, cy, r, circle)
	// Soft-clip cat inside the disc, leaving `keep` px of pure disc so the
	// inverted badge still reads as a Steam-sized circle on the taskbar.
	catR := r - keep
	if catR < r*0.5 {
		catR = r * 0.5
	}
	for y := 0; y < out; y++ {
		for x := 0; x < out; x++ {
			s := fg.NRGBAAt(x, y)
			if s.A == 0 {
				continue
			}
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			cover := catR + 0.5 - math.Hypot(dx, dy)
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

func withRim(img *image.NRGBA, rim color.NRGBA, out int, r, width float64) *image.NRGBA {
	dst := cloneNRGBA(img)
	cx := float64(out) / 2
	cy := float64(out) / 2
	outer := r + 0.15
	inner := r - width
	for y := 0; y < out; y++ {
		for x := 0; x < out; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			d := math.Hypot(dx, dy)
			if d > outer+0.5 || d < inner-0.5 {
				continue
			}
			cover := 1.0
			if d > outer-0.5 {
				cover = outer + 0.5 - d
			} else if d < inner+0.5 {
				cover = d - (inner - 0.5)
			}
			if cover <= 0 {
				continue
			}
			if cover > 1 {
				cover = 1
			}
			c := rim
			c.A = uint8(float64(rim.A)*cover*0.85 + 0.5)
			dst.SetNRGBA(x, y, over(c, dst.NRGBAAt(x, y)))
		}
	}
	return dst
}

func cloneNRGBA(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
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

func downsample(src *image.NRGBA, size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
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
	drawLabel(img, pad, pad+12, "Gopurr Windows tray — circle variants (r=15.5 + cat silhouette) + 16/24/32", color.NRGBA{R: 250, G: 250, B: 249, A: 255})

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
	drawLabel(img, pad, height-20, "Recommended: C inverted badge + wide cat silhouette (tray-face.svg). Mac tray.png unchanged.", color.NRGBA{R: 161, G: 161, B: 170, A: 255})
	return img
}

func buildTrayStrip(src *image.NRGBA, bbox image.Rectangle, v pair, g candidate) *image.NRGBA {
	lt, dk := renderPairGeom(src, bbox, v, g)
	const pad = 20
	width := pad*2 + 336*2
	height := pad*2 + 160
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	fillRect(img, image.Rect(0, 0, width/2, height), color.NRGBA{R: 243, G: 244, B: 246, A: 255})
	fillRect(img, image.Rect(width/2, 0, width, height), color.NRGBA{R: 24, G: 24, B: 27, A: 255})
	drawLabel(img, pad, pad+8, "Light taskbar — recommended C cat silhouette", color.NRGBA{R: 39, G: 39, B: 42, A: 255})
	drawLabel(img, width/2+pad, pad+8, "Dark taskbar — recommended C cat silhouette", color.NRGBA{R: 228, G: 228, B: 231, A: 255})
	x := pad
	for _, sz := range []int{16, 24, 32} {
		var zoom *image.NRGBA
		if sz == 32 {
			zoom = upNearest(lt, 4)
		} else {
			zoom = upNearest(downsample(lt, sz), 4)
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
			zoom = upNearest(downsample(dk, sz), 4)
		}
		draw.Draw(img, image.Rect(x, 50, x+zoom.Bounds().Dx(), 50+zoom.Bounds().Dy()), zoom, image.Point{}, draw.Over)
		drawLabel(img, x, 50+zoom.Bounds().Dy()+14, fmt.Sprintf("%dpx -> 4x", sz), color.NRGBA{R: 161, G: 161, B: 170, A: 255})
		x += zoom.Bounds().Dx() + 16
	}
	return img
}

func buildCropVsNewFace(bodySrc *image.NRGBA, bodyBBox image.Rectangle, faceSrc *image.NRGBA, faceBBox image.Rectangle, v pair) *image.NRGBA {
	bodyGeom := candidate{
		id: "body", label: "full-body bold", layout: layoutBold, r: circleRadius, margin: 1.50,
	}
	cropGeom := candidate{
		id: "crop", label: "prior head crop", layout: layoutHead, r: circleRadius, margin: catMargin,
	}
	faceGeom := candidate{
		id: "face", label: "wide cat silhouette", layout: layoutFace, r: circleRadius, margin: catMargin,
	}
	bodyLt, bodyDk := renderPairGeom(bodySrc, bodyBBox, v, bodyGeom)
	cropLt, cropDk := renderPairGeom(bodySrc, bodyBBox, v, cropGeom)
	faceLt, faceDk := renderPairGeom(faceSrc, faceBBox, v, faceGeom)

	const pad = 20
	const cell = 128
	const rowH = cell + 72
	width := pad*2 + 220 + (cell+16)*2 + 48*3 + 24
	height := pad + 48 + rowH*3 + 36
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), color.NRGBA{R: 24, G: 24, B: 27, A: 255})
	drawLabel(img, pad, pad+12, "Windows tray: curled body / prior head-crop / wide cat silhouette — variant C", color.NRGBA{R: 250, G: 250, B: 249, A: 255})
	drawLabel(img, pad, pad+28, "Selected: wide cat silhouette; disc r=15.5. Mac tray.png unchanged.", color.NRGBA{R: 161, G: 161, B: 170, A: 255})

	rows := []struct {
		title  string
		lt, dk *image.NRGBA
	}{
		{"FULL-BODY (curled)", bodyLt, bodyDk},
		{"PRIOR HEAD CROP", cropLt, cropDk},
		{"CAT SILHOUETTE (recommended)", faceLt, faceDk},
	}
	y := pad + 48
	for _, row := range rows {
		drawLabel(img, pad, y+14, row.title, color.NRGBA{R: 212, G: 212, B: 216, A: 255})
		x := pad + 220
		placeZoom(img, row.lt, x, y, cell, color.NRGBA{R: 228, G: 228, B: 231, A: 255})
		x += cell + 16
		placeZoom(img, row.dk, x, y, cell, color.NRGBA{R: 39, G: 39, B: 42, A: 255})
		x += cell + 24
		trayBG := color.NRGBA{R: 243, G: 244, B: 246, A: 255}
		for _, sz := range []int{16, 24, 32} {
			placeSized(img, row.lt, x, y+cell-sz-20, sz, trayBG, 40)
			drawLabel(img, x, y+cell-2, fmt.Sprintf("%d", sz), color.NRGBA{R: 161, G: 161, B: 170, A: 255})
			x += 48
		}
		y += rowH
	}
	drawLabel(img, pad, height-18, "Shipped: internal/brand/tray-windows-light/dark.png (C + tray-face.svg).", color.NRGBA{R: 161, G: 161, B: 170, A: 255})
	return img
}

func buildCandidateCompareMulti(bodySrc *image.NRGBA, bodyBBox image.Rectangle, faceSrc *image.NRGBA, faceBBox image.Rectangle, v pair, cands []candidate, mockups string) *image.NRGBA {
	const pad = 16
	const cell = 96
	const rowH = cell + 56
	const steamBox = 40
	width := pad + 300 + 48*3 + 16 + cell + 24 + steamBox + pad
	height := pad + 48 + len(cands)*rowH + 36
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	fillRect(img, img.Bounds(), color.NRGBA{R: 24, G: 24, B: 27, A: 255})
	drawLabel(img, pad, pad+12, "Windows tray: layouts at 16/24/32px + 3x zoom; circular footprint reference", color.NRGBA{R: 250, G: 250, B: 249, A: 255})
	drawLabel(img, pad, pad+28, "Selected: tray-face.svg; disc r=15.5; 75% viewport; no ear clipping", color.NRGBA{R: 161, G: 161, B: 170, A: 255})

	trayBG := color.NRGBA{R: 243, G: 244, B: 246, A: 255}
	steamRef := color.NRGBA{R: 31, G: 41, B: 55, A: 255}

	y := pad + 48
	for _, g := range cands {
		var src *image.NRGBA
		var bbox image.Rectangle
		if g.layout == layoutFace {
			src, bbox = faceSrc, faceBBox
		} else {
			src, bbox = bodySrc, bodyBBox
		}
		lt, _ := renderPairGeom(src, bbox, v, g)
		base := filepath.Join(mockups, "cand-"+g.id)
		_ = writePNG(base+"-light-32.png", lt)
		_ = writePNG(base+"-light-24.png", downsample(lt, 24))
		_ = writePNG(base+"-light-16.png", downsample(lt, 16))
		_ = writePNG(base+"-light-128.png", upNearest(lt, 4))

		drawLabel(img, pad, y+14, g.label, color.NRGBA{R: 212, G: 212, B: 216, A: 255})
		x := pad + 300
		for _, sz := range []int{16, 24, 32} {
			placeSized(img, lt, x, y+cell-sz-20, sz, trayBG, 40)
			drawLabel(img, x, y+cell-2, fmt.Sprintf("%d", sz), color.NRGBA{R: 161, G: 161, B: 170, A: 255})
			x += 48
		}
		x += 8
		placeZoom(img, lt, x, y, cell, trayBG)
		x += cell + 16
		ref := image.NewNRGBA(image.Rect(0, 0, 32, 32))
		fillCircle(ref, 16, 16, 16.0, steamRef)
		fillCircle(ref, 16, 16, 6.5, color.NRGBA{R: 245, G: 245, B: 244, A: 255})
		placeSized(img, ref, x, y+cell-32-20, 32, trayBG, steamBox)
		drawLabel(img, x-4, y+cell-2, "Steam~", color.NRGBA{R: 161, G: 161, B: 170, A: 255})

		y += rowH
	}
	drawLabel(img, pad, height-18, "Recommended: tray-windows-light/dark.png (C + tray-face.svg silhouette).", color.NRGBA{R: 161, G: 161, B: 170, A: 255})
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
		s = downsample(src, size)
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
