//go:build windows

package lockscreen

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"github.com/remigl/tapis/internal/input"
	"github.com/remigl/tapis/internal/winsec"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	wtsapi32 = syscall.NewLazyDLL("wtsapi32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")

	pActiveConsoleSession = kernel32.NewProc("WTSGetActiveConsoleSessionId")
	pProcessIdToSessionId = kernel32.NewProc("ProcessIdToSessionId")
	pCreateNamedPipe      = kernel32.NewProc("CreateNamedPipeW")
	pConnectNamedPipe     = kernel32.NewProc("ConnectNamedPipe")
	pDisconnectNamedPipe  = kernel32.NewProc("DisconnectNamedPipe")
	pGetPipeServerPID     = kernel32.NewProc("GetNamedPipeServerProcessId")
	pDuplicateTokenEx     = advapi32.NewProc("DuplicateTokenEx")
	pSetTokenInformation  = advapi32.NewProc("SetTokenInformation")
	pCreateProcessAsUser  = advapi32.NewProc("CreateProcessAsUserW")
	pSDDLToSD             = advapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
	pWTSQueryUserToken    = wtsapi32.NewProc("WTSQueryUserToken")
	pWTSQuerySessionInfo  = wtsapi32.NewProc("WTSQuerySessionInformationW")
	pWTSFreeMemory        = wtsapi32.NewProc("WTSFreeMemory")
	pOpenInputDesktop     = user32.NewProc("OpenInputDesktop")
	pSetThreadDesktop     = user32.NewProc("SetThreadDesktop")
	pCloseDesktop         = user32.NewProc("CloseDesktop")
)

