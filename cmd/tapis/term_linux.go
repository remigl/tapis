//go:build linux

package main

import (
	"syscall"
	"unsafe"
)

// interactive reports whether stdin is a terminal someone can type in.
func interactive() bool {
	var t syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, 0, syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return e == 0
}

func attachConsole() {}

// terminalOut reports whether standard output is a terminal.
func terminalOut() bool {
	var t syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, 1, syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return e == 0
}
