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
	relay := fs.String("relay", os.Getenv("MESH_RELAY"), "中继服务地址，例如 https://mesh.ranchulin.com")
	password := fs.String("password", os.Getenv("MESH_PASSWORD"), "中继约定密码")
	advertise := fs.String("advertise", os.Getenv("MESH_ADVERTISE"), "对外回连地址，可空")
	headless := fs.Bool("headless", false, "无窗口，仅本机节点 HTTP 服务")
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
		log.Printf("无法打开窗口: %v", err)
		log.Printf("改为无界面节点模式，可用 -headless 显式指定")
		app.runHeadless()
		return
	}
	err := wails.Run(&options.App{
		Title:     "接线台 · Agent Mesh",
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
