//go:build windows

package win

import (
	"syscall"
	"unsafe"
)

// ---------------- clipboard ----------------

func OpenClipboard(h HWND) bool {
	r, _, _ := pOpenClipboard.Call(uintptr(h))
	if r == 0 {
		Sleep(10)
		r, _, _ = pOpenClipboard.Call(uintptr(h))
	}
	return r != 0
}

func CloseClipboard() { pCloseClipboard.Call() }

func EmptyClipboard() { pEmptyClipboard.Call() }

func SetClipboardData(format uint32, data []byte) bool {
	if len(data) == 0 {
		return false
	}
	h, _, _ := pGlobalAlloc.Call(GMEM_MOVEABLE, uintptr(len(data)))
	if h == 0 {
		return false
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		pGlobalFree.Call(h)
		return false
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(p)), len(data)), data)
	pGlobalUnlock.Call(h)
	r, _, _ := pSetClipboardData.Call(uintptr(format), h)
	if r == 0 {
		pGlobalFree.Call(h)
		return false
	}
	return true
}

func RegisterClipboardFormat(name string) uint32 {
	r, _, _ := pRegisterClipboardFormatW.Call(uintptr(unsafe.Pointer(UTF16Ptr(name))))
	return uint32(r)
}

// ---------------- tray icon ----------------

type Tray struct {
	data NOTIFYICONDATAW
	hwnd HWND
	icon HICON
}

func NewTray(hwnd HWND, icon HICON, tip string, cbMsg uint32) *Tray {
	t := &Tray{hwnd: hwnd, icon: icon}
	d := &t.data
	d.CbSize = uint32(unsafe.Sizeof(NOTIFYICONDATAW{}))
	d.HWnd = hwnd
	d.UID = 1
	d.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	d.UCallbackMessage = cbMsg
	d.HIcon = icon
	CopyUTF16(d.SzTip[:], tip)
	ok, errno := t.Add()
	if !ok {
		Logf("tray: Shell_NotifyIcon(NIM_ADD) failed err=%d icon=%d", errno, uintptr(icon))
	} else {
		Logf("tray: icon added hwnd=%d icon=%d", uintptr(hwnd), uintptr(icon))
	}
	return t
}

// Add (re)adds the icon; explorer drops it when it restarts.
func (t *Tray) Add() (bool, uint32) {
	r, _, e := pShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&t.data)))
	errno, _ := e.(syscall.Errno)
	return r != 0, uint32(errno)
}

// SetIcon swaps the icon and refreshes the tray entry.
func (t *Tray) SetIcon(icon HICON) {
	if icon == 0 {
		return
	}
	t.icon = icon
	t.data.HIcon = icon
	t.data.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	pShellNotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(&t.data)))
}

func (t *Tray) Balloon(title, text string, flags uint32) {
	d := &t.data
	d.UFlags = NIF_INFO
	CopyUTF16(d.SzInfo[:], text)
	CopyUTF16(d.SzInfoTitle[:], title)
	d.DwInfoFlags = flags
	pShellNotifyIconW.Call(NIM_MODIFY, uintptr(unsafe.Pointer(d)))
	d.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
}

// Refresh re-registers the icon so the shell picks up new settings.
func (t *Tray) Refresh() {
	pShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&t.data)))
	t.data.UFlags = NIF_MESSAGE | NIF_ICON | NIF_TIP
	pShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&t.data)))
}

func (t *Tray) Remove() {
	pShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&t.data)))
}

// ---------------- popup menu ----------------

type Menu struct{ h HMENU }

func NewPopupMenu() *Menu {
	r, _, _ := pCreatePopupMenu.Call()
	return &Menu{h: HMENU(r)}
}

func (m *Menu) Add(id uint32, text string, checked bool) {
	flags := uint32(MF_STRING)
	if checked {
		flags |= MF_CHECKED
	}
	pAppendMenuW.Call(uintptr(m.h), uintptr(flags), uintptr(id), uintptr(unsafe.Pointer(UTF16Ptr(text))))
}

func (m *Menu) Separator() {
	pAppendMenuW.Call(uintptr(m.h), MF_SEPARATOR, 0, 0)
}

func (m *Menu) Default(id uint32) {
	pSetMenuDefaultItem.Call(uintptr(m.h), uintptr(id), 0)
}

func (m *Menu) Destroy() {
	if m.h != 0 {
		pDestroyMenu.Call(uintptr(m.h))
		m.h = 0
	}
}

