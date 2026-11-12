//go:build windows

package winui

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	pRegisterClassEx   = user32.NewProc("RegisterClassExW")
	pCreateWindowEx    = user32.NewProc("CreateWindowExW")
	pDefWindowProc     = user32.NewProc("DefWindowProcW")
	pDestroyWindow     = user32.NewProc("DestroyWindow")
	pGetMessage        = user32.NewProc("GetMessageW")
	pTranslateMessage  = user32.NewProc("TranslateMessage")
	pDispatchMessage   = user32.NewProc("DispatchMessageW")
	pPostMessage       = user32.NewProc("PostMessageW")
	pPostQuitMessage   = user32.NewProc("PostQuitMessage")
	pShowWindow        = user32.NewProc("ShowWindow")
	pUpdateWindow      = user32.NewProc("UpdateWindow")
	pSetForeground     = user32.NewProc("SetForegroundWindow")
	pCreatePopupMenu   = user32.NewProc("CreatePopupMenu")
	pAppendMenu        = user32.NewProc("AppendMenuW")
	pTrackPopupMenu    = user32.NewProc("TrackPopupMenu")
	pDestroyMenu       = user32.NewProc("DestroyMenu")
	pGetCursorPos      = user32.NewProc("GetCursorPos")
	pMessageBox        = user32.NewProc("MessageBoxW")
	pLoadCursor        = user32.NewProc("LoadCursorW")
	pBeginPaint        = user32.NewProc("BeginPaint")
	pEndPaint          = user32.NewProc("EndPaint")
	pFillRect          = user32.NewProc("FillRect")
	pDrawText          = user32.NewProc("DrawTextW")
	pGetClientRect     = user32.NewProc("GetClientRect")
	pInvalidateRect    = user32.NewProc("InvalidateRect")
	pGetSystemMetrics  = user32.NewProc("GetSystemMetrics")
	pFindWindow        = user32.NewProc("FindWindowW")
	pRegisterWinMsg    = user32.NewProc("RegisterWindowMessageW")
	pSetProcessDPI     = user32.NewProc("SetProcessDPIAware")
	pGetDpiForSystem   = user32.NewProc("GetDpiForSystem")
	pLookupIconID      = user32.NewProc("LookupIconIdFromDirectoryEx")
	pCreateIconFromRes = user32.NewProc("CreateIconFromResourceEx")
	pShellNotifyIcon   = shell32.NewProc("Shell_NotifyIconW")
	pShellExecute      = shell32.NewProc("ShellExecuteW")
	pGetStockObject    = gdi32.NewProc("GetStockObject")
	pCreateFont        = gdi32.NewProc("CreateFontW")
	pSelectObject      = gdi32.NewProc("SelectObject")
	pSetBkMode         = gdi32.NewProc("SetBkMode")
	pSetTextColor      = gdi32.NewProc("SetTextColor")
	pCreateSolidBrush  = gdi32.NewProc("CreateSolidBrush")
	pCreateMutex       = kernel32.NewProc("CreateMutexW")
	pGetModuleHandle   = kernel32.NewProc("GetModuleHandleW")
	pGetUILanguage     = kernel32.NewProc("GetUserDefaultUILanguage")
	pRegOpenKeyEx      = advapi32.NewProc("RegOpenKeyExW")
	pRegSetValueEx     = advapi32.NewProc("RegSetValueExW")
	pRegQueryValueEx   = advapi32.NewProc("RegQueryValueExW")
	pRegDeleteValue    = advapi32.NewProc("RegDeleteValueW")
	pRegCloseKey       = advapi32.NewProc("RegCloseKey")
)

