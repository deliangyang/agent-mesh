//go:build windows && cgo

package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// runGUI hosts the shared Vue UI inside WebView2.
func runGUI(app *App) error {
	return runWails(app, func(opts *options.App) {
		opts.Windows = &windows.Options{
			WebviewIsTransparent: false,
			Theme:                windows.SystemDefault,
		}
	})
}
