// Package winui is Tapis' Windows user interface: an icon in the
// notification area (system tray) with a menu, a window that shows the
// pairing QR code, and message boxes. It talks to Win32 directly and has
// no dependency. On other systems every function is a no-op.
package winui

import (
	_ "embed"

	"github.com/remigl/tapis/internal/qr"
)

//go:embed tapis.ico
var iconICO []byte

// Pairing is what the QR window shows.
type Pairing struct {
	URL   string
	Other []string // addresses on other networks
	Code  *qr.Code
}

// Options configure the tray application.
type Options struct {
	French bool
	// NewPairing creates a fresh pairing code (called from the menu).
	NewPairing func() Pairing
	// Tooltip returns the text shown when hovering the icon.
	Tooltip func() string
	// Autostart and Lockscreen report and change the two toggles of the menu.
	AutostartEnabled func() bool
	SetAutostart     func(bool) error
	LockscreenOn     func() bool
	SetLockscreen    func(bool) error
	OpenLog          func()
	OpenHelp         func()
	Quit             func()
	// Ready is called once the icon is shown.
	Ready func()
}

// T returns the French or English text.
func (o *Options) T(fr, en string) string {
	if o.French {
		return fr
	}
	return en
}
