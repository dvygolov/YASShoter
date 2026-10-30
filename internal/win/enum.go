//go:build windows

package win

import (
	"syscall"
	"unsafe"
)

func ptrAt(b *[]byte, i int) unsafe.Pointer { return unsafe.Pointer(&(*b)[i]) }

// WindowInfo describes a capturable top-level window.
type WindowInfo struct {
	Hwnd  HWND
	Rect  RECT
	PID   uint32
	Title string
}

var enumCB = syscall.NewCallback(func(hwnd HWND, lparam uintptr) uintptr {
	ctx := (*enumCtx)(unsafe.Pointer(lparam))
	if ctx == nil {
		return 1
	}
	if len(ctx.out) >= 4096 {
		return 0
	}
	ctx.out = append(ctx.out, WindowInfo{Hwnd: hwnd})
	ctx.n++
	return 1
})

type enumCtx struct {
	out []WindowInfo
	n   int
}

// TopLevelWindows returns visible top level windows in Z order (top first).
func TopLevelWindows() []WindowInfo {
	ctx := &enumCtx{out: make([]WindowInfo, 0, 256)}
	pEnumWindows.Call(enumCB, uintptr(unsafe.Pointer(ctx)))
	for i := range ctx.out {
		h := ctx.out[i].Hwnd
		ctx.out[i].Rect = h.FrameRect()
		ctx.out[i].PID = h.PID()
	}
	return ctx.out
}

// CapturableWindows filters the Z-ordered window list down to windows a user
// would expect to snap to, mirroring ShareX's WindowsRectangleList rules.
func CapturableWindows(selfPID uint32, screen RECT) []WindowInfo {
	all := TopLevelWindows()
	out := make([]WindowInfo, 0, len(all))
	skipClass := map[string]bool{
		"Progman":                    true,
		"WorkerW":                    true,
		"Shell_TrayWnd":              true,
		"Shell_SecondaryTrayWnd":     true,
		"Windows.UI.Core.CoreWindow": true,
		"ApplicationFrameWindow":     false,
	}
	for _, w := range all {
		if w.PID == selfPID {
			continue
		}
		if w.Rect.Empty() || w.Rect.W() < 24 || w.Rect.H() < 24 {
			continue
		}
		if skipClass[w.Hwnd.ClassName()] {
			continue
		}
		st := w.Hwnd.Style()
		if st&WS_CHILD != 0 || st&WS_DISABLED != 0 {
			continue
		}
		ex := w.Hwnd.ExStyle()
		if ex&(WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE) != 0 && ex&WS_EX_APPWINDOW == 0 {
			continue
		}
		if ex&WS_EX_TRANSPARENT != 0 {
			continue
		}
		if w.Hwnd.IsCloaked() {
			continue
		}
		if !w.Hwnd.IsVisible() {
			continue
		}
		// must intersect the visible virtual screen
		if w.Rect.Right <= screen.Left || w.Rect.Left >= screen.Right ||
			w.Rect.Bottom <= screen.Top || w.Rect.Top >= screen.Bottom {
			continue
		}
		w.Title = w.Hwnd.Title()
		out = append(out, w)
	}
	return out
}

// WindowAt returns the topmost capturable window containing the point.
func WindowAt(pt POINT, selfPID uint32, screen RECT) (WindowInfo, bool) {
	for _, w := range CapturableWindows(selfPID, screen) {
		if pt.X >= w.Rect.Left && pt.X < w.Rect.Right && pt.Y >= w.Rect.Top && pt.Y < w.Rect.Bottom {
			return w, true
		}
	}
	return WindowInfo{}, false
}

var selfPID uint32 = func() uint32 {
	r, _, _ := dllKernel32.NewProc("GetCurrentProcessId").Call()
	return uint32(r)
}()

// SelfPID returns the process id of the running program.
func SelfPID() uint32 { return selfPID }
