//go:build windows

package win

import (
	"syscall"
	"unsafe"
)

// Windows blocks a non-elevated process from receiving keyboard input while a
// higher-integrity window has the foreground (UIPI). A low level keyboard hook
// simply stops seeing keys for those apps, which looks like "the hotkey works
// here but not there".

var (
	pOpenProcess      = dllKernel32.NewProc("OpenProcess")
	pOpenProcessToken = dllAdvapi32.NewProc("OpenProcessToken")
	pGetTokenInfo     = dllAdvapi32.NewProc("GetTokenInformation")
	pCloseHandle      = dllKernel32.NewProc("CloseHandle")
)

const (
	PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	TOKEN_QUERY                       = 0x0008
	TokenElevation                    = 20
)

// IsProcessElevated reports whether the process runs with administrator rights.
func IsProcessElevated(pid uint32) bool {
	h, _, _ := pOpenProcess.Call(PROCESS_QUERY_LIMITED_INFORMATION, 0, uintptr(pid))
	if h == 0 {
		return false
	}
	defer pCloseHandle.Call(h)
	var tok uintptr
	if r, _, _ := pOpenProcessToken.Call(h, TOKEN_QUERY, uintptr(unsafe.Pointer(&tok))); r == 0 || tok == 0 {
		return false
	}
	defer pCloseHandle.Call(tok)
	var elevated int32
	var ret uint32
	if r, _, _ := pGetTokenInfo.Call(tok, TokenElevation,
		uintptr(unsafe.Pointer(&elevated)), 4, uintptr(unsafe.Pointer(&ret))); r == 0 {
		return false
	}
	return elevated != 0
}

// SelfElevated reports whether we are running as administrator.
func SelfElevated() bool { return IsProcessElevated(SelfPID()) }

// ProcessImageName returns the executable path of another process (best effort).
func ProcessImageName(pid uint32) string {
	h, _, _ := pOpenProcess.Call(PROCESS_QUERY_LIMITED_INFORMATION, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer pCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if r, _, _ := pQueryFullProcessImage.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size))); r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:size])
}
