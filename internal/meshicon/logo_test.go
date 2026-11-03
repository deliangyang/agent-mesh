package meshicon

import "testing"

func TestDrawSizes(t *testing.T) {
	for _, size := range []int{1, 16, 32, 48, 256} {
		t.Run(itoa(size), func(t *testing.T) {
			img := Draw(size)
			if img.Bounds().Dx() != size || img.Bounds().Dy() != size {
				t.Fatalf("got %dx%d", img.Bounds().Dx(), img.Bounds().Dy())
			}
			c := img.RGBAAt(size/2, size/2)
			if c.A == 0 {
				t.Fatal("center transparent")
			}
		})
	}
}

func TestToARGBNetwork(t *testing.T) {
	img := Draw(16)
	data := ToARGBNetwork(img)
	want := 16 * 16 * 4
	if len(data) != want {
		t.Fatalf("len=%d want %d", len(data), want)
	}
	// Rounded corners are transparent; center of the mark is opaque ink.
	off := (8*16 + 8) * 4
	if data[off] != 0xff {
		t.Fatalf("center alpha %#x", data[off])
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
