//go:build windows

package win

import (
	"syscall"
	"unsafe"
)

// ---------- kernel32 ----------

func GetModuleHandle() HINSTANCE {
	h, _, _ := pGetModuleHandleW.Call(0)
	return HINSTANCE(h)
}

func CreateMutex(name string) HANDLE {
	h, _, _ := pCreateMutexW.Call(0, 0, ptr(unsafe.Pointer(UTF16Ptr(name))))
	return HANDLE(h)
}

// CreateMutexAndCheck creates a named mutex and reports whether this process is
// the first owner (ERROR_ALREADY_EXISTS means another instance is running).
func CreateMutexAndCheck(name string) (HANDLE, error) {
	h, _, e := pCreateMutexW.Call(0, 0, ptr(unsafe.Pointer(UTF16Ptr(name))))
	errno, _ := e.(syscall.Errno)
	if h == 0 {
		return 0, errorString("mutex creation failed")
	}
	if errno == 183 { // ERROR_ALREADY_EXISTS
		return HANDLE(h), errorString("already running")
	}
	return HANDLE(h), nil
}

type errorString string

func (e errorString) Error() string { return string(e) }

func Sleep(ms uint32) { pSleep.Call(uintptr(ms)) }

func GetTickCount() uint32 {
	r, _, _ := pGetTickCount.Call()
	return uint32(r)
}

// ---------- user32 window ----------

type WndProcFunc func(hwnd HWND, msg uint32, wparam, lparam uintptr) uintptr

var (
	wndProcs   []WndProcFunc
	wndProcCB  = syscall.NewCallback(wndProcDispatcher)
	registered = map[string]bool{}
)

func wndProcDispatcher(hwnd HWND, msg uint32, wparam, lparam uintptr) uintptr {
	var id uintptr
	if msg == WM_NCCREATE {
		cs := (*CREATESTRUCTW)(unsafe.Pointer(lparam))
		id = cs.LpCreateParams
		SetWindowLongPtr(hwnd, GWLP_USERDATA, id)
	} else {
		id = GetWindowLongPtr(hwnd, GWLP_USERDATA)
	}
	if id > 0 && int(id) <= len(wndProcs) {
		return wndProcs[id-1](hwnd, msg, wparam, lparam)
	}
	r, _, _ := pDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wparam, lparam)
	return r
}

// RegisterClass registers a window class once. All windows of that class share
// the dispatcher, which routes messages through GWLP_USERDATA.
func RegisterClass(className string, cursorID uintptr) {
	if registered[className] {
		return
	}
	inst := GetModuleHandle()
	var cursor uintptr
	if cursorID != 0 {
		cursor = LoadCursor(0, cursorID)
	}
	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		Style:         0,
		LpfnWndProc:   wndProcCB,
		HInstance:     inst,
		HCursor:       cursor,
		HbrBackground: 0,
		LpszClassName: UTF16Ptr(className),
	}
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	registered[className] = true
}

// NewHandler registers a message handler for one window instance and returns the
// id to pass as lpCreateParams to CreateWindowEx.
func NewHandler(fn WndProcFunc) uintptr {
	wndProcs = append(wndProcs, fn)
	return uintptr(len(wndProcs))
}

func CreateWindowEx(exStyle uint32, className string, title string, style uint32, x, y, w, h int32, parent HWND, menu HMENU, param uintptr) HWND {
	r, _, _ := pCreateWindowExW.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(UTF16Ptr(className))),
		uintptr(unsafe.Pointer(UTF16Ptr(title))),
		uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		uintptr(parent), uintptr(menu), uintptr(GetModuleHandle()),
		param,
	)
	return HWND(r)
}

func (h HWND) Destroy() { pDestroyWindow.Call(uintptr(h)) }

func (h HWND) Show(cmd int) { pShowWindow.Call(uintptr(h), uintptr(cmd)) }

func (h HWND) ShowWindow(cmd int) { pShowWindow.Call(uintptr(h), uintptr(cmd)) }

func (h HWND) IsWindow() bool {
	r, _, _ := pIsWindow.Call(uintptr(h))
	return r != 0
}

func (h HWND) Update() { pUpdateWindow.Call(uintptr(h)) }