const (
	wmDestroy       = 0x0002
	wmPaint         = 0x000F
	wmClose         = 0x0010
	wmCommand       = 0x0111
	wmLButtonUp     = 0x0202
	wmRButtonUp     = 0x0205
	wmApp           = 0x8000
	wmTray          = wmApp + 1 // tray icon events
	wmInvoke        = wmApp + 2 // run queued functions on the UI thread
	wmShowQRExt     = wmApp + 3 // sent by a second instance
	nimAdd          = 0
	nimModify       = 1
	nimDelete       = 2
	nifMessage      = 0x1
	nifIcon         = 0x2
	nifTip          = 0x4
	nifInfo         = 0x10
	mfString        = 0x0
	mfChecked       = 0x8
	mfSeparator     = 0x800
	mfGrayed        = 0x1
	tpmReturnCmd    = 0x100
	tpmRightBtn     = 0x2
	mbYesNo         = 0x4
	mbIconError     = 0x10
	mbIconQuest     = 0x20
	mbIconInfo      = 0x40
	mbSystemModal   = 0x1000
	mbSetFore       = 0x10000
	mbTopmost       = 0x40000
	idYes           = 6
	wsOverlapped    = 0x00C80000 // caption + system menu + border
	wsMinimizeBox   = 0x00020000
	swShow          = 5
	dtCenter        = 0x1
	dtWordBreak     = 0x10
	dtNoPrefix      = 0x800
	hkeyCurrentUser = 0x80000001
	keyRead         = 0x20019
	keyWrite        = 0x20006
	regSZ           = 1

	cmdShowQR     = 1
	cmdAutostart  = 2
	cmdLockscreen = 3
	cmdLog        = 4
	cmdHelp       = 5
	cmdQuit       = 6

	trayClass = "TapisTray"
	qrClass   = "TapisQR"
)

type point struct{ x, y int32 }
type rect struct{ left, top, right, bottom int32 }

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     uintptr
}

type paintStruct struct {
	hdc      uintptr
	erase    int32
	rc       rect
	restore  int32
	incUpd   int32
	reserved [32]byte
}

type notifyIconData struct {
	size        uint32
	hwnd        uintptr
	id          uint32
	flags       uint32
	callbackMsg uint32
	icon        uintptr
	tip         [128]uint16
	state       uint32
	stateMask   uint32
	info        [256]uint16
	version     uint32
	infoTitle   [64]uint16
	infoFlags   uint32
	guid        [16]byte
	balloonIcon uintptr
}

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

func copyU16(dst []uint16, s string) {
	u, _ := syscall.UTF16FromString(s)
	if len(u) > len(dst) {
		u = u[:len(dst)]
		u[len(u)-1] = 0
	}
	copy(dst, u)
}

// IsFrench reports whether Windows is displayed in French.
func IsFrench() bool {
	lang, _, _ := pGetUILanguage.Call()
	return lang&0x3FF == 0x0C
}

// MessageBox shows a message box and returns true for "Yes"/"OK".
func MessageBox(title, text string, question, isErr bool) bool {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	flags := uintptr(mbTopmost | mbSetFore | mbSystemModal)
	switch {
	case question:
		flags |= mbYesNo | mbIconQuest
	case isErr:
		flags |= mbIconError
	default:
		flags |= mbIconInfo
	}
	r, _, _ := pMessageBox.Call(0, uintptr(unsafe.Pointer(u16(text))), uintptr(unsafe.Pointer(u16(title))), flags)
	return r == idYes || (!question && r == 1)
}

// SignalRunningInstance returns true if Tapis is already running in this
// session; in that case it asks it to show the QR code window.
func SignalRunningInstance() bool {
	_, _, err := pCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(u16("Local\\TapisSingleInstance"))))
	if !errors.Is(err, syscall.ERROR_ALREADY_EXISTS) {
		return false // the mutex handle is kept until exit on purpose
	}
	if hwnd, _, _ := pFindWindow.Call(uintptr(unsafe.Pointer(u16(trayClass))), 0); hwnd != 0 {
		pPostMessage.Call(hwnd, wmShowQRExt, 0, 0)
	}
	return true
}

// ---- autostart (HKCU\Software\Microsoft\Windows\CurrentVersion\Run) ----

const runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func exePath() string {
	p, _ := os.Executable()
	return p
}

