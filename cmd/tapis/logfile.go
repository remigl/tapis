package main

import (
	"io"
	"os"
	"path/filepath"
)

// stdoutDiscarded reports whether standard output goes nowhere useful
// (/dev/null, or no console at all).
func stdoutDiscarded() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return true
	}
	return fi.Mode()&os.ModeCharDevice != 0 && !terminalOut()
}

// openLog returns the log file used when Tapis runs without a terminal.
func openLog(dir string) io.Writer {
	os.MkdirAll(dir, 0o700)
	p := filepath.Join(dir, "tapis.log")
	if fi, err := os.Stat(p); err == nil && fi.Size() > 1<<20 {
		os.Rename(p, p+".old")
	}
	f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return io.Discard
	}
	return f
}