func (h HWND) Invalidate(erase bool) {
	e := 0
	if erase {
		e = 1
	}
	pInvalidateRect.Call(uintptr(h), 0, uintptr(e))
}

func (h HWND) InvalidateRect(r *RECT, erase bool) {
	e := 0
	if erase {
		e = 1
	}
	pInvalidateRect.Call(uintptr(h), uintptr(unsafe.Pointer(r)), uintptr(e))
}

func (h HWND) SetPos(z HWND, x, y, w, hgt int32, flags uint32) {
	pSetWindowPos.Call(uintptr(h), uintptr(z), uintptr(x), uintptr(y), uintptr(w), uintptr(hgt), uintptr(flags))
}

func (h HWND) Rect() RECT {
	var r RECT
	pGetWindowRect.Call(uintptr(h), uintptr(unsafe.Pointer(&r)))
	return r
}

func (h HWND) ClientRect() RECT {
	var r RECT
	pGetClientRect.Call(uintptr(h), uintptr(unsafe.Pointer(&r)))
	return r
}

func (h HWND) ClassName() string {
	buf := make([]uint16, 256)
	n, _, _ := pGetClassNameW.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

func (h HWND) Title() string {
	buf := make([]uint16, 512)
	n, _, _ := pGetWindowTextW.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

func (h HWND) IsVisible() bool {
	r, _, _ := pIsWindowVisible.Call(uintptr(h))
	return r != 0
}

func (h HWND) Style() uint32 {
	r, _, _ := pGetWindowLongPtrW.Call(uintptr(h), GWL_STYLE)
	return uint32(r)
}

func (h HWND) ExStyle() uint32 {
	r, _, _ := pGetWindowLongPtrW.Call(uintptr(h), GWL_EXSTYLE)
	return uint32(r)
}

func (h HWND) SetExStyle(v uint32) {
	pSetWindowLongPtrW.Call(uintptr(h), GWL_EXSTYLE, uintptr(v))
}

func (h HWND) PID() uint32 {
	var pid uint32
	pGetWindowThreadProcessId.Call(uintptr(h), uintptr(unsafe.Pointer(&pid)))
	return pid
}

func (h HWND) SetForeground() { pSetForegroundWindow.Call(uintptr(h)) }

func (h HWND) PostMessage(msg uint32, wp, lp uintptr) {
	pPostMessageW.Call(uintptr(h), uintptr(msg), wp, lp)
}

func (h HWND) SendMessage(msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := pSendMessageW.Call(uintptr(h), uintptr(msg), wp, lp)
	return r
}

func SetWindowLongPtr(h HWND, index uintptr, v uintptr) uintptr {
	r, _, _ := pSetWindowLongPtrW.Call(uintptr(h), index, v)
	return r
}

func GetWindowLongPtr(h HWND, index uintptr) uintptr {
	r, _, _ := pGetWindowLongPtrW.Call(uintptr(h), index)
	return r
}

func GetForegroundWindow() HWND {
	r, _, _ := pGetForegroundWindow.Call()
	return HWND(r)
}

func SetCursorPos(x, y int32) {
	pSetCursorPos.Call(uintptr(int(x)), uintptr(int(y)))
}

func GetCursorPos() POINT {
	var p POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

func LoadCursor(inst HINSTANCE, id uintptr) uintptr {
	r, _, _ := pLoadCursorW.Call(uintptr(inst), id)
	return r
}

func SetCursor(cur uintptr) {
	pSetCursor.Call(cur)
}

func SetCapture(h HWND) {
	pSetCapture.Call(uintptr(h))
}

func ReleaseCapture() { pReleaseCapture.Call() }

func GetCapture() HWND {
	r, _, _ := pGetCapture.Call()
	return HWND(r)
}

func GetDC(h HWND) HDC {
	r, _, _ := pGetDC.Call(uintptr(h))
	return HDC(r)
}

func ReleaseDC(h HWND, dc HDC) {
	pReleaseDC.Call(uintptr(h), uintptr(dc))
}

func ScreenDC() (HDC, func()) {
	dc := GetDC(0)
	return dc, func() { ReleaseDC(0, dc) }
}

func GetSystemMetrics(i int) int32 {
	r, _, _ := pGetSystemMetrics.Call(uintptr(i))
	return int32(r)
}

func VirtualScreen() RECT {
	return RECT{
		Left:   GetSystemMetrics(SM_XVIRTUALSCREEN),
		Top:    GetSystemMetrics(SM_YVIRTUALSCREEN),
		Right:  GetSystemMetrics(SM_XVIRTUALSCREEN) + GetSystemMetrics(SM_CXVIRTUALSCREEN),
		Bottom: GetSystemMetrics(SM_YVIRTUALSCREEN) + GetSystemMetrics(SM_CYVIRTUALSCREEN),
	}
}

func PrimaryScreen() RECT {
	return RECT{0, 0, GetSystemMetrics(SM_CXSCREEN), GetSystemMetrics(SM_CYSCREEN)}
}

func MonitorFromPoint(p POINT) HWND {
	r, _, _ := pMonitorFromPoint.Call(uintptr(p.X), uintptr(p.Y), 0)
	return HWND(r)
}

func MonitorFromWindow(h HWND) HWND {
	r, _, _ := pMonitorFromWindow.Call(uintptr(h), 0)
	return HWND(r)
}

func (m HWND) MonitorInfo() MONITORINFO {
	mi := MONITORINFO{CbSize: uint32(unsafe.Sizeof(MONITORINFO{}))}
	pGetMonitorInfoW.Call(uintptr(m), uintptr(unsafe.Pointer(&mi)))
	return mi
}

func DrawText(dc HDC, text string, r *RECT, flags uint32) {
	pDrawTextW.Call(uintptr(dc), uintptr(unsafe.Pointer(UTF16Ptr(text))), ^uintptr(0), uintptr(unsafe.Pointer(r)), uintptr(flags))
}

func TextExtent(dc HDC, text string) SIZE {
	var s SIZE
	pGetTextExtentPoint32W.Call(uintptr(dc), uintptr(unsafe.Pointer(UTF16Ptr(text))), uintptr(len([]rune(text))), uintptr(unsafe.Pointer(&s)))
	return s
}

func FillRect(dc HDC, r *RECT, br HBRUSH) {
	pFillRect.Call(uintptr(dc), uintptr(unsafe.Pointer(r)), uintptr(br))
}

func MessageBox(text, title string, flags uint32) int {
	r, _, _ := pMessageBoxW.Call(0,
		uintptr(unsafe.Pointer(UTF16Ptr(text))),
		uintptr(unsafe.Pointer(UTF16Ptr(title))),
		uintptr(flags))
	return int(r)
}

func RegisterHotKey(h HWND, id int32, mods uint32, vk uint32) (bool, uint32) {
	r, _, e := pRegisterHotKey.Call(uintptr(h), uintptr(id), uintptr(mods), uintptr(vk))
	errno, _ := e.(syscall.Errno)
	return r != 0, uint32(errno)
}

func UnregisterHotKey(h HWND, id int32) {
	pUnregisterHotKey.Call(uintptr(h), uintptr(id))
}

func SetProcessDPIAware() {
	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
	if pSetProcessDPI.Find() == nil {
		if r, _, _ := pSetProcessDPI.Call(^uintptr(3)); r != 0 {
			return
		}
	}
	if pSetProcessDPIAware.Find() == nil {
		pSetProcessDPIAware.Call()
	}
}

func GetKeyState(vk int) int16 {
	r, _, _ := pGetKeyStateVK.Call(uintptr(vk))
	return int16(r)
}

// ---------- message loop ----------

func GetMessage(m *MSG) int32 {
	r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(m)), 0, 0, 0)
	return int32(r)
}

func PeekMessage(m *MSG, filterMin, filterMax uint32, remove bool) bool {
	rm := uintptr(0)
	if remove {
		rm = 1
	}
	r, _, _ := pPeekMessageW.Call(uintptr(unsafe.Pointer(m)), 0, uintptr(filterMin), uintptr(filterMax), rm)
	return r != 0
}

func TranslateMessage(m *MSG) { pTranslateMessage.Call(uintptr(unsafe.Pointer(m))) }

func DispatchMessage(m *MSG) { pDispatchMessageW.Call(uintptr(unsafe.Pointer(m))) }

func PostQuitMessage(code int32) {
	pPostQuitMessage.Call(uintptr(code))
}

func DefWindowProc(h HWND, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := pDefWindowProcW.Call(uintptr(h), uintptr(msg), wp, lp)
	return r
}

func SetTimer(h HWND, id uintptr, ms uint32) {
	pSetTimer.Call(uintptr(h), id, uintptr(ms), 0)
}

func KillTimer(h HWND, id uintptr) {
	pKillTimer.Call(uintptr(h), id)
}

// ---------- dwm ----------

func DwmGetWindowAttributeInt32(h HWND, attr uint32) (int32, bool) {
	var v int32
	hr, _, _ := pDwmGetWindowAttribute.Call(uintptr(h), uintptr(attr), uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v))
	return v, int32(hr) == 0
}

