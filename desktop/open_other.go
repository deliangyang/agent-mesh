//go:build !windows

package main

func openWindowsURL(string) error { return nil }
