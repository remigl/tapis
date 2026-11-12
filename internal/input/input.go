// Package input injects mouse and keyboard events into the operating system.
//
// Backends:
//   - Windows: SendInput (user32.dll), Unicode text via KEYEVENTF_UNICODE.
//   - Linux:   a virtual device created through /dev/uinput. Works on X11 and
//     on every Wayland compositor, because events come from the kernel.
package input

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

// Button is a mouse button.
type Button int

const (
	Left Button = iota
	Right
	Middle
)

// ParseButton converts a protocol name to a Button.
func ParseButton(s string) (Button, bool) {
	switch s {
	case "left":
		return Left, true
	case "right":
		return Right, true
	case "middle":
		return Middle, true
	}
	return 0, false
}

func (b Button) String() string { return [...]string{"left", "right", "middle"}[b] }

// Injector is implemented by each platform backend.
type Injector interface {
	// Move moves the pointer by a relative amount, in pixels.
	Move(dx, dy int) error
	// Scroll scrolls by a relative amount, where 120 units equal one wheel
	// notch. Positive dy scrolls up, positive dx scrolls right.
	Scroll(dx, dy int) error
	// Button presses or releases a mouse button.
	Button(b Button, down bool) error
	// Key presses or releases a named key (see NamedKeys), a modifier
	// ("ctrl", "shift", "alt", "meta") or a single character.
	Key(name string, down bool) error
	// Type types a string of text.
	Type(text string) error
	Close() error
}

// Options configure a backend.
type Options struct {
	// Layout is the keyboard layout of the computer (Linux only, e.g. "be").
	Layout string
}

// Modifiers accepted by Key.
var Modifiers = map[string]bool{"ctrl": true, "shift": true, "alt": true, "meta": true}

// NamedKeys is the list of non-character keys understood by every backend.
var NamedKeys = map[string]bool{}

func init() {
	for _, k := range strings.Fields(`Enter Backspace Tab Escape Space Delete Insert Home End PageUp
		PageDown ArrowLeft ArrowRight ArrowUp ArrowDown PrintScreen ContextMenu CapsLock
		VolumeUp VolumeDown VolumeMute MediaPlayPause MediaNext MediaPrev
		F1 F2 F3 F4 F5 F6 F7 F8 F9 F10 F11 F12`) {
		NamedKeys[k] = true
	}
}

// ValidKey reports whether name can be passed to Injector.Key.
func ValidKey(name string) bool {
	if NamedKeys[name] || Modifiers[name] {
		return true
	}
	return len([]rune(name)) == 1
}

// Logger is a backend that only prints events. Used by --dry-run and tests.
type Logger struct {
	mu sync.Mutex
	W  io.Writer
}

func (l *Logger) log(f string, a ...any) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.W, "[input] "+f+"\n", a...)
	return nil
}
func (l *Logger) Move(dx, dy int) error   { return l.log("move %d %d", dx, dy) }
func (l *Logger) Scroll(dx, dy int) error { return l.log("scroll %d %d", dx, dy) }
func (l *Logger) Button(b Button, down bool) error {
	return l.log("button %s %s", b, upDown(down))
}
func (l *Logger) Key(name string, down bool) error { return l.log("key %q %s", name, upDown(down)) }
func (l *Logger) Type(text string) error           { return l.log("type %q", text) }
func (l *Logger) Close() error                     { return nil }

func upDown(down bool) string {
	if down {
		return "down"
	}
	return "up"
}