func u16(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

func pipeName(session uint32) string { return fmt.Sprintf(`\\.\pipe\tapis-lockscreen-%d`, session) }

func currentSession() uint32 {
	var sid uint32
	pProcessIdToSessionId.Call(uintptr(os.Getpid()), uintptr(unsafe.Pointer(&sid)))
	return sid
}

// helperPath is where the helper lives: only administrators can write there.
func helperPath() string {
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	return filepath.Join(pf, "Tapis", "tapis-lockscreen.exe")
}

// Installed reports whether the helper is installed.
func Installed() bool {
	_, err := os.Stat(helperPath())
	return err == nil
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func encode(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		b[2*i], b[2*i+1] = byte(c), byte(c>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// runElevated runs a PowerShell script as administrator (one UAC prompt).
func runElevated(script string) error {
	launcher := fmt.Sprintf(`Start-Process -FilePath %s -Verb RunAs -Wait -WindowStyle Hidden -ArgumentList '-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass','-EncodedCommand','%s'`, psQuote(winsec.PowerShell()), encode(script))
	cmd := exec.Command(winsec.PowerShell(), "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", launcher)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Install copies Tapis to Program Files and registers the helper task.
func Install() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// The file that will run as SYSTEM must be exactly this program: its
	// hash is computed here and checked by the elevated script before and
	// after copying (so it cannot be swapped while the UAC prompt is open).
	data, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	hash := strings.ToUpper(hex.EncodeToString(sum[:]))
	dst := helperPath()
	script := fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$src = %s; $dst = %s; $name = %s; $hash = '%s'
if ((Get-FileHash -Algorithm SHA256 -LiteralPath $src).Hash -ne $hash) { throw 'Tapis was modified, installation cancelled' }
Stop-ScheduledTask -TaskName $name -ErrorAction SilentlyContinue
Get-Process -Name 'tapis-lockscreen' -ErrorAction SilentlyContinue | Stop-Process -Force
New-Item -ItemType Directory -Force -Path (Split-Path $dst) | Out-Null
Copy-Item -Force -LiteralPath $src -Destination $dst
if ((Get-FileHash -Algorithm SHA256 -LiteralPath $dst).Hash -ne $hash) { Remove-Item -Force -LiteralPath $dst; throw 'copy mismatch, installation cancelled' }
$a = New-ScheduledTaskAction -Execute $dst -Argument '--lockscreen-service'
$t = New-ScheduledTaskTrigger -AtStartup
$p = New-ScheduledTaskPrincipal -UserId 'S-1-5-18' -LogonType ServiceAccount -RunLevel Highest
$s = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit ([TimeSpan]::Zero) -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) -MultipleInstances IgnoreNew
Register-ScheduledTask -TaskName $name -Action $a -Trigger $t -Principal $p -Settings $s -Description 'Lets Tapis type on the lock screen (only while the session is locked).' -Force | Out-Null
Start-ScheduledTask -TaskName $name`, psQuote(exe), psQuote(dst), psQuote(TaskName), hash)
	if err := runElevated(script); err != nil {
		return err
	}
	if !Installed() {
		return errors.New("the helper was not installed (was the administrator prompt declined?)")
	}
	return nil
}

// Uninstall removes the helper task and files.
func Uninstall() error {
	script := fmt.Sprintf(`$ErrorActionPreference = 'SilentlyContinue'
$dst = %s; $name = %s
Stop-ScheduledTask -TaskName $name
Unregister-ScheduledTask -TaskName $name -Confirm:$false
Get-Process -Name 'tapis-lockscreen' | Stop-Process -Force
Start-Sleep -Milliseconds 500
Remove-Item -Force -Recurse -LiteralPath (Split-Path $dst)`, psQuote(helperPath()), psQuote(TaskName))
	if err := runElevated(script); err != nil {
		return err
	}
	if Installed() {
		return errors.New("the helper is still installed (was the administrator prompt declined?)")
	}
	return nil
}

// ---------------------------------------------------------------------
// Service: runs as SYSTEM in session 0 and keeps one agent running in the
// active console session, on the secure (Winlogon) desktop.

// RunService never returns.
func RunService() {
	exe, _ := os.Executable()
	agents := map[uint32]syscall.Handle{}
	for {
		sid, _, _ := pActiveConsoleSession.Call()
		s := uint32(sid)
		if s != 0xFFFFFFFF {
			if h, ok := agents[s]; ok {
				if ev, _ := syscall.WaitForSingleObject(h, 0); ev == syscall.WAIT_OBJECT_0 {
					syscall.CloseHandle(h)
					delete(agents, s)
				}
			}
			if _, ok := agents[s]; !ok {
				if h, err := spawnAgent(exe, s); err == nil {
					agents[s] = h
				}
			}
		}
		time.Sleep(2 * time.Second)
	}
}

type startupInfo struct {
	cb                                 uint32
	reserved, desktop, title           *uint16
	x, y, xSize, ySize, xChars, yChars uint32
	fill, flags                        uint32
	showWindow, cbReserved2            uint16
	reserved2                          *byte
	stdIn, stdOut, stdErr              syscall.Handle
}

func spawnAgent(exe string, session uint32) (syscall.Handle, error) {
	proc, _ := syscall.GetCurrentProcess()
	var tok syscall.Token
	const access = 0x0001 | 0x0002 | 0x0008 | 0x0080 | 0x0100 // ASSIGN_PRIMARY|DUPLICATE|QUERY|ADJUST_DEFAULT|ADJUST_SESSIONID
	if err := syscall.OpenProcessToken(proc, access, &tok); err != nil {
		return 0, err
	}
	defer tok.Close()
	var dup syscall.Token
	if r, _, err := pDuplicateTokenEx.Call(uintptr(tok), 0x02000000, 0, 2, 1, uintptr(unsafe.Pointer(&dup))); r == 0 {
		return 0, err
	}
	defer dup.Close()
	if r, _, err := pSetTokenInformation.Call(uintptr(dup), 12, uintptr(unsafe.Pointer(&session)), 4); r == 0 { // TokenSessionId
		return 0, err
	}
	si := startupInfo{desktop: u16(`winsta0\winlogon`)}
	si.cb = uint32(unsafe.Sizeof(si))
	var pi syscall.ProcessInformation
	cmd := u16(`"` + exe + `" --lockscreen-agent`)
	if r, _, err := pCreateProcessAsUser.Call(uintptr(dup), uintptr(unsafe.Pointer(u16(exe))), uintptr(unsafe.Pointer(cmd)),
		0, 0, 0, 0x08000000, 0, 0, uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi))); r == 0 { // CREATE_NO_WINDOW
		return 0, err
	}
	syscall.CloseHandle(pi.Thread)
	return pi.Process, nil
}

// ---------------------------------------------------------------------
// Agent: runs as SYSTEM in the user's session. Listens on the pipe and
// injects input on the input desktop, only while the session is locked.

func sessionLocked(session uint32) bool {
	var buf *byte
	var n uint32
	if r, _, _ := pWTSQuerySessionInfo.Call(0, uintptr(session), 25, uintptr(unsafe.Pointer(&buf)), uintptr(unsafe.Pointer(&n))); r == 0 || buf == nil {
		return false
	}
	defer pWTSFreeMemory.Call(uintptr(unsafe.Pointer(buf)))
	if n < 16 {
		return false
	}
	// WTSINFOEXW { DWORD Level; WTSINFOEX_LEVEL1_W { ULONG SessionId; WTS_CONNECTSTATE_CLASS State; LONG SessionFlags; ... } }
	data := unsafe.Slice(buf, n)
	flags := int32(binary.LittleEndian.Uint32(data[12:]))
	return flags == 0 // WTS_SESSIONSTATE_LOCK
}

// userSID returns the SID of the user logged on in a session.
func userSID(session uint32) (string, error) {
	var tok syscall.Token
	if r, _, err := pWTSQueryUserToken.Call(uintptr(session), uintptr(unsafe.Pointer(&tok))); r == 0 {
		return "", err
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return "", err
	}
	return u.User.Sid.String()
}

func createPipe(session uint32, user string) (syscall.Handle, error) {
	// Only SYSTEM and the user of this session; low-integrity processes
	// (sandboxed browsers…) are refused by the default integrity label.
	sddl := "D:P(A;;GA;;;SY)(A;;GRGW;;;" + user + ")"
	var sd uintptr
	if r, _, err := pSDDLToSD.Call(uintptr(unsafe.Pointer(u16(sddl))), 1, uintptr(unsafe.Pointer(&sd)), 0); r == 0 {
		return 0, err
	}
	defer syscall.LocalFree(syscall.Handle(sd))
	sa := syscall.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(sa))
	h, _, err := pCreateNamedPipe.Call(uintptr(unsafe.Pointer(u16(pipeName(session)))),
		0x00000003|0x00080000, // PIPE_ACCESS_DUPLEX | FILE_FLAG_FIRST_PIPE_INSTANCE
		0x00000008,            // PIPE_TYPE_BYTE | PIPE_WAIT | PIPE_REJECT_REMOTE_CLIENTS
		1, 4096, 4096, 0, uintptr(unsafe.Pointer(&sa)))
	if syscall.Handle(h) == syscall.InvalidHandle {
		return 0, err
	}
	return syscall.Handle(h), nil
}

type pipeReader struct{ h syscall.Handle }

func (p pipeReader) Read(b []byte) (int, error) {
	var n uint32
	err := syscall.ReadFile(p.h, b, &n, nil)
	if n == 0 && err == nil {
		err = errors.New("EOF")
	}
	return int(n), err
}

// RunAgent never returns (except on fatal errors).
func RunAgent() {
	session := currentSession()

	// All injection happens on one OS thread attached to the input desktop.
	events := make(chan event, 256)
	go func() {
		runtime.LockOSThread()
		in, err := input.New(input.Options{})
		if err != nil {
			return
		}
		for ev := range events {
			if !sessionLocked(session) {
				continue // never act on an unlocked session (UAC prompts!)
			}
			desk, _, _ := pOpenInputDesktop.Call(0, 0, 0x10000000) // GENERIC_ALL
			if desk == 0 {
				continue
			}
			pSetThreadDesktop.Call(desk)
			apply(in, ev)
			pCloseDesktop.Call(desk)
		}
	}()

	for {
		user, err := userSID(session)
		if err != nil { // nobody logged on yet
			time.Sleep(3 * time.Second)
			continue
		}
		h, err := createPipe(session, user)
		if err != nil {
			time.Sleep(3 * time.Second)
			continue
		}
		pConnectNamedPipe.Call(uintptr(h), 0)
		sc := bufio.NewScanner(pipeReader{h})
		sc.Buffer(make([]byte, 0, 4096), 16<<10)
		for sc.Scan() {
			var ev event
			if json.Unmarshal(sc.Bytes(), &ev) == nil {
				select {
				case events <- ev:
				default: // drop rather than block
				}
			}
		}
		pDisconnectNamedPipe.Call(uintptr(h))
		syscall.CloseHandle(h)
	}
}

func clamp(v, lim int) int {
	if v > lim {
		return lim
	}
	if v < -lim {
		return -lim
	}
	return v
}

func apply(in input.Injector, ev event) {
	switch ev.Op {
	case "move":
		in.Move(clamp(ev.X, 4000), clamp(ev.Y, 4000))
	case "scroll":
		in.Scroll(clamp(ev.X, 12000), clamp(ev.Y, 12000))
	case "button":
		if ev.B >= 0 && ev.B <= 2 {
			in.Button(input.Button(ev.B), ev.Down)
		}
	case "key":
		if input.ValidKey(ev.K) {
			in.Key(ev.K, ev.Down)
		}
	case "text":
		if r := []rune(ev.S); len(r) <= 256 {
			in.Type(ev.S)
		}
	}
}

// ---------------------------------------------------------------------
// Client side: wraps the normal injector. While the secure desktop is
// active, events go to the agent instead.

type client struct {
	input.Injector
	log *log.Logger

	mu      sync.Mutex
	pipe    syscall.Handle
	checked time.Time
	secure  bool
	lastTry time.Time
}

// Wrap returns an injector that also works on the lock screen when the
// helper is installed.
func Wrap(in input.Injector, l *log.Logger) input.Injector {
	return &client{Injector: in, log: l, pipe: syscall.InvalidHandle}
}

// secureDesktop reports whether the input desktop is not ours (lock
// screen, UAC prompt, Ctrl+Alt+Del screen).
func (c *client) secureDesktop() bool {
	if time.Since(c.checked) < 150*time.Millisecond {
		return c.secure
	}
	c.checked = time.Now()
	d, _, _ := pOpenInputDesktop.Call(0, 0, 0x0001) // DESKTOP_READOBJECTS
	if d != 0 {
		pCloseDesktop.Call(d)
	}
	c.secure = d == 0
	return c.secure
}

// pipeIsSystem checks that the pipe server is a SYSTEM process.
func pipeIsSystem(h syscall.Handle) bool {
	var pid uint32
	if r, _, _ := pGetPipeServerPID.Call(uintptr(h), uintptr(unsafe.Pointer(&pid))); r == 0 {
		return false
	}
	p, err := syscall.OpenProcess(0x1000, false, pid) // PROCESS_QUERY_LIMITED_INFORMATION
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(p)
	var tok syscall.Token
	if syscall.OpenProcessToken(p, syscall.TOKEN_QUERY, &tok) != nil {
		return false
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return false
	}
	s, err := u.User.Sid.String()
	return err == nil && s == "S-1-5-18"
}

func (c *client) send(ev event) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.secureDesktop() {
		return false
	}
	if c.pipe == syscall.InvalidHandle {
		if time.Since(c.lastTry) < 2*time.Second {
			return false
		}
		c.lastTry = time.Now()
		h, err := syscall.CreateFile(u16(pipeName(currentSession())), syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING,
			0x00100000, 0) // SECURITY_SQOS_PRESENT | SECURITY_ANONYMOUS: the server cannot impersonate us
		if err != nil {
			return false // helper not installed: fall back to normal input
		}
		if !pipeIsSystem(h) {
			syscall.CloseHandle(h)
			c.log.Printf("lock screen: refusing a pipe that is not owned by SYSTEM")
			return false
		}
		c.pipe = h
	}
	b, _ := json.Marshal(ev)
	b = append(b, '\n')
	var n uint32
	if err := syscall.WriteFile(c.pipe, b, &n, nil); err != nil {
		syscall.CloseHandle(c.pipe)
		c.pipe = syscall.InvalidHandle
		return false
	}
	return true
}

func (c *client) Move(dx, dy int) error {
	if c.send(event{Op: "move", X: dx, Y: dy}) {
		return nil
	}
	return c.Injector.Move(dx, dy)
}

func (c *client) Scroll(dx, dy int) error {
	if c.send(event{Op: "scroll", X: dx, Y: dy}) {
		return nil
	}
	return c.Injector.Scroll(dx, dy)
}

func (c *client) Button(b input.Button, down bool) error {
	if c.send(event{Op: "button", B: int(b), Down: down}) {
		return nil
	}
	return c.Injector.Button(b, down)
}

func (c *client) Key(name string, down bool) error {
	if c.send(event{Op: "key", K: name, Down: down}) {
		return nil
	}
	return c.Injector.Key(name, down)
}

func (c *client) Type(text string) error {
	if c.send(event{Op: "text", S: text}) {
		return nil
	}
	return c.Injector.Type(text)
}
