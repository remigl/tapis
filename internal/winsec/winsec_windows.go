//go:build windows

package winsec

import (
	"path/filepath"
	"syscall"
	"unsafe"
)

// Only load DLLs from System32: a malicious DLL dropped next to tapis.exe
// (for example in the Downloads folder) must never be loaded (DLL planting).
func init() {
	k := syscall.NewLazyDLL("kernel32.dll") // KnownDLL, always from System32
	if p := k.NewProc("SetDefaultDllDirectories"); p.Find() == nil {
		p.Call(0x00000800) // LOAD_LIBRARY_SEARCH_SYSTEM32
	}
}

// SystemDir returns the System32 directory, without trusting the environment.
func SystemDir() string {
	buf := make([]uint16, 260)
	k := syscall.NewLazyDLL("kernel32.dll").NewProc("GetSystemDirectoryW")
	n, _, _ := k.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || int(n) > len(buf) {
		return `C:\Windows\System32`
	}
	return syscall.UTF16ToString(buf[:n])
}

// PowerShell returns the absolute path of Windows PowerShell, so that a
// "powershell.exe" placed earlier in PATH can never be run instead.
func PowerShell() string {
	return filepath.Join(SystemDir(), "WindowsPowerShell", "v1.0", "powershell.exe")
}