// AutostartPath returns the command registered to start with Windows, or "".
func AutostartPath() string {
	var key uintptr
	if r, _, _ := pRegOpenKeyEx.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(u16(runKey))), 0, keyRead, uintptr(unsafe.Pointer(&key))); r != 0 {
		return ""
	}
	defer pRegCloseKey.Call(key)
	var typ, size uint32
	name := u16("Tapis")
	if r, _, _ := pRegQueryValueEx.Call(key, uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&typ)), 0, uintptr(unsafe.Pointer(&size))); r != 0 || size == 0 {
		return ""
	}
	buf := make([]uint16, size/2+1)
	if r, _, _ := pRegQueryValueEx.Call(key, uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r != 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// SetAutostart registers (or removes) this executable to start with Windows.
func SetAutostart(on bool) error {
	var key uintptr
	if r, _, _ := pRegOpenKeyEx.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(u16(runKey))), 0, keyWrite, uintptr(unsafe.Pointer(&key))); r != 0 {
		return syscall.Errno(r)
	}
	defer pRegCloseKey.Call(key)
	if !on {
		pRegDeleteValue.Call(key, uintptr(unsafe.Pointer(u16("Tapis"))))
		return nil
	}
	val, _ := syscall.UTF16FromString(`"` + exePath() + `"`)
	if r, _, _ := pRegSetValueEx.Call(key, uintptr(unsafe.Pointer(u16("Tapis"))), 0, regSZ,
		uintptr(unsafe.Pointer(&val[0])), uintptr(len(val)*2)); r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

// RefreshAutostart points an existing autostart entry at this executable
// (useful after downloading a new version).
func RefreshAutostart() {
	if p := AutostartPath(); p != "" && p != `"`+exePath()+`"` {
		SetAutostart(true)
	}
}

// OpenPath opens a file, folder or URL with its default application.
func OpenPath(p string) {
	pShellExecute.Call(0, uintptr(unsafe.Pointer(u16("open"))), uintptr(unsafe.Pointer(u16(p))), 0, 0, 1)
}

// ---- the application ----

type app struct {
	opts       Options
	tray       uintptr
	qrWnd      uintptr
	icon       uintptr
	nid        notifyIconData
	taskbarMsg uintptr
	pairing    Pairing
	fontBig    uintptr
	fontText   uintptr
	fontSmall  uintptr
	scale      float64

	qmu   sync.Mutex
	queue []func()
}

var current *app

func loadIcon(size int) uintptr {
	off, _, _ := pLookupIconID.Call(uintptr(unsafe.Pointer(&iconICO[0])), 1, uintptr(size), uintptr(size), 0)
	if off == 0 || int(off) >= len(iconICO) {
		return 0
	}
	// Find the size of that image in the ICO directory.
	n := int(binary.LittleEndian.Uint16(iconICO[4:]))
	length := len(iconICO) - int(off)
	for i := 0; i < n; i++ {
		e := iconICO[6+16*i:]
		if int(binary.LittleEndian.Uint32(e[12:])) == int(off) {
			length = int(binary.LittleEndian.Uint32(e[8:]))
		}
	}
	h, _, _ := pCreateIconFromRes.Call(uintptr(unsafe.Pointer(&iconICO[off])), uintptr(length), 1, 0x00030000, uintptr(size), uintptr(size), 0)
	return h
}

func registerClass(name string, proc uintptr, icon uintptr) error {
	cursor, _, _ := pLoadCursor.Call(0, 32512)
	bg, _, _ := pGetStockObject.Call(0) // WHITE_BRUSH
	wc := wndClassEx{wndProc: proc, instance: inst(), cursor: cursor, background: bg, className: u16(name), icon: icon, iconSm: icon}
	wc.size = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return err
	}
	return nil
}

