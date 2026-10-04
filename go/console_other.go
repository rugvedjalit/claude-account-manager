//go:build !windows

package main

import (
	"os"
	"runtime"
)

func initConsole() {
	if fi, err := os.Stdout.Stat(); err == nil && fi.Mode()&os.ModeCharDevice == 0 {
		colorOn = false
	}
	if os.Getenv("NO_COLOR") != "" {
		colorOn = false
	}
}

func platformName() string { return runtime.GOOS }
