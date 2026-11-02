//go:build windows

package main

import (
	"fmt"

	"golang.org/x/sys/windows"
)

func openWindowsURL(url string) error {
	p, err := windows.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	if err := windows.ShellExecute(0, nil, p, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		return fmt.Errorf("desktop ui: shell execute: %w", err)
	}
	return nil
}
