//go:build windows

package win

import (
	"syscall"
	"unsafe"
)

// WH_KEYBOARD_LL hooking. RegisterHotKey cannot claim PrintScreen on Windows 11
// because the shell owns it, but a low level keyboard hook is called before hot
// key processing, so the key can be swallowed before the Snipping Tool sees it.

type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

const (
	WH_KEYBOARD_LL = 13
	HC_ACTION      = 0
	WM_KEYDOWN_    = 0x0100
	WM_KEYUP_      = 0x0101
	WM_SYSKEYDOWN_ = 0x0104
	WM_SYSKEYUP_   = 0x0105
	LLKHF_INJECTED = 0x10
)

var (
	pSetWindowsHookExW = dllUser32.NewProc("SetWindowsHookExW")
	pCallNextHookEx    = dllUser32.NewProc("CallNextHookEx")
	pUnhookWinEvent    = dllUser32.NewProc("UnhookWindowsHookEx")
	pGetAsyncKeyState  = dllUser32.NewProc("GetAsyncKeyState")
)

// KeyFilter decides whether the key event is swallowed.
type KeyFilter func(vk uint32, down bool, mods uint32) bool

var (
	keyFilter   KeyFilter
	keyHookProc = syscall.NewCallback(keyboardProc)
	hookHandle  uintptr
)

func keyboardProc(nCode int32, wparam, lparam uintptr) uintptr {
	if nCode == HC_ACTION && keyFilter != nil {
		kb := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lparam))
		down := wparam == WM_KEYDOWN_ || wparam == WM_SYSKEYDOWN_
		mods := Modifiers()
		if keyFilter(kb.VkCode, down, mods) {
			return 1 // swallowed
		}
	}
	r, _, _ := pCallNextHookEx.Call(hookHandle, uintptr(nCode), wparam, lparam)
	return r
}

// InstallKeyboardHook installs the global low level keyboard hook. It must be
// called from the thread that runs the message loop.
func InstallKeyboardHook(filter KeyFilter) bool {
	keyFilter = filter
	h, _, _ := pSetWindowsHookExW.Call(WH_KEYBOARD_LL, keyHookProc, uintptr(GetModuleHandle()), 0)
	hookHandle = h
	return h != 0
}

func RemoveKeyboardHook() {
	if hookHandle != 0 {
		pUnhookWinEvent.Call(hookHandle)
		hookHandle = 0
	}
}

// Modifiers returns the currently pressed modifier keys in RegisterHotKey terms.
func Modifiers() uint32 {
	var m uint32
	if keyDown(VK_CONTROL) {
		m |= MOD_CONTROL
	}
	if keyDown(VK_MENU) {
		m |= MOD_ALT
	}
	if keyDown(VK_SHIFT) {
		m |= MOD_SHIFT
	}
	if keyDown(VK_LWIN) || keyDown(VK_RWIN) {
		m |= MOD_WIN
	}
	return m
}

const (
	VK_LWIN = 0x5B
	VK_RWIN = 0x5C
)

func keyDown(vk int) bool {
	r, _, _ := pGetAsyncKeyState.Call(uintptr(vk))
	return uint16(r)&0x8000 != 0
}
