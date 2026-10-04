//go:build windows

package main

import (
	"os"
	"syscall"
)

var (
	kernel32DLL              = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode       = kernel32DLL.NewProc("GetConsoleMode")
	procSetConsoleMode       = kernel32DLL.NewProc("SetConsoleMode")
	procSetConsoleOutputCP   = kernel32DLL.NewProc("SetConsoleOutputCP")
)

// initConsole turns on ANSI colour handling and UTF-8 output in the Windows console.
func initConsole() {
	const enableVirtualTerminalProcessing = 0x0004
	h := syscall.Handle(os.Stdout.Fd())
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(uintptr(h), uintptr(unsafePointer(&mode)))
	if r == 0 {
		// Not a console (redirected); keep colours off.
		colorOn = false
		return
	}
	r, _, _ = procSetConsoleMode.Call(uintptr(h), uintptr(mode|enableVirtualTerminalProcessing))
	if r == 0 {
		colorOn = false
	}
	procSetConsoleOutputCP.Call(uintptr(65001))
}

func platformName() string { return "windows" }
