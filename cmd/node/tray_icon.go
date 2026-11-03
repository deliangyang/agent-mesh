package main

import "agent-mesh/internal/meshicon"

// trayIconPixmap is one StatusNotifier IconPixmap entry: (width, height, ARGB32 network-order bytes).
type trayIconPixmap struct {
	Width  int32
	Height int32
	Data   []byte
}

// meshTrayIconPixmaps returns Agent Mesh marks at several sizes for HiDPI trays.
func meshTrayIconPixmaps() []trayIconPixmap {
	out := make([]trayIconPixmap, 0, len(meshicon.TraySizes))
	for _, size := range meshicon.TraySizes {
		img := meshicon.Draw(size)
		out = append(out, trayIconPixmap{
			Width:  int32(size),
			Height: int32(size),
			Data:   meshicon.ToARGBNetwork(img),
		})
	}
	return out
}