// Run shows the tray icon and runs the Windows message loop until Quit.
// It must be called once; it blocks.
func Run(opts Options) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pSetProcessDPI.Call()
	a := &app{opts: opts, scale: 1}
	if pGetDpiForSystem.Find() != nil {
		// Windows older than 10 (1607): keep scale 1
	} else if dpi, _, _ := pGetDpiForSystem.Call(); dpi > 0 {
		a.scale = float64(dpi) / 96
	}
	current = a
	a.icon = loadIcon(int(32 * a.scale))
	if err := registerClass(trayClass, syscall.NewCallback(a.trayProc), a.icon); err != nil {
		return err
	}
	if err := registerClass(qrClass, syscall.NewCallback(a.qrProc), a.icon); err != nil {
		return err
	}
	a.tray, _, _ = pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(u16(trayClass))), uintptr(unsafe.Pointer(u16("Tapis"))), 0, 0, 0, 0, 0, 0, 0, inst(), 0)
	if a.tray == 0 {
		return errors.New("cannot create the tray window")
	}
	a.taskbarMsg, _, _ = pRegisterWinMsg.Call(uintptr(unsafe.Pointer(u16("TaskbarCreated"))))
	small := loadIcon(int(16 * a.scale))
	if small == 0 {
		small = a.icon
	}
	a.nid = notifyIconData{hwnd: a.tray, id: 1, flags: nifMessage | nifIcon | nifTip, callbackMsg: wmTray, icon: small}
	a.nid.size = uint32(unsafe.Sizeof(a.nid))
	copyU16(a.nid.tip[:], "Tapis")
	pShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&a.nid)))
	defer pShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&a.nid)))
	mk := func(h, w int, name string) uintptr {
		f, _, _ := pCreateFont.Call(uintptr(-int32(float64(h)*a.scale)), 0, 0, 0, uintptr(w), 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(u16(name))))
		return f
	}
	a.fontBig, a.fontText, a.fontSmall = mk(22, 600, "Segoe UI"), mk(16, 400, "Segoe UI"), mk(13, 400, "Segoe UI")
	if opts.Ready != nil {
		go opts.Ready()
	}
	var m msg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return nil
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// invoke runs fn on the UI thread.
func invoke(fn func()) {
	a := current
	if a == nil || a.tray == 0 {
		return
	}
	a.qmu.Lock()
	a.queue = append(a.queue, fn)
	a.qmu.Unlock()
	pPostMessage.Call(a.tray, wmInvoke, 0, 0)
}

// ShowQR opens (or refreshes) the pairing window.
func ShowQR(p Pairing) { invoke(func() { current.showQR(p) }) }

// CloseQR closes the pairing window if it is open.
func CloseQR() {
	invoke(func() {
		if current.qrWnd != 0 {
			pDestroyWindow.Call(current.qrWnd)
		}
	})
}

// Notify shows a notification balloon from the tray icon.
func Notify(title, text string) {
	invoke(func() {
		a := current
		n := a.nid
		n.flags = nifInfo
		copyU16(n.info[:], text)
		copyU16(n.infoTitle[:], title)
		n.infoFlags = 0x4 | 0x20 // NIIF_USER | NIIF_LARGE_ICON: use the app icon
		pShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&n)))
	})
}

// Quit ends the message loop.
func Quit() { invoke(func() { pPostQuitMessage.Call(0) }) }

func (a *app) updateTip() {
	if a.opts.Tooltip == nil {
		return
	}
	n := a.nid
	n.flags = nifTip
	copyU16(n.tip[:], a.opts.Tooltip())
	pShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&n)))
}

