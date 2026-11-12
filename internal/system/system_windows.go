//go:build windows

package system

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

var errUnknown = errors.New("unknown action")

var (
	user32         = syscall.NewLazyDLL("user32.dll")
	powrprof       = syscall.NewLazyDLL("powrprof.dll")
	advapi32       = syscall.NewLazyDLL("advapi32.dll")
	procLock       = user32.NewProc("LockWorkStation")
	procSuspend    = powrprof.NewProc("SetSuspendState")
	procLookupPriv = advapi32.NewProc("LookupPrivilegeValueW")
	procAdjustPriv = advapi32.NewProc("AdjustTokenPrivileges")
)

func lock() error {
	if r, _, err := procLock.Call(); r == 0 {
		return fmt.Errorf("LockWorkStation: %v", err)
	}
	return nil
}

// enableShutdownPrivilege enables SeShutdownPrivilege, which every
// interactive user holds but which is disabled by default.
func enableShutdownPrivilege() error {
	proc, _ := syscall.GetCurrentProcess()
	var tok syscall.Token
	const tokenAdjust, tokenQuery = 0x0020, 0x0008
	if err := syscall.OpenProcessToken(proc, tokenAdjust|tokenQuery, &tok); err != nil {
		return err
	}
	defer tok.Close()
	var tp struct {
		count uint32
		luid  struct{ low, high uint32 }
		attrs uint32
	}
	name, _ := syscall.UTF16PtrFromString("SeShutdownPrivilege")
	if r, _, err := procLookupPriv.Call(0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&tp.luid))); r == 0 {
		return err
	}
	tp.count, tp.attrs = 1, 0x2 // SE_PRIVILEGE_ENABLED
	if r, _, err := procAdjustPriv.Call(uintptr(tok), 0, uintptr(unsafe.Pointer(&tp)), 0, 0, 0); r == 0 {
		return err
	}
	return nil
}

func sleep() error {
	if err := enableShutdownPrivilege(); err != nil {
		return fmt.Errorf("enabling shutdown privilege: %v", err)
	}
	// SetSuspendState(hibernate=FALSE, force=FALSE, disableWakeEvents=FALSE)
	if r, _, err := procSuspend.Call(0, 0, 0); r == 0 {
		return fmt.Errorf("SetSuspendState: %v", err)
	}
	return nil
}