func DwmGetWindowAttributeRect(h HWND, attr uint32) (RECT, bool) {
	var r RECT
	hr, _, _ := pDwmGetWindowAttribute.Call(uintptr(h), uintptr(attr), uintptr(unsafe.Pointer(&r)), unsafe.Sizeof(r))
	return r, int32(hr) == 0
}

func (h HWND) IsCloaked() bool {
	v, ok := DwmGetWindowAttributeInt32(h, DWM_CLOAKED)
	return ok && v != 0
}

// FrameRect returns the visible (shadow-less) window rectangle.
func (h HWND) FrameRect() RECT {
	if r, ok := DwmGetWindowAttributeRect(h, DWM_EXTENDED_FRAME_BOUNDS); ok && !r.Empty() {
		return r
	}
	return h.Rect()
}

func FlushDwm() { pDwmFlush.Call() }

// ---------- shell ----------

func ShellExecute(verb, file, params, dir string, show int32) {
	var vp, fp, pp, dp *uint16
	if verb != "" {
		vp = UTF16Ptr(verb)
	}
	if file != "" {
		fp = UTF16Ptr(file)
	}
	if params != "" {
		pp = UTF16Ptr(params)
	}
	if dir != "" {
		dp = UTF16Ptr(dir)
	}
	pShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(vp)),
		uintptr(unsafe.Pointer(fp)),
		uintptr(unsafe.Pointer(pp)),
		uintptr(unsafe.Pointer(dp)),
		uintptr(show))
}

