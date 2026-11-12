//go:build windows

package input

import (
	"fmt"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var (
	user32            = syscall.NewLazyDLL("user32.dll")
	procSendInput     = user32.NewProc("SendInput")
	procVkKeyScanW    = user32.NewProc("VkKeyScanW")
	procMapVirtualKey = user32.NewProc("MapVirtualKeyW")
)

const (
	inputMouse    = 0
	inputKeyboard = 1

	mouseMove       = 0x0001
	mouseLeftDown   = 0x0002
	mouseLeftUp     = 0x0004
	mouseRightDown  = 0x0008
	mouseRightUp    = 0x0010
	mouseMiddleDown = 0x0020
	mouseMiddleUp   = 0x0040
	mouseWheel      = 0x0800
	mouseHWheel     = 0x1000

	keyExtended = 0x0001
	keyUp       = 0x0002
	keyUnicode  = 0x0004

	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12
	vkReturn  = 0x0D
	vkTab     = 0x09
)

// INPUT with a MOUSEINPUT payload.
type mouseInput struct {
	typ uint32
	mi  struct {
		dx, dy                 int32
		mouseData, flags, time uint32
		extra                  uintptr
	}
}

// INPUT with a KEYBDINPUT payload, padded to the size of the union.
type keyInput struct {
	typ uint32
	ki  struct {
		vk, scan    uint16
		flags, time uint32
		extra       uintptr
	}
	_ [8]byte
}

func init() {
	if unsafe.Sizeof(mouseInput{}) != unsafe.Sizeof(keyInput{}) {
		panic("INPUT struct size mismatch")
	}
}

type vkDef struct {
	vk       uint16
	extended bool
}

var winNamed = map[string]vkDef{
	"Enter": {0x0D, false}, "Backspace": {0x08, false}, "Tab": {0x09, false}, "Escape": {0x1B, false},
	"Space": {0x20, false}, "PageUp": {0x21, true}, "PageDown": {0x22, true}, "End": {0x23, true},
	"Home": {0x24, true}, "ArrowLeft": {0x25, true}, "ArrowUp": {0x26, true}, "ArrowRight": {0x27, true},
	"ArrowDown": {0x28, true}, "PrintScreen": {0x2C, true}, "Insert": {0x2D, true}, "Delete": {0x2E, true},
	"ContextMenu": {0x5D, true}, "CapsLock": {0x14, false},
	"VolumeMute": {0xAD, true}, "VolumeDown": {0xAE, true}, "VolumeUp": {0xAF, true},
	"MediaNext": {0xB0, true}, "MediaPrev": {0xB1, true}, "MediaPlayPause": {0xB3, true},
	"ctrl": {vkControl, false}, "shift": {vkShift, false}, "alt": {vkMenu, false}, "meta": {0x5B, true},
}

func init() {
	for i := 1; i <= 12; i++ {
		winNamed[fmt.Sprintf("F%d", i)] = vkDef{uint16(0x6F + i), false}
	}
}

type sendInput struct {
	mu   sync.Mutex
	held map[uint16]bool
}

// New creates the Windows SendInput backend. Layout is ignored: text is
// sent as Unicode, which works with every keyboard layout.
func New(opts Options) (Injector, error) {
	if err := procSendInput.Find(); err != nil {
		return nil, err
	}
	return &sendInput{held: map[uint16]bool{}}, nil
}

func sendMouse(in []mouseInput) error {
	if len(in) == 0 {
		return nil
	}
	n, _, err := procSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
	if int(n) != len(in) {
		return fmt.Errorf("SendInput: %v (blocked by UIPI or a secure desktop?)", err)
	}
	return nil
}

func sendKeys(in []keyInput) error {
	if len(in) == 0 {
		return nil
	}
	n, _, err := procSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
	if int(n) != len(in) {
		return fmt.Errorf("SendInput: %v (blocked by UIPI or a secure desktop?)", err)
	}
	return nil
}

func mouse(flags uint32, dx, dy int32, data uint32) mouseInput {
	var m mouseInput
	m.typ = inputMouse
	m.mi.dx, m.mi.dy, m.mi.flags, m.mi.mouseData = dx, dy, flags, data
	return m
}

func vkEvent(vk uint16, extended, down bool) keyInput {
	var k keyInput
	k.typ = inputKeyboard
	k.ki.vk = vk
	sc, _, _ := procMapVirtualKey.Call(uintptr(vk), 0)
	k.ki.scan = uint16(sc)
	if extended {
		k.ki.flags |= keyExtended
	}
	if !down {
		k.ki.flags |= keyUp
	}
	return k
}

func unicodeEvent(unit uint16, down bool) keyInput {
	var k keyInput
	k.typ = inputKeyboard
	k.ki.scan = unit
	k.ki.flags = keyUnicode
	if !down {
		k.ki.flags |= keyUp
	}
	return k
}

func (s *sendInput) Move(dx, dy int) error {
	return sendMouse([]mouseInput{mouse(mouseMove, int32(dx), int32(dy), 0)})
}

func (s *sendInput) Scroll(dx, dy int) error {
	var in []mouseInput
	if dy != 0 {
		in = append(in, mouse(mouseWheel, 0, 0, uint32(int32(dy))))
	}
	if dx != 0 {
		in = append(in, mouse(mouseHWheel, 0, 0, uint32(int32(dx))))
	}
	return sendMouse(in)
}

func (s *sendInput) Button(b Button, down bool) error {
	flags := map[Button][2]uint32{
		Left:   {mouseLeftDown, mouseLeftUp},
		Right:  {mouseRightDown, mouseRightUp},
		Middle: {mouseMiddleDown, mouseMiddleUp},
	}[b]
	f := flags[1]
	if down {
		f = flags[0]
	}
	return sendMouse([]mouseInput{mouse(f, 0, 0, 0)})
}

func (s *sendInput) Key(name string, down bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := winNamed[name]; ok {
		if Modifiers[name] {
			s.held[d.vk] = down
		}
		return sendKeys([]keyInput{vkEvent(d.vk, d.extended, down)})
	}
	r := []rune(name)
	if len(r) != 1 {
		return fmt.Errorf("unknown key %q", name)
	}
	res, _, _ := procVkKeyScanW.Call(uintptr(r[0]))
	if int16(res) == -1 {
		if down {
			return s.typeLocked(name)
		}
		return nil
	}
	vk, shift := uint16(res&0xFF), (res>>8)&1 == 1
	var in []keyInput
	needShift := shift && !s.held[vkShift]
	if down {
		if needShift {
			in = append(in, vkEvent(vkShift, false, true))
		}
		in = append(in, vkEvent(vk, false, true))
	} else {
		in = append(in, vkEvent(vk, false, false))
		if needShift {
			in = append(in, vkEvent(vkShift, false, false))
		}
	}
	return sendKeys(in)
}

func (s *sendInput) typeLocked(text string) error {
	var in []keyInput
	for _, r := range text {
		switch r {
		case '\n', '\r':
			in = append(in, vkEvent(vkReturn, false, true), vkEvent(vkReturn, false, false))
			continue
		case '\t':
			in = append(in, vkEvent(vkTab, false, true), vkEvent(vkTab, false, false))
			continue
		}
		for _, u := range utf16.Encode([]rune{r}) {
			in = append(in, unicodeEvent(u, true), unicodeEvent(u, false))
		}
	}
	return sendKeys(in)
}

func (s *sendInput) Type(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.typeLocked(text)
}

func (s *sendInput) Close() error { return nil }
