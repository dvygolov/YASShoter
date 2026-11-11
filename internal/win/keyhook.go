//go:build windows

package win

import (
	"syscall"
	"time"
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

	// Our own view of the modifier keys. GetAsyncKeyState lags behind the hook
	// for the event being dispatched, which made Ctrl+<key> unreliable on some
	// keyboards: the hook sees Ctrl go down but the async state still says up.
	trackedCtrl, trackedAlt, trackedShift, trackedWin bool
)

var modEventAt int64 // unix nano of the last modifier key event we saw

// TrackModifier updates the tracked modifier state from a keyboard event.
func TrackModifier(vk uint32, down bool) {
	switch vk {
	case VK_CONTROL, VK_LCONTROL, VK_RCONTROL, VK_MENU, VK_LMENU, VK_RMENU,
		VK_SHIFT, VK_LSHIFT, VK_RSHIFT, VK_LWIN, VK_RWIN:
		modEventAt = time.Now().UnixNano()
	}
	switch vk {
	case VK_CONTROL, VK_LCONTROL, VK_RCONTROL:
		trackedCtrl = down
	case VK_MENU, VK_LMENU, VK_RMENU:
		trackedAlt = down
	case VK_SHIFT, VK_LSHIFT, VK_RSHIFT:
		trackedShift = down
	case VK_LWIN, VK_RWIN:
		trackedWin = down
	}
}

const (
	VK_LCONTROL = 0xA2
	VK_RCONTROL = 0xA3
	VK_LMENU    = 0xA4
	VK_RMENU    = 0xA5
	VK_LSHIFT   = 0xA0
	VK_RSHIFT   = 0xA1
)

// TrackedModifiers returns the modifier mask built from the events we have seen.
func TrackedModifiers() uint32 {
	var m uint32
	if trackedCtrl {
		m |= MOD_CONTROL
	}
	if trackedAlt {
		m |= MOD_ALT
	}
	if trackedShift {
		m |= MOD_SHIFT
	}
	if trackedWin {
		m |= MOD_WIN
	}
	return m
}

// ResyncModifiers clears tracked bits that the OS says are released. If a key up
// event is ever missed (window switch, input grabbed by another app, the hook
// being dropped for a moment), the tracked state would stay stuck forever and
// e.g. Ctrl+PrintScreen would look like Ctrl+Alt+PrintScreen. Every new key down
// heals it.
func ResyncModifiers() {
	// Never trust the async state right after a modifier event: GetAsyncKeyState
	// can lag behind (or not see injected keys) and would drop a legitimately
	// held Ctrl, turning Ctrl+PrintScreen into a plain screenshot.
	if time.Since(time.Unix(0, modEventAt)) < 2*time.Second {
		return
	}
	if trackedCtrl && !keyDown(VK_CONTROL) {
		trackedCtrl = false
	}
	if trackedAlt && !keyDown(VK_MENU) {
		trackedAlt = false
	}
	if trackedShift && !keyDown(VK_SHIFT) {
		trackedShift = false
	}
	if trackedWin && !keyDown(VK_LWIN) && !keyDown(VK_RWIN) {
		trackedWin = false
	}
}

// ReinstallKeyboardHook replaces the hook. Windows silently removes low level
// hooks whose callback is too slow, so a periodic reinstall is a cheap safety net.
func ReinstallKeyboardHook() bool {
	// Install the new hook first: it becomes the head of the hook chain and if
	// the call fails we still have the previous one working.
	h, _, _ := pSetWindowsHookExW.Call(WH_KEYBOARD_LL, keyHookProc, uintptr(GetModuleHandle()), 0)
	if h == 0 {
		return false
	}
	old := hookHandle
	hookHandle = h
	if old != 0 && old != h {
		pUnhookWinEvent.Call(old)
	}
	SeedModifiers()
	return true
}

// SeedModifiers initialises the tracked state from the current keyboard state so
// a modifier that was already held when the hook started is not missed.
func SeedModifiers() {
	TrackModifier(VK_LCONTROL, keyDown(VK_CONTROL))
	TrackModifier(VK_LMENU, keyDown(VK_MENU))
	TrackModifier(VK_LSHIFT, keyDown(VK_SHIFT))
	TrackModifier(VK_LWIN, keyDown(VK_LWIN))
}

func keyboardProc(nCode int32, wparam, lparam uintptr) uintptr {
	if nCode == HC_ACTION && keyFilter != nil {
		kb := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lparam))
		down := wparam == WM_KEYDOWN_ || wparam == WM_SYSKEYDOWN_
		TrackModifier(kb.VkCode, down)
		if down {
			ResyncModifiers()
		}
		mods := TrackedModifiers()
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
	SeedModifiers()
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
