//go:build linux

package input

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Linux input event constants (linux/input-event-codes.h, linux/uinput.h).
const (
	evSyn = 0x00
	evKey = 0x01
	evRel = 0x02

	relX           = 0x00
	relY           = 0x01
	relHWheel      = 0x06
	relWheel       = 0x08
	relWheelHiRes  = 0x0b
	relHWheelHiRes = 0x0c

	btnLeft   = 0x110
	btnRight  = 0x111
	btnMiddle = 0x112

	keyLeftCtrl  = 29
	keyLeftShift = 42
	keyLeftAlt   = 56
	keyRightAlt  = 100 // AltGr
	keyLeftMeta  = 125

	uiSetEvBit   = 0x40045564
	uiSetKeyBit  = 0x40045565
	uiSetRelBit  = 0x40045566
	uiDevSetup   = 0x405c5503
	uiDevCreate  = 0x5501
	uiDevDestroy = 0x5502
)

var linuxNamed = map[string]uint16{
	"Escape": 1, "Backspace": 14, "Tab": 15, "Enter": 28, "Space": 57, "CapsLock": 58,
	"F1": 59, "F2": 60, "F3": 61, "F4": 62, "F5": 63, "F6": 64, "F7": 65, "F8": 66, "F9": 67, "F10": 68,
	"F11": 87, "F12": 88, "PrintScreen": 99, "Home": 102, "ArrowUp": 103, "PageUp": 104,
	"ArrowLeft": 105, "ArrowRight": 106, "End": 107, "ArrowDown": 108, "PageDown": 109,
	"Insert": 110, "Delete": 111, "VolumeMute": 113, "VolumeDown": 114, "VolumeUp": 115,
	"ContextMenu": 127, "MediaNext": 163, "MediaPlayPause": 164, "MediaPrev": 165,
	"ctrl": keyLeftCtrl, "shift": keyLeftShift, "alt": keyLeftAlt, "meta": keyLeftMeta,
}

type uinput struct {
	mu       sync.Mutex
	f        *os.File
	layout   *Layout
	wheelAcc [2]int // low-resolution wheel remainders (x, y)
	held     map[uint16]bool
}

func ioctl(fd uintptr, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg); e != 0 {
		return e
	}
	return nil
}

// DetectLayout guesses the system keyboard layout from common config files.
func DetectLayout() string {
	for _, path := range []string{"/etc/default/keyboard", "/etc/vconsole.conf", "/etc/X11/xorg.conf.d/00-keyboard.conf"} {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			var v string
			switch {
			case strings.HasPrefix(line, "XKBLAYOUT="):
				v = strings.TrimPrefix(line, "XKBLAYOUT=")
			case strings.HasPrefix(line, "KEYMAP="):
				v = strings.TrimPrefix(line, "KEYMAP=")
			case strings.HasPrefix(line, `Option "XkbLayout"`):
				v = strings.TrimPrefix(line, `Option "XkbLayout"`)
			default:
				continue
			}
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			v = strings.SplitN(v, ",", 2)[0]
			v = strings.SplitN(v, "-", 2)[0] // e.g. KEYMAP=be-latin1
			if _, ok := layoutDefs[v]; ok {
				f.Close()
				return v
			}
		}
		f.Close()
	}
	return "us"
}

// New creates the Linux uinput backend.
func New(opts Options) (Injector, error) {
	name := opts.Layout
	if name == "" || name == "auto" {
		name = DetectLayout()
	}
	layout, err := LoadLayout(name)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return nil, fmt.Errorf("cannot open /dev/uinput: permission denied.\n" +
				"Install the udev rule shipped in packaging/linux (see README) and log in again")
		}
		return nil, fmt.Errorf("cannot open /dev/uinput (%w); is the uinput module loaded? try: sudo modprobe uinput", err)
	}
	fd := f.Fd()
	set := func(req uintptr, v uintptr) {
		if err == nil {
			err = ioctl(fd, req, v)
		}
	}
	set(uiSetEvBit, evKey)
	set(uiSetEvBit, evRel)
	set(uiSetEvBit, evSyn)
	for k := uintptr(1); k < 256; k++ {
		set(uiSetKeyBit, k)
	}
	for _, b := range []uintptr{btnLeft, btnRight, btnMiddle} {
		set(uiSetKeyBit, b)
	}
	for _, r := range []uintptr{relX, relY, relWheel, relHWheel, relWheelHiRes, relHWheelHiRes} {
		set(uiSetRelBit, r)
	}
	// struct uinput_setup { struct input_id id; char name[80]; __u32 ff_effects_max; }
	var setup [92]byte
	binary.LittleEndian.PutUint16(setup[0:], 0x06)   // BUS_VIRTUAL
	binary.LittleEndian.PutUint16(setup[2:], 0x7461) // vendor "ta"
	binary.LittleEndian.PutUint16(setup[4:], 0x7069) // product "pi"
	binary.LittleEndian.PutUint16(setup[6:], 1)
	copy(setup[8:88], "Tapis virtual trackpad and keyboard")
	set(uiDevSetup, uintptr(unsafe.Pointer(&setup[0])))
	set(uiDevCreate, 0)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("creating uinput device: %w", err)
	}
	// Give udev and the compositor a moment to pick up the new device.
	time.Sleep(300 * time.Millisecond)
	return &uinput{f: f, layout: layout, held: map[uint16]bool{}}, nil
}

