//go:build windows

package win

import (
	"syscall"
	"unsafe"
)

var dllShcore = syscall.NewLazyDLL("shcore.dll")

var (
	pGetDpiForMonitor = dllShcore.NewProc("GetDpiForMonitor")
	pGetDeviceCaps    = dllGdi32.NewProc("GetDeviceCaps")
)

const (
	MDT_EFFECTIVE_DPI = 0
	MDT_ANGULAR_DPI   = 1
	MDT_RAW_DPI       = 2
	LOGPIXELSX        = 88
)

// DpiForWindow returns the effective DPI of the monitor a window is on.
// 96 means 100% scaling, 144 means 150%, 192 means 200%.
func DpiForWindow(h HWND) uint32 {
	if h != 0 && pGetDpiForWindow.Find() == nil {
		if r, _, _ := pGetDpiForWindow.Call(uintptr(h)); r >= 96 && r <= 480 {
			return uint32(r)
		}
	}
	return SystemDPI()
}

// DpiForPoint returns the effective DPI of the monitor containing a point.
func DpiForPoint(p POINT) uint32 {
	m := MonitorFromPoint(p)
	if m != 0 && pGetDpiForMonitor.Find() == nil {
		var x, y uint32
		if r, _, _ := pGetDpiForMonitor.Call(uintptr(m), MDT_EFFECTIVE_DPI,
			uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y))); r == 0 && x >= 96 && x <= 480 {
			return x
		}
	}
	if h := GetForegroundWindow(); h != 0 {
		if d := DpiForWindow(h); d != 0 {
			return d
		}
	}
	return SystemDPI()
}

// SystemDPI falls back to the desktop DPI for old systems.
func SystemDPI() uint32 {
	dc := GetDC(0)
	if dc == 0 {
		return 96
	}
	defer ReleaseDC(0, dc)
	r, _, _ := pGetDeviceCaps.Call(uintptr(dc), LOGPIXELSX)
	if r >= 96 && r <= 480 {
		return uint32(r)
	}
	return 96
}

// Scale converts a 96-DPI design value into physical pixels for the given DPI.
func Scale(v int32, dpi uint32) int32 {
	if dpi == 0 {
		dpi = 96
	}
	s := int32((int64(v)*int64(dpi) + 48) / 96)
	if v > 0 && s < 1 {
		s = 1
	}
	return s
}

// ScaleF is the float variant used by the rasterizer.
func ScaleF(v float64, dpi uint32) float64 {
	if dpi == 0 {
		dpi = 96
	}
	return v * float64(dpi) / 96
}

// ScaleFactor returns dpi/96.
func ScaleFactor(dpi uint32) float64 {
	if dpi == 0 {
		dpi = 96
	}
	return float64(dpi) / 96
}

// EnableDarkTitleBar asks DWM for a dark window frame (unused by borderless
// windows, kept for dialogs).
func EnableDarkTitleBar(h HWND) {
	v := int32(1)
	pDwmSetWindowAttribute.Call(uintptr(h), DWM_USE_IMMERSIVE_DARK_MODE,
		uintptr(unsafe.Pointer(&v)), unsafe.Sizeof(v))
}

var _ = syscall.Handle(0)
