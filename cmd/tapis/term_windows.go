//go:build windows

package main

import "syscall"

// interactive reports whether stdin is a console someone can type in.
func interactive() bool {
	var mode uint32
	return syscall.GetConsoleMode(syscall.Stdin, &mode) == nil
}