// Track blocks until the user picks an item; returns 0 when dismissed.
func (m *Menu) Track(owner HWND) uint32 {
	var pt POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(uintptr(owner))
	r, _, _ := pTrackPopupMenu.Call(uintptr(m.h), TPM_RETURNCMD|TPM_RIGHTBUTTON|TPM_NONOTIFY,
		uintptr(pt.X), uintptr(pt.Y), 0, uintptr(owner), 0)
	owner.PostMessage(WM_NULL, 0, 0)
	return uint32(r)
}

// ---------------- registry ----------------

const (
	RRF_RT_REG_SZ           = 0x00000002
	RRF_RT_REG_DWORD        = 0x00000010
	HKEY_CURRENT_USER       = HKEY(0x80000001)
	KEY_READ                = 0x20019
	KEY_WRITE               = 0x20006
	KEY_ALL_ACCESS          = 0xF003F
	REG_SZ                  = 1
	REG_DWORD               = 4
	REG_OPTION_NON_VOLATILE = 0
)

func regCreate(keyPath string) HKEY {
	var h HKEY
	var disp uint32
	pRegCreateKeyExW.Call(uintptr(HKEY_CURRENT_USER),
		uintptr(unsafe.Pointer(UTF16Ptr(keyPath))), 0, 0,
		REG_OPTION_NON_VOLATILE, KEY_ALL_ACCESS, 0,
		uintptr(unsafe.Pointer(&h)), uintptr(unsafe.Pointer(&disp)))
	return h
}

func regSetString(h HKEY, name, value string) bool {
	v := syscall.StringToUTF16(value)
	r, _, _ := pRegSetValueExW.Call(uintptr(h), uintptr(unsafe.Pointer(UTF16Ptr(name))), 0,
		REG_SZ, uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2))
	return r == 0
}

func regSetDword(h HKEY, name string, value uint32) bool {
	v := value
	r, _, _ := pRegSetValueExW.Call(uintptr(h), uintptr(unsafe.Pointer(UTF16Ptr(name))), 0,
		REG_DWORD, uintptr(unsafe.Pointer(&v)), 4)
	return r == 0
}

func regClose(h HKEY) {
	dllAdvapi32.NewProc("RegCloseKey").Call(uintptr(h))
}

func SetRunKey(name, exePath string) bool {
	h := regCreate(`Software\Microsoft\Windows\CurrentVersion\Run`)
	if h == 0 {
		return false
	}
	defer regClose(h)
	return regSetString(h, name, `"`+exePath+`"`)
}

func DeleteRunKey(name string) {
	h := regCreate(`Software\Microsoft\Windows\CurrentVersion\Run`)
	if h == 0 {
		return
	}
	defer regClose(h)
	pRegDeleteValueW.Call(uintptr(h), uintptr(unsafe.Pointer(UTF16Ptr(name))))
}

// DisableSnippingToolPrintScreen turns off the Windows 11 "use Print Screen to
// open Snipping Tool" behaviour so our global hotkey can own the key.
func DisableSnippingToolPrintScreen() {
	h := regCreate(`Control Panel\Keyboard`)
	if h == 0 {
		return
	}
	defer regClose(h)
	regSetDword(h, "PrintScreenKeyForSnippingToolEnabled", 0)
}

func EnableSnippingToolPrintScreen() {
	h := regCreate(`Control Panel\Keyboard`)
	if h == 0 {
		return
	}
	defer regClose(h)
	regSetDword(h, "PrintScreenKeyForSnippingToolEnabled", 1)
}

func SnippingToolPrintScreenEnabled() bool {
	var v uint32
	var size uint32 = 4
	r, _, _ := pRegGetValueW.Call(uintptr(HKEY_CURRENT_USER),
		uintptr(unsafe.Pointer(UTF16Ptr(`Control Panel\Keyboard`))),
		uintptr(unsafe.Pointer(UTF16Ptr("PrintScreenKeyForSnippingToolEnabled"))),
		RRF_RT_REG_DWORD, 0,
		uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&size)))
	if r != 0 {
		return true // missing value means Windows default (enabled)
	}
	return v != 0
}

// ---------------- misc ----------------

