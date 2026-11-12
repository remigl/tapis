//go:build !windows

package lockscreen

import (
	"errors"
	"log"

	"github.com/remigl/tapis/internal/input"
)

// On Linux the virtual uinput device already reaches the lock screen,
// so no helper is needed.

func Installed() bool                                      { return false }
func Install() error                                       { return errors.New("not needed on this system") }
func Uninstall() error                                     { return nil }
func RunService()                                          {}
func RunAgent()                                            {}
func Wrap(in input.Injector, _ *log.Logger) input.Injector { return in }

var _ = event{}
