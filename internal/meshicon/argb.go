package meshicon

import "image"

// ToARGBNetwork encodes an RGBA image into StatusNotifier IconPixmap byte order
// (A, R, G, B per pixel, network / big-endian channel order as used by SNI).
func ToARGBNetwork(img *image.RGBA) []byte {
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