func ExePath() string {
	buf := make([]uint16, 1024)
	hProc, _, _ := dllKernel32.NewProc("GetCurrentProcess").Call()
	n := uint32(len(buf))
	r, _, _ := pQueryFullProcessImage.Call(hProc, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

// IconFromDib builds an HICON from a 32bpp BGRA DIB with straight alpha.
func IconFromDib(d *Dib) HICON {
	// A 32bpp color bitmap needs a monochrome mask handle (NULL is accepted by
	// some builds, but an explicit mask keeps every Windows version happy).
	mask, _, _ := pCreateBitmap.Call(uintptr(int(d.W)), uintptr(int(d.H)), 1, 1, 0)
	ii := ICONINFO{FIcon: 1, HbmMask: HBITMAP(mask), HbmColor: d.Bmp}
	r, _, _ := pCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	if mask != 0 {
		pDeleteObject.Call(mask)
	}
	return HICON(r)
}

// IconFromExe extracts icon #0 from an .exe/.ico file, preferring the size the
// shell asks for.
func IconFromExe(path string, small bool) HICON {
	var large, sml HICON
	n, _, _ := pExtractIconExW.Call(uintptr(unsafe.Pointer(UTF16Ptr(path))), 0,
		uintptr(unsafe.Pointer(&large)), uintptr(unsafe.Pointer(&sml)), 1)
	if n == 0 || n == ^uintptr(0) {
		return 0
	}
	if small {
		if sml != 0 {
			if large != 0 {
				pDestroyIcon.Call(uintptr(large))
			}
			return sml
		}
		return large
	}
	if large != 0 {
		if sml != 0 {
			pDestroyIcon.Call(uintptr(sml))
		}
		return large
	}
	return sml
}

// Logf is a hook the app installs so the win package can report diagnostics.
var Logf = func(string, ...any) {}

// RegisterWindowMessage returns a broadcast message id.
func RegisterWindowMessage(name string) uint32 {
	r, _, _ := dllUser32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(UTF16Ptr(name))))
	return uint32(r)
}

// ---------------- tray icon visibility (Windows 11) ----------------

var (
	pRegOpenKeyExW = dllAdvapi32.NewProc("RegOpenKeyExW")
	pRegEnumKeyExW = dllAdvapi32.NewProc("RegEnumKeyExW")
)

// PromoteTrayIcon asks the shell to show the icon on the taskbar instead of the
// overflow flyout. Windows 11 keeps that state per executable in
// HKCU\Control Panel\NotifyIconSettings\<hash>\IsPromoted, so a new app is hidden
// by default until this is set.
func PromoteTrayIcon(exePath string) bool {
	if exePath == "" {
		return false
	}
	const root = `Control Panel\NotifyIconSettings`
	var hkey HKEY
	r, _, _ := pRegOpenKeyExW.Call(uintptr(HKEY_CURRENT_USER),
		uintptr(unsafe.Pointer(UTF16Ptr(root))), 0, KEY_READ,
		uintptr(unsafe.Pointer(&hkey)))
	if r != 0 || hkey == 0 {
		return false
	}
	defer regClose(hkey)

	buf := make([]uint16, 256)
	done := false
	for i := uint32(0); ; i++ {
		n := uint32(len(buf))
		rr, _, _ := pRegEnumKeyExW.Call(uintptr(hkey), uintptr(i),
			uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)), 0, 0, 0, 0)
		if rr != 0 {
			break
		}
		sub := syscall.UTF16ToString(buf[:n])
		full := root + `\` + sub

		var pathBuf [1024]uint16
		size := uint32(len(pathBuf) * 2)
		pr, _, _ := pRegGetValueW.Call(uintptr(HKEY_CURRENT_USER),
			uintptr(unsafe.Pointer(UTF16Ptr(full))),
			uintptr(unsafe.Pointer(UTF16Ptr("ExecutablePath"))),
			RRF_RT_REG_SZ|0x00000040, 0,
			uintptr(unsafe.Pointer(&pathBuf[0])), uintptr(unsafe.Pointer(&size)))
		if pr != 0 {
			continue
		}
		stored := syscall.UTF16ToString(pathBuf[:])
		if !equalFold(stored, exePath) {
			continue
		}
		var kh HKEY
		or, _, _ := pRegOpenKeyExW.Call(uintptr(HKEY_CURRENT_USER),
			uintptr(unsafe.Pointer(UTF16Ptr(full))), 0, KEY_WRITE,
			uintptr(unsafe.Pointer(&kh)))
		if or != 0 || kh == 0 {
			continue
		}
		regSetDword(kh, "IsPromoted", 1)
		regClose(kh)
		done = true
	}
	return done
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if x >= 'A' && x <= 'Z' {
			x += 32
		}
		if y >= 'A' && y <= 'Z' {
			y += 32
		}
		if x != y {
			return false
		}
	}
	return true
}
