//go:build (!windows && !linux) || !cgo

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
)

// runGUI starts the local node and opens the shared Vue UI in a Chrome/Edge app window.
// Used when CGO is off, or on platforms without a Wails shell (macOS). Windows/Linux with
// CGO use run_*_wails.go instead.
func runGUI(app *App) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app.startup(ctx)
	if app.srv == nil || app.listen == "" {
		return fmt.Errorf("desktop node failed to start")
	}
	uiURL := localUIURL(app.listen)
	waitUI(uiURL)
	if err := startChromeApp(uiURL); err != nil {
		log.Printf("chrome app window unavailable: %v", err)
		if runtime.GOOS == "windows" {
			if err := openWindowsURL(uiURL + "/"); err != nil {
				return err
			}
		} else if err := exec.Command("xdg-open", uiURL+"/").Start(); err != nil {
			return err
		}
	}
	<-ctx.Done()
	app.shutdown(context.Background())
	return nil
}