const (
	FOLDERID_Downloads      = "{374DE290-123F-4565-9164-39C4925E467B}"
	FOLDERID_Pictures       = "{33E28130-4E1E-4676-835A-98395C3BC3BB}"
	FOLDERID_Desktop        = "{B4BFCC3A-DB2C-424C-B029-7FE99A87C641}"
	FOLDERID_AppData        = "{3EB685DB-65F9-4CF6-A03A-E3EF65729F3D}"
	FOLDERID_RoamingAppData = "{3EB685DB-65F9-4CF6-A03A-E3EF65729F3D}"
	FOLDERID_LocalAppData   = "{F1B32785-6FBA-4FCF-9D55-7B8E7F157091}"
)

type GUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

func GUIDFromString(s string) (GUID, bool) {
	var g GUID
	// only plain {XXXXXXXX-XXXX-XXXX-XXXX-XXXXXXXXXXXX} form is used here
	if len(s) != 38 || s[0] != '{' || s[37] != '}' {
		return g, false
	}
	hex := func(c byte) (uint32, bool) {
		switch {
		case c >= '0' && c <= '9':
			return uint32(c - '0'), true
		case c >= 'a' && c <= 'f':
			return uint32(c-'a') + 10, true
		case c >= 'A' && c <= 'F':
			return uint32(c-'A') + 10, true
		}
		return 0, false
	}
	val := func(start, n int) (uint32, bool) {
		var v uint32
		for i := 0; i < n; i++ {
			d, ok := hex(s[start+i])
			if !ok {
				return 0, false
			}
			v = v<<4 | d
		}
		return v, true
	}
	var ok bool
	if g.Data1, ok = val(1, 8); !ok {
		return g, false
	}
	v2, ok := val(10, 4)
	if !ok {
		return g, false
	}
	g.Data2 = uint16(v2)
	v3, ok := val(15, 4)
	if !ok {
		return g, false
	}
	g.Data3 = uint16(v3)
	for i := 0; i < 2; i++ {
		v, ok := val(20+i*2, 2)
		if !ok {
			return g, false
		}
		g.Data4[i] = byte(v)
	}
	for i := 0; i < 6; i++ {
		v, ok := val(25+i*2, 2)
		if !ok {
			return g, false
		}
		g.Data4[2+i] = byte(v)
	}
	return g, true
}