var timevalSize = int(unsafe.Sizeof(syscall.Timeval{}))

func (u *uinput) emit(buf []byte, typ, code uint16, val int32) []byte {
	ev := make([]byte, timevalSize+8)
	binary.LittleEndian.PutUint16(ev[timevalSize:], typ)
	binary.LittleEndian.PutUint16(ev[timevalSize+2:], code)
	binary.LittleEndian.PutUint32(ev[timevalSize+4:], uint32(val))
	return append(buf, ev...)
}

func (u *uinput) flush(buf []byte) error {
	buf = u.emit(buf, evSyn, 0, 0)
	_, err := u.f.Write(buf)
	return err
}

func (u *uinput) Move(dx, dy int) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	var b []byte
	if dx != 0 {
		b = u.emit(b, evRel, relX, int32(dx))
	}
	if dy != 0 {
		b = u.emit(b, evRel, relY, int32(dy))
	}
	return u.flush(b)
}

func (u *uinput) Scroll(dx, dy int) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	var b []byte
	axis := func(v int, acc *int, hi, lo uint16) {
		if v == 0 {
			return
		}
		b = u.emit(b, evRel, hi, int32(v))
		*acc += v
		if n := *acc / 120; n != 0 {
			b = u.emit(b, evRel, lo, int32(n))
			*acc -= n * 120
		}
	}
	axis(dy, &u.wheelAcc[1], relWheelHiRes, relWheel)
	axis(dx, &u.wheelAcc[0], relHWheelHiRes, relHWheel)
	return u.flush(b)
}

func (u *uinput) Button(btn Button, down bool) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	code := map[Button]uint16{Left: btnLeft, Right: btnRight, Middle: btnMiddle}[btn]
	return u.flush(u.emit(nil, evKey, code, b2i(down)))
}

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

func levelMods(level uint8) []uint16 {
	switch level {
	case 2:
		return []uint16{keyLeftShift}
	case 3:
		return []uint16{keyRightAlt}
	case 4:
		return []uint16{keyLeftShift, keyRightAlt}
	}
	return nil
}

func (u *uinput) keyEvents(b []byte, st stroke, down bool) []byte {
	mods := levelMods(st.level)
	if down {
		for _, m := range mods {
			if !u.held[m] {
				b = u.emit(b, evKey, m, 1)
			}
		}
		return u.emit(b, evKey, st.code, 1)
	}
	b = u.emit(b, evKey, st.code, 0)
	for _, m := range mods {
		if !u.held[m] {
			b = u.emit(b, evKey, m, 0)
		}
	}
	return b
}

func (u *uinput) Key(name string, down bool) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if code, ok := linuxNamed[name]; ok {
		if Modifiers[name] {
			u.held[code] = down
		}
		return u.flush(u.emit(nil, evKey, code, b2i(down)))
	}
	r := []rune(name)
	if len(r) != 1 {
		return fmt.Errorf("unknown key %q", name)
	}
	seq := u.layout.Lookup(r[0])
	if seq == nil && r[0] >= 'A' && r[0] <= 'Z' {
		seq = u.layout.Lookup(r[0] + 32)
	}
	if len(seq) != 1 {
		if down {
			return u.typeRune(r[0])
		}
		return nil
	}
	return u.flush(u.keyEvents(nil, seq[0], down))
}

func (u *uinput) tap(st stroke) error {
	if err := u.flush(u.keyEvents(nil, st, true)); err != nil {
		return err
	}
	err := u.flush(u.keyEvents(nil, st, false))
	// Some applications drop events that arrive too quickly.
	time.Sleep(2 * time.Millisecond)
	return err
}

func (u *uinput) typeRune(r rune) error {
	switch r {
	case '\n', '\r':
		return u.tap(stroke{28, 1})
	case '\t':
		return u.tap(stroke{15, 1})
	}
	if seq := u.layout.Lookup(r); seq != nil {
		for _, st := range seq {
			if err := u.tap(st); err != nil {
				return err
			}
		}
		return nil
	}
	// Fallback: GTK/Qt/IBus Unicode entry (Ctrl+Shift+U, hex code, Space).
	b := u.emit(nil, evKey, keyLeftCtrl, 1)
	b = u.emit(b, evKey, keyLeftShift, 1)
	b = u.emit(b, evKey, 22, 1) // KEY_U
	b = u.emit(b, evKey, 22, 0)
	b = u.emit(b, evKey, keyLeftShift, 0)
	b = u.emit(b, evKey, keyLeftCtrl, 0)
	if err := u.flush(b); err != nil {
		return err
	}
	for _, h := range fmt.Sprintf("%x", r) {
		if seq := u.layout.Lookup(h); len(seq) == 1 {
			if err := u.tap(seq[0]); err != nil {
				return err
			}
		}
	}
	return u.tap(stroke{57, 1})
}

func (u *uinput) Type(text string) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	for _, r := range text {
		if err := u.typeRune(r); err != nil {
			return err
		}
	}
	return nil
}

func (u *uinput) Close() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	ioctl(u.f.Fd(), uiDevDestroy, 0)
	return u.f.Close()
}
