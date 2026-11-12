//go:build windows

package focus

import (
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

var (
	ole32         = syscall.NewLazyDLL("ole32.dll")
	oleaut32      = syscall.NewLazyDLL("oleaut32.dll")
	pCoInit       = ole32.NewProc("CoInitializeEx")
	pCoCreate     = ole32.NewProc("CoCreateInstance")
	pVariantClear = oleaut32.NewProc("VariantClear")
)

type guid struct {
	d1     uint32
	d2, d3 uint16
	d4     [8]byte
}

var (
	clsidCUIAutomation = guid{0xff48dba4, 0x60ef, 0x4201, [8]byte{0xaa, 0x87, 0x54, 0x10, 0x3e, 0xef, 0x59, 0x4e}}
	iidIUIAutomation   = guid{0x30cbe57d, 0xd9d0, 0x452a, [8]byte{0xab, 0x13, 0x7a, 0xc5, 0xac, 0x48, 0x25, 0xee}}
)

// comObj is a COM interface pointer: its first word points to the vtable.
type comObj struct{ vtbl *[64]uintptr }

func (o *comObj) call(method int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(o.vtbl[method], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return r
}

func (o *comObj) release() { o.call(2) }

// VARIANT (24 bytes on 64-bit Windows, 16 on 32-bit).
type variant struct {
	vt       uint16
	reserved [3]uint16
	val      [16]byte
}

const (
	// vtable indexes (UIAutomationClient.h)
	uiaGetFocusedElement      = 8  // IUIAutomation
	elemGetCurrentPropertyVal = 10 // IUIAutomationElement

	propControlType         = 30003
	propIsKeyboardFocusable = 30009
	propIsValuePatternAvail = 30043
	propValueIsReadOnly     = 30046
	ctButton                = 50000
	ctCheckBox              = 50002
	ctComboBox              = 50003
	ctEdit                  = 50004
	ctListItem              = 50007
	ctMenuItem              = 50011
	ctTreeItem              = 50024
	ctDataItem              = 50029
	ctDocument              = 50030
	vtEmpty                 = 0
	vtI4                    = 3
	vtBool                  = 11
)

type request struct{ reply chan bool }

type probe struct{ req chan request }

// All UI Automation calls happen on one OS thread, in the COM
// multithreaded apartment.
func newProbe() Probe {
	p := &probe{req: make(chan request)}
	ready := make(chan bool)
	go func() {
		runtime.LockOSThread()
		pCoInit.Call(0, 0) // COINIT_MULTITHREADED
		var uia *comObj
		if r, _, _ := pCoCreate.Call(uintptr(unsafe.Pointer(&clsidCUIAutomation)), 0, 1, // CLSCTX_INPROC_SERVER
			uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&uia))); r != 0 || uia == nil {
			ready <- false
			return
		}
		ready <- true
		for rq := range p.req {
			rq.reply <- editable(uia)
		}
		uia.release()
	}()
	if !<-ready {
		return nil
	}
	return p
}

func property(el *comObj, id int) (uint16, int32) {
	var v variant
	if el.call(elemGetCurrentPropertyVal, uintptr(id), uintptr(unsafe.Pointer(&v))) != 0 {
		return vtEmpty, 0
	}
	defer pVariantClear.Call(uintptr(unsafe.Pointer(&v)))
	switch v.vt {
	case vtI4:
		return vtI4, *(*int32)(unsafe.Pointer(&v.val[0]))
	case vtBool:
		return vtBool, int32(*(*int16)(unsafe.Pointer(&v.val[0])))
	}
	return v.vt, 0
}

func editable(uia *comObj) bool {
	var el *comObj
	if uia.call(uiaGetFocusedElement, uintptr(unsafe.Pointer(&el))) != 0 || el == nil {
		return false
	}
	defer el.release()
	_, ct := property(el, propControlType)
	vtRO, ro := property(el, propValueIsReadOnly)
	readOnly := vtRO == vtBool && ro != 0
	switch ct {
	case ctEdit:
		return !readOnly
	case ctButton, ctCheckBox, ctComboBox, ctListItem, ctMenuItem, ctTreeItem, ctDataItem:
		return false
	}
	// Documents and custom widgets (rich editors, contenteditable areas)
	// count only if they expose an explicitly writable value.
	_, hasValue := property(el, propIsValuePatternAvail)
	_, focusable := property(el, propIsKeyboardFocusable)
	return hasValue != 0 && focusable != 0 && vtRO == vtBool && !readOnly
}

// Editable asks the UI Automation thread; it gives up after 300 ms (an
// application that does not answer counts as "not a text field").
func (p *probe) Editable() bool {
	rq := request{reply: make(chan bool, 1)}
	select {
	case p.req <- rq:
	case <-time.After(300 * time.Millisecond):
		return false
	}
	select {
	case v := <-rq.reply:
		return v
	case <-time.After(300 * time.Millisecond):
		return false
	}
}

// Close is a no-op: the probe lives as long as Tapis.
func (p *probe) Close() {}
