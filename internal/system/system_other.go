//go:build !windows && !linux

package system

import "errors"

var errUnknown = errors.New("unknown action")

func lock() error  { return errors.New("not supported on this platform") }
func sleep() error { return errors.New("not supported on this platform") }
