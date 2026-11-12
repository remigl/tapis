//go:build !linux && !windows

package input

import (
	"errors"
	"runtime"
)

// New reports that this platform has no backend yet. Contributions welcome
// (macOS: CGEventPost via Quartz Event Services).
func New(opts Options) (Injector, error) {
	return nil, errors.New("input injection is not implemented on " + runtime.GOOS + " yet (use --dry-run to try the app)")
}
