// Package meshicon draws the Agent Mesh mark at arbitrary pixel sizes.
package meshicon

import (
	"image"
	"image/color"
	"image/draw"
)

// Brand colors match the web UI tokens (--ink / --signal / paper accent).
var (
	Ink    = color.RGBA{R: 0x1e, G: 0x2b, B: 0x27, A: 0xff}
	Signal = color.RGBA{R: 0x7f, G: 0xbe, B: 0xad, A: 0xff}
	Paper  = color.RGBA{R: 0xe3, G: 0xeb, B: 0xe6, A: 0xff}
)

// DesktopSizes are Freedesktop hicolor app-icon sizes.
var DesktopSizes = []int{16, 24, 32, 48, 64, 128, 256, 512}

// TraySizes are common StatusNotifier / HiDPI tray pixmap sizes.
var TraySizes = []int{16, 22, 24, 32, 48, 64}

// WindowsICOSizes are the bitmaps packed into icon.ico / APPICON.
var WindowsICOSizes = []int{16, 20, 24, 32, 40, 48, 64, 128, 256}

// Draw returns a size×size Agent Mesh mark (dark rounded square, signal triangle).
// Small sizes are supersampled so edges stay sharp on HiDPI trays and launchers.
func Draw(size int) *image.RGBA {
	if size < 1 {
		size = 1
	}
	scale := 1
	if size < 128 {
		scale = 4
	}
	src := drawExact(size * scale)
	if scale == 1 {
		return src
	}
	return downscaleBox(src, size)
}

func drawExact(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	rr := float64(size) * 7 / 32
	fillRoundRect(img, Ink, rr)
	s := float64(size)
	x1, y1 := s*16/32, s*9/32
	x2, y2 := s*9.2/32, s*21.2/32
	x3, y3 := s*22.8/32, s*21.2/32
	stroke := s * 1.6 / 32
	if stroke < 1 {
		stroke = 1
	}
	strokeLine(img, Signal, x1, y1, x2, y2, stroke)
	strokeLine(img, Signal, x2, y2, x3, y3, stroke)
	strokeLine(img, Signal, x3, y3, x1, y1, stroke)
	cr := s * 2.4 / 32
	fillCircle(img, Paper, x1, y1, cr)
	fillCircle(img, Signal, x2, y2, cr)
	fillCircle(img, Signal, x3, y3, cr)
	return img
}

func downscaleBox(src *image.RGBA, size int) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	sw := src.Bounds().Dx()
	factor := sw / size
	if factor < 1 {
		draw.Draw(out, out.Bounds(), src, image.Point{}, draw.Src)
		return out
	}
	area := uint32(factor * factor)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a uint32
			for dy := 0; dy < factor; dy++ {
				for dx := 0; dx < factor; dx++ {
					c := src.RGBAAt(x*factor+dx, y*factor+dy)
					r += uint32(c.R)
					g += uint32(c.G)
					b += uint32(c.B)
					a += uint32(c.A)
				}
			}
			out.SetRGBA(x, y, color.RGBA{
				R: uint8(r / area),
				G: uint8(g / area),
				B: uint8(b / area),
				A: uint8(a / area),
			})
		}
	}
	return out
}

func fillRoundRect(img *image.RGBA, c color.RGBA, r float64) {
	w := img.Bounds().Dx()
	h := img.Bounds().Dy()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if inRoundRect(float64(x)+0.5, float64(y)+0.5, float64(w), float64(h), r) {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

func inRoundRect(x, y, w, h, r float64) bool {
	if r < 1 {
		return true
	}
	if x >= r && x < w-r {
		return y >= 0 && y < h
	}
	if y >= r && y < h-r {
		return x >= 0 && x < w
	}
	cx, cy := r, r
	switch {
	case x >= w-r && y < r:
		cx, cy = w-r, r
	case x < r && y >= h-r:
		cx, cy = r, h-r
	case x >= w-r && y >= h-r:
		cx, cy = w-r, h-r
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

func fillCircle(img *image.RGBA, c color.RGBA, cx, cy, r float64) {
	minx := int(cx - r - 1)
	maxx := int(cx + r + 1)
	miny := int(cy - r - 1)
	maxy := int(cy + r + 1)
	rr := r * r
	b := img.Bounds()
	for y := miny; y <= maxy; y++ {
		for x := minx; x <= maxx; x++ {
			if !image.Pt(x, y).In(b) {
				continue
			}
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			if dx*dx+dy*dy <= rr {
				img.SetRGBA(x, y, c)
			}
		}
	}
}

func strokeLine(img *image.RGBA, c color.RGBA, x0, y0, x1, y1, width float64) {
	dx := x1 - x0
	dy := y1 - y0
	n := int(abs(dx)+abs(dy))*2 + 1
	hw := width / 2
	b := img.Bounds()
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		cx := x0 + dx*t
		cy := y0 + dy*t
		minx := int(cx - hw - 1)
		maxx := int(cx + hw + 1)
		miny := int(cy - hw - 1)
		maxy := int(cy + hw + 1)
		rr := hw * hw
		for y := miny; y <= maxy; y++ {
			for x := minx; x <= maxx; x++ {
				if !image.Pt(x, y).In(b) {
					continue
				}
				ddx := float64(x) + 0.5 - cx
				ddy := float64(y) + 0.5 - cy
				if ddx*ddx+ddy*ddy <= rr {
					img.SetRGBA(x, y, c)
				}
			}
		}
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
