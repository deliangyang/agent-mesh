//go:build !windows

package main

import (
	"context"
	"log/slog"
)

func runWindowsTray(ctx context.Context, cancel context.CancelFunc, uiURL string) bool {
	return false
}

func withGUILog(log *slog.Logger) *slog.Logger { return log }
