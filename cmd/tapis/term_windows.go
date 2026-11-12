//go:build windows

package main

import (
	"os"
	"syscall"
)

// interactive reports whether stdin is a console someone can type in.
func interactive() bool {
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(os.Stdin.Fd()), &mode) == nil
}

// attachConsole lets the Windows GUI build print to the terminal it was
// started from (for commands like "tapis devices").
func attachConsole() {
	k := syscall.NewLazyDLL("kernel32.dll").NewProc("AttachConsole")
	if r, _, _ := k.Call(uintptr(0xFFFFFFFF)); r == 0 { // ATTACH_PARENT_PROCESS
		return
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout, os.Stderr = f, f
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = f
	}
}

// terminalOut reports whether standard output is a console.
func terminalOut() bool {
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(os.Stdout.Fd()), &mode) == nil
}
