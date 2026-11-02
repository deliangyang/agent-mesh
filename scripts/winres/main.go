// Generates cmd/node Windows icon.ico and rsrc_windows_*.syso.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func main() {
	root, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	// scripts/winres → repo root
	if filepath.Base(root) == "winres" {
		root = filepath.Join(root, "..", "..")
	}
	nodeDir := filepath.Join(root, "cmd", "node")
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		fatal(err)
	}

	sizes := []int{16, 32, 48, 256}
	pngs := make([][]byte, len(sizes))
	imgs := make([]image.Image, len(sizes))
	for i, s := range sizes {
		img := drawLogo(s)
		imgs[i] = img
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			fatal(err)
		}
		pngs[i] = buf.Bytes()
	}
	ico := encodeICO(pngs, sizes)
	icoPath := filepath.Join(nodeDir, "icon.ico")
	if err := os.WriteFile(icoPath, ico, 0o644); err != nil {
		fatal(err)
	}

	icon, err := winres.NewIconFromImages(imgs)
	if err != nil {
		fatal(err)
	}
	rs := winres.ResourceSet{}
	if err := rs.SetIcon(winres.Name("APPICON"), icon); err != nil {
		fatal(err)
	}
	vi := version.Info{
		FileVersion:    [4]uint16{1, 0, 0, 0},
		ProductVersion: [4]uint16{1, 0, 0, 0},
	}
	_ = vi.Set(version.LangDefault, version.ProductName, "Agent Mesh")
	_ = vi.Set(version.LangDefault, version.FileDescription, "Agent Mesh Node")
	_ = vi.Set(version.LangDefault, version.OriginalFilename, "mesh-node.exe")
	_ = vi.Set(version.LangDefault, version.InternalName, "mesh-node")
	rs.SetVersionInfo(vi)
	rs.SetManifest(winres.AppManifest{
		Description:         "Agent Mesh Node",
		ExecutionLevel:      winres.AsInvoker,
		UseCommonControlsV6: true,
	})
	for _, arch := range []struct {
		name string
		arch winres.Arch
	}{
		{"amd64", winres.ArchAMD64},
		{"arm64", winres.ArchARM64},
	} {
		path := filepath.Join(nodeDir, "rsrc_windows_"+arch.name+".syso")
		f, err := os.Create(path)
		if err != nil {
			fatal(err)
		}
		if err := rs.WriteObject(f, arch.arch); err != nil {
			f.Close()
			fatal(err)
		}
		if err := f.Close(); err != nil {
			fatal(err)
		}
		fmt.Println("wrote", path)
	}
	fmt.Println("wrote", icoPath)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func encodeICO(pngs [][]byte, sizes []int) []byte {
	n := len(pngs)
	offset := 6 + 16*n
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, uint16(0))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
	_ = binary.Write(&buf, binary.LittleEndian, uint16(n))
	for i, p := range pngs {
		w := sizes[i]
		wb := byte(w)
		if w >= 256 {
			wb = 0
		}
		buf.WriteByte(wb)
		buf.WriteByte(wb)
		buf.WriteByte(0)
		buf.WriteByte(0)
		_ = binary.Write(&buf, binary.LittleEndian, uint16(1))
		_ = binary.Write(&buf, binary.LittleEndian, uint16(32))
		_ = binary.Write(&buf, binary.LittleEndian, uint32(len(p)))
		_ = binary.Write(&buf, binary.LittleEndian, uint32(offset))
		offset += len(p)
	}
	for _, p := range pngs {
		buf.Write(p)
	}
	return buf.Bytes()
}

func drawLogo(size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg := color.RGBA{0x1E, 0x2B, 0x27, 0xFF}
	ink := color.RGBA{0xE3, 0xEB, 0xE6, 0xFF}
	signal := color.RGBA{0x7F, 0xBE, 0xAD, 0xFF}
	rr := float64(size) * 7 / 32
	fillRoundRect(img, bg, rr)
	s := float64(size)
	x1, y1 := s*16/32, s*9/32
	x2, y2 := s*9.2/32, s*21.2/32
	x3, y3 := s*22.8/32, s*21.2/32
	stroke := s * 1.6 / 32
	if stroke < 1 {
		stroke = 1
	}
	strokeLine(img, signal, x1, y1, x2, y2, stroke)
	strokeLine(img, signal, x2, y2, x3, y3, stroke)
	strokeLine(img, signal, x3, y3, x1, y1, stroke)
	cr := s * 2.4 / 32
	fillCircle(img, ink, x1, y1, cr)
	fillCircle(img, signal, x2, y2, cr)
	fillCircle(img, signal, x3, y3, cr)
	return img
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
