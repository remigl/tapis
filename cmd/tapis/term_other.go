//go:build !linux && !windows

package main

import "os"

func interactive() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0 && fi.Name() != os.DevNull
}

func attachConsole() {}