func (a *app) menu() {
	o := &a.opts
	h, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(h)
	add := func(id int, text string, checked bool) {
		f := uintptr(mfString)
		if checked {
			f |= mfChecked
		}
		pAppendMenu.Call(h, f, uintptr(id), uintptr(unsafe.Pointer(u16(text))))
	}
	sep := func() { pAppendMenu.Call(h, mfSeparator, 0, 0) }
	info := func(text string) { pAppendMenu.Call(h, mfString|mfGrayed, 0, uintptr(unsafe.Pointer(u16(text)))) }
	if o.Connected != nil {
		names := o.Connected()
		switch len(names) {
		case 0:
			info(o.T("Aucun téléphone connecté", "No phone connected"))
		case 1:
			info(o.T("1 téléphone connecté :", "1 phone connected:"))
		default:
			info(fmt.Sprintf(o.T("%d téléphones connectés :", "%d phones connected:"), len(names)))
		}
		for _, n := range names {
			info("    " + n)
		}
		sep()
	}
	add(cmdShowQR, o.T("Afficher le QR code (associer un téléphone)", "Show the QR code (pair a phone)"), false)
	sep()
	add(cmdAutostart, o.T("Lancer au démarrage de Windows", "Start with Windows"), o.AutostartEnabled != nil && o.AutostartEnabled())
	if o.SetLockscreen != nil {
		add(cmdLockscreen, o.T("Taper sur l'écran de verrouillage", "Type on the lock screen"), o.LockscreenOn != nil && o.LockscreenOn())
	}
	sep()
	add(cmdLog, o.T("Ouvrir le journal", "Open the log"), false)
	add(cmdHelp, o.T("Aide et dépannage", "Help and troubleshooting"), false)
	sep()
	add(cmdQuit, o.T("Quitter Tapis", "Quit Tapis"), false)
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForeground.Call(a.tray) // required so the menu closes when clicking elsewhere
	cmd, _, _ := pTrackPopupMenu.Call(h, tpmReturnCmd|tpmRightBtn, uintptr(pt.x), uintptr(pt.y), 0, a.tray, 0)
	pPostMessage.Call(a.tray, 0, 0, 0)
	a.command(int(cmd))
}

func (a *app) command(cmd int) {
	o := &a.opts
	switch cmd {
	case cmdShowQR:
		if o.NewPairing != nil {
			a.showQR(o.NewPairing())
		}
	case cmdAutostart:
		if o.SetAutostart != nil {
			on := !(o.AutostartEnabled != nil && o.AutostartEnabled())
			if err := o.SetAutostart(on); err != nil {
				go MessageBox("Tapis", err.Error(), false, true)
			}
		}
	case cmdLockscreen:
		on := !(o.LockscreenOn != nil && o.LockscreenOn())
		go func() { // may show a UAC prompt: don't block the UI thread
			if err := o.SetLockscreen(on); err != nil {
				MessageBox("Tapis", err.Error(), false, true)
			}
		}()
	case cmdLog:
		if o.OpenLog != nil {
			o.OpenLog()
		}
	case cmdHelp:
		if o.OpenHelp != nil {
			o.OpenHelp()
		}
	case cmdQuit:
		if o.Quit != nil {
			go o.Quit()
		}
	}
}

func (a *app) trayProc(hwnd, m, wp, lp uintptr) uintptr {
	switch m {
	case wmTray:
		switch lp {
		case wmLButtonUp:
			a.command(cmdShowQR)
		case wmRButtonUp:
			a.updateTip()
			a.menu()
		}
		return 0
	case wmInvoke:
		a.qmu.Lock()
		q := a.queue
		a.queue = nil
		a.qmu.Unlock()
		for _, fn := range q {
			fn()
		}
		return 0
	case wmShowQRExt:
		a.command(cmdShowQR)
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	if a.taskbarMsg != 0 && m == a.taskbarMsg { // Explorer restarted
		pShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&a.nid)))
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, m, wp, lp)
	return r
}

func (a *app) px(v int) int32 { return int32(float64(v) * a.scale) }

