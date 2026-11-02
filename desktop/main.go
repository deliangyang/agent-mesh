package main

import (
	"embed"
	"flag"
	"log"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

//go:embed all:frontend/dist
var assets embed.FS

func parseBoot() bootArgs {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	relay := fs.String("relay", os.Getenv("MESH_RELAY"), "relay URL, for example https://mesh.ranchulin.com")
	password := fs.String("password", os.Getenv("MESH_PASSWORD"), "relay password")
	advertise := fs.String("advertise", os.Getenv("MESH_ADVERTISE"), "public callback URL; may be empty")
	headless := fs.Bool("headless", false, "headless; local node HTTP only")
	_ = fs.Parse(os.Args[1:])
	url := strings.TrimSpace(*relay)
	if url == "" {
		for _, a := range fs.Args() {
			if strings.HasPrefix(a, "http://") || strings.HasPrefix(a, "https://") {
				url = a
				break
			}
		}
	}
	return bootArgs{Relay: url, Password: *password, Advertise: *advertise, Headless: *headless}
}

func main() {
	boot := parseBoot()
	app := NewApp()
	app.boot = boot
	if boot.Headless {
		app.runHeadless()
		return
	}
	if err := ensureGUIEnv(); err != nil {
		log.Printf("cannot open a window: %v", err)
		log.Printf("falling back to headless node mode; pass -headless to do this explicitly")
		app.runHeadless()
		return
	}
	err := wails.Run(&options.App{
		Title:     "Agent Mesh",
		Width:     1360,
		Height:    860,
		MinWidth:  980,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 28, G: 23, B: 18, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
		Linux: &linux.Options{
			WebviewGpuPolicy: linux.WebviewGpuPolicyNever,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
