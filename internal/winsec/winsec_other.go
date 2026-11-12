//go:build !windows

package winsec

// PowerShell is only meaningful on Windows.
func PowerShell() string { return "powershell" }
