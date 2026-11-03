//go:build (windows || linux) && cgo

package main

import (
	"context"
	"embed"
	"fmt"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var splashAssets embed.FS

// runWails hosts the shared Vue UI in a native webview.
//
// Wails' AssetServer does not support WebSockets, and the Vue catalog uses
// /v1/openai/ws, so after the local node is up we navigate the webview to the
// real http://127.0.0.1:<port>/ origin. Bindings stay available for future
// shell controls; the Vue app keeps talking HTTP/WS to the node.
func runWails(app *App, tune func(*options.App)) error {
	opts := &options.App{
		Title:     "Agent Mesh",
		Width:     1360,
		Height:    860,
		MinWidth:  980,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: splashAssets,
		},
		BackgroundColour: &options.RGBA{R: 28, G: 23, B: 18, A: 255},
		OnStartup:        app.startup,
		OnDomReady:       app.navigateToLocalUI,
		OnShutdown:       app.shutdown,
		Bind:             []interface{}{app},
	}
	if tune != nil {
		tune(opts)
	}
	if err := wails.Run(opts); err != nil {
		return fmt.Errorf("desktop wails: %w", err)
	}
	return nil
}

func (a *App) navigateToLocalUI(ctx context.Context) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if a.srv != nil && a.listen != "" {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if a.srv == nil || a.listen == "" {
		runtime.LogError(ctx, "desktop wails: node did not start")
		return
	}
	uiURL := strings.TrimRight(localUIURL(a.listen), "/") + "/"
	waitUI(strings.TrimRight(uiURL, "/"))
	runtime.WindowExecJS(ctx, fmt.Sprintf("window.location.replace(%q)", uiURL))
}
