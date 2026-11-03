// Generates Windows icon.ico / rsrc_windows_*.syso and Linux desktop PNG icons.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"agent-mesh/internal/meshicon"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func main() {
	root, err := repoRoot()
	if err != nil {
		fatal(err)
	}
	nodeDir := filepath.Join(root, "cmd", "node")
	if err := os.MkdirAll(nodeDir, 0o755); err != nil {
		fatal(err)
	}

	sizes := meshicon.WindowsICOSizes
	pngs := make([][]byte, len(sizes))
	imgs := make([]image.Image, len(sizes))
	for i, s := range sizes {
		img := meshicon.Draw(s)
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

	if err := writeDesktopIcons(root); err != nil {
		fatal(err)
	}
}

func writeDesktopIcons(root string) error {
	base := filepath.Join(root, "deploy", "desktop", "icons")
	if err := os.MkdirAll(filepath.Join(base, "scalable", "apps"), 0o755); err != nil {
		return err
	}
	svgPath := filepath.Join(base, "scalable", "apps", "agent-mesh.svg")
	if err := os.WriteFile(svgPath, []byte(meshicon.SVG), 0o644); err != nil {
		return err
	}
	fmt.Println("wrote", svgPath)
	for _, size := range meshicon.DesktopSizes {
		dir := filepath.Join(base, fmt.Sprintf("%dx%d", size, size), "apps")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		path := filepath.Join(dir, "agent-mesh.png")
		f, err := os.Create(path)
		if err != nil {
			return err
		}
		if err := png.Encode(f, meshicon.Draw(size)); err != nil {
			f.Close()
			return err
		}
		if err := f.Close(); err != nil {
			return err
		}
		fmt.Println("wrote", path)
	}
	return nil
}

func repoRoot() (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", err
	}
	// Prefer module root when run as `go run ./scripts/winres`.
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		return root, nil
	}
	// Legacy: `cd scripts/winres && go run .`
	if filepath.Base(root) == "winres" {
		cand := filepath.Join(root, "..", "..")
		if _, err := os.Stat(filepath.Join(cand, "go.mod")); err == nil {
			return cand, nil
		}
	}
	return root, nil
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