func (a *app) showQR(p Pairing) {
	a.pairing = p
	if a.qrWnd != 0 {
		pInvalidateRect.Call(a.qrWnd, 0, 1)
		pShowWindow.Call(a.qrWnd, swShow)
		pSetForeground.Call(a.qrWnd)
		return
	}
	w, h := a.px(460), a.px(640)
	sw, _, _ := pGetSystemMetrics.Call(0)
	sh, _, _ := pGetSystemMetrics.Call(1)
	x, y := (int32(sw)-w)/2, (int32(sh)-h)/2
	title := a.opts.T("Tapis – associer un téléphone", "Tapis – pair a phone")
	a.qrWnd, _, _ = pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(u16(qrClass))), uintptr(unsafe.Pointer(u16(title))),
		wsOverlapped|wsMinimizeBox, uintptr(x), uintptr(y), uintptr(w), uintptr(h), 0, 0, inst(), 0)
	pShowWindow.Call(a.qrWnd, swShow)
	pUpdateWindow.Call(a.qrWnd)
	pSetForeground.Call(a.qrWnd)
}

func (a *app) drawText(hdc uintptr, font uintptr, color uint32, text string, r *rect) {
	pSelectObject.Call(hdc, font)
	pSetTextColor.Call(hdc, uintptr(color))
	pDrawText.Call(hdc, uintptr(unsafe.Pointer(u16(text))), ^uintptr(0), uintptr(unsafe.Pointer(r)), dtCenter|dtWordBreak|dtNoPrefix)
}

func (a *app) qrProc(hwnd, m, wp, lp uintptr) uintptr {
	switch m {
	case wmPaint:
		var ps paintStruct
		hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		var cr rect
		pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&cr)))
		white, _, _ := pGetStockObject.Call(0)
		black, _, _ := pGetStockObject.Call(4)
		pFillRect.Call(hdc, uintptr(unsafe.Pointer(&cr)), white)
		pSetBkMode.Call(hdc, 1)
		pad := a.px(24)
		o := &a.opts
		r := rect{pad, pad, cr.right - pad, pad + a.px(34)}
		a.drawText(hdc, a.fontBig, 0x202020, o.T("Associer un téléphone", "Pair a phone"), &r)
		r = rect{pad, r.bottom + a.px(4), cr.right - pad, r.bottom + a.px(52)}
		a.drawText(hdc, a.fontText, 0x404040, o.T("Scanne ce code avec l'appareil photo du téléphone, connecté au même Wi-Fi.",
			"Scan this code with the phone's camera, on the same Wi-Fi."), &r)
		top := r.bottom + a.px(12)
		bottomText := a.px(130)
		if c := a.pairing.Code; c != nil {
			avail := cr.right - 2*pad
			if hAvail := cr.bottom - top - bottomText; hAvail < avail {
				avail = hAvail
			}
			n := int32(c.Size + 8) // 4-module quiet zone on each side
			mod := avail / n
			if mod < 1 {
				mod = 1
			}
			size := mod * n
			x0 := (cr.right - size) / 2
			for yy := 0; yy < c.Size; yy++ {
				for xx := 0; xx < c.Size; xx++ {
					if c.Modules[yy][xx] {
						mr := rect{x0 + mod*int32(xx+4), top + mod*int32(yy+4), x0 + mod*int32(xx+5), top + mod*int32(yy+5)}
						pFillRect.Call(hdc, uintptr(unsafe.Pointer(&mr)), black)
					}
				}
			}
			top += size
		}
		r = rect{pad, top + a.px(6), cr.right - pad, top + a.px(30)}
		a.drawText(hdc, a.fontSmall, 0x606060, a.pairing.URL, &r)
		r = rect{pad, r.bottom + a.px(4), cr.right - pad, cr.bottom - pad}
		a.drawText(hdc, a.fontSmall, 0x606060, o.T(
			"Code valable 5 minutes, une seule fois. Le navigateur affichera un avertissement de certificat la première fois : touche « Paramètres avancés » puis « Continuer ». Ensuite, confirme ici.",
			"Valid once, for 5 minutes. The browser shows a certificate warning the first time: tap Advanced, then Proceed. Then confirm here."), &r)
		pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmClose:
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		if a.qrWnd == hwnd {
			a.qrWnd = 0
		}
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, m, wp, lp)
	return r
}

var _ = mbIconInfo

func inst() uintptr {
	h, _, _ := pGetModuleHandle.Call(0)
	return h
}
