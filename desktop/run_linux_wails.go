//go:build linux && cgo

package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
)

// runGUI hosts the shared Vue UI inside WebKitGTK (same navigate-to-node pattern as Windows).
func runGUI(app *App) error {
	return runWails(app, func(opts *options.App) {
		opts.Linux = &linux.Options{
			// Env prep in display_linux.go still needed: DMA-BUF probes /dev/dri before create.
			WebviewGpuPolicy: linux.WebviewGpuPolicyNever,
			ProgramName:      "agent-mesh",
		}
	})
}
