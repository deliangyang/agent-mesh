package main

import (
	"image"
	"image/color"
	"image/draw"
)

// trayIconPixmap is one StatusNotifier IconPixmap entry: (width, height, ARGB32 network-order bytes).
type trayIconPixmap struct {
	Width  int32
	Height int32
	Data   []byte
}

// meshTrayIconPixmaps returns a 32×32 Agent Mesh mark for Linux StatusNotifierItem.
func meshTrayIconPixmaps() []trayIconPixmap {
	const size = 32
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	ink := color.RGBA{R: 0x1e, G: 0x2b, B: 0x27, A: 0xff}
	signal := color.RGBA{R: 0x3d, G: 0x8b, B: 0x6e, A: 0xff}
	paper := color.RGBA{R: 0xf1, G: 0xf3, B: 0xf2, A: 0xff}
	draw.Draw(img, img.Bounds(), &image.Uniform{C: paper}, image.Point{}, draw.Src)
	// Rounded-ish body.
	for y := 4; y < size-4; y++ {
		for x := 4; x < size-4; x++ {
			dx, dy := x-size/2, y-size/2
			if dx*dx+dy*dy <= 12*12 {
				img.SetRGBA(x, y, ink)
			}
		}
	}
	// Signal accent bar.
	for y := 14; y < 18; y++ {
		for x := 10; x < 22; x++ {
			img.SetRGBA(x, y, signal)
		}
	}
	return []trayIconPixmap{{
		Width:  size,
		Height: size,
		Data:   rgbaToARGBNetwork(img),
	}}
}

// rgbaToARGBNetwork encodes premultiplied-looking RGBA into SNI ARGB32 network byte order.
func rgbaToARGBNetwork(img *image.RGBA) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := make([]byte, w*h*4)
	i := 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.RGBAAt(x, y)
			out[i] = c.A
			out[i+1] = c.R
			out[i+2] = c.G
			out[i+3] = c.B
			i += 4
		}
	}
	return out
}