// KnownFolderPath resolves a KNOWNFOLDERID (e.g. FOLDERID_Downloads).
func KnownFolderPath(id string) string {
	g, ok := GUIDFromString(id)
	if !ok {
		return ""
	}
	var p *uint16
	hr, _, _ := pSHGetKnownFolder.Call(uintptr(unsafe.Pointer(&g)), 0, 0, uintptr(unsafe.Pointer(&p)))
	if int32(hr) != 0 || p == nil {
		return ""
	}
	s := syscall.UTF16ToString((*[32768]uint16)(unsafe.Pointer(p))[:])
	pCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
	return s
}

func BeginPaint(h HWND, ps *PAINTSTRUCT) HDC {
	r, _, _ := pBeginPaint.Call(uintptr(h), uintptr(unsafe.Pointer(ps)))
	return HDC(r)
}

func EndPaint(h HWND, ps *PAINTSTRUCT) {
	pEndPaint.Call(uintptr(h), uintptr(unsafe.Pointer(ps)))
}

// PaintDC prepares a paint cycle and returns the DC plus a closer.
func (h HWND) Paint() (HDC, func()) {
	var ps PAINTSTRUCT
	dc := BeginPaint(h, &ps)
	return dc, func() { EndPaint(h, &ps) }
}

// UpdateLayeredWindow pushes a premultiplied ARGB DIB to a layered window.
func UpdateLayeredWindow(hwnd HWND, screenDC HDC, memDC HDC, x, y, w, h int32, alpha byte) (bool, uint32) {
	pos := POINT{X: x, Y: y}
	size := SIZE{CX: w, CY: h}
	src := POINT{0, 0}
	bf := BLENDFUNCTION{BlendOp: AC_SRC_OVER, BlendFlags: 0, SourceConstantAlpha: 255, AlphaFormat: AC_SRC_ALPHA}
	r, _, e := pUpdateLayeredWindow.Call(uintptr(hwnd), uintptr(screenDC), uintptr(unsafe.Pointer(&pos)),
		uintptr(unsafe.Pointer(&size)), uintptr(memDC), uintptr(unsafe.Pointer(&src)), 0,
		uintptr(unsafe.Pointer(&bf)), ULW_ALPHA)
	errno, _ := e.(syscall.Errno)
	return r != 0, uint32(errno)
}

// HideCaretlessShow shows a window without activating it.
func (h HWND) ShowNoActivate() { pShowWindow.Call(uintptr(h), SW_SHOWNOACTIVATE) }

// UpdateLayeredWindowOpaque pushes the frame without per pixel alpha.
func UpdateLayeredWindowOpaque(hwnd HWND, screenDC HDC, memDC HDC, x, y, w, h int32) (bool, uint32) {
	pos := POINT{X: x, Y: y}
	size := SIZE{CX: w, CY: h}
	src := POINT{0, 0}
	bf := BLENDFUNCTION{BlendOp: AC_SRC_OVER, BlendFlags: 0, SourceConstantAlpha: 255, AlphaFormat: 0}
	r, _, e := pUpdateLayeredWindow.Call(uintptr(hwnd), uintptr(screenDC), uintptr(unsafe.Pointer(&pos)),
		uintptr(unsafe.Pointer(&size)), uintptr(memDC), uintptr(unsafe.Pointer(&src)), 0,
		uintptr(unsafe.Pointer(&bf)), ULW_ALPHA)
	errno, _ := e.(syscall.Errno)
	return r != 0, uint32(errno)
}

// PaintBegin starts a WM_PAINT cycle and exposes the damaged rectangle.
func (h HWND) PaintBegin() (HDC, RECT, func()) {
	var ps PAINTSTRUCT
	dc := BeginPaint(h, &ps)
	return dc, ps.RcPaint, func() { EndPaint(h, &ps) }
}
