//go:build windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// pauseIfDoubleClicked keeps the console window open after an error when
// Tapis was started by double-clicking it (the window would otherwise
// close before the message can be read).
func pauseIfDoubleClicked() {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleProcessList")
	var ids [4]uint32
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&ids[0])), 4)
	if n == 1 { // we are the only process attached to this console
		fmt.Print("\nPress Enter to close this window...")
		bufio.NewReader(os.Stdin).ReadString('\n')
	}
}
