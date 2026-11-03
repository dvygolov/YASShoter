//go:build windows

package win

import (
	"sync"
	"unsafe"
)

// Cursor drawing for the recorder: the mouse pointer has to be composited into
// each captured frame, because GDI BitBlt never includes it.

type CURSORINFO struct {
	CbSize      uint32
	Flags       uint32
	HCursor     uintptr
	PtScreenPos POINT
}

const (
	CURSOR_SHOWING    = 0x00000001
	CURSOR_SUPPRESSED = 0x00000002
	DI_NORMAL         = 0x0003
)

var (
	pGetCursorInfo = dllUser32.NewProc("GetCursorInfo")
	pDrawIconEx    = dllUser32.NewProc("DrawIconEx")
)

// GetCursorInfo returns the current cursor handle, visibility and position.
func GetCursorInfo() (CURSORINFO, bool) {
	ci := CURSORINFO{CbSize: uint32(unsafe.Sizeof(CURSORINFO{}))}
	r, _, _ := pGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci)))
	if r == 0 || ci.HCursor == 0 {
		return ci, false
	}
	return ci, ci.Flags&CURSOR_SHOWING != 0
}

type hotspot struct{ x, y int32 }

var (
	hotMu    sync.Mutex
	hotCache = map[uintptr]hotspot{}
)

// cursorHotspot caches ICONINFO lookups: GetIconInfo allocates two bitmaps per
// call, which would be far too much work at 30 frames per second.
func cursorHotspot(h uintptr) hotspot {
	hotMu.Lock()
	if hs, ok := hotCache[h]; ok {
		hotMu.Unlock()
		return hs
	}
	hotMu.Unlock()

	var ii ICONINFO
	hs := hotspot{}
	if r, _, _ := pGetIconInfo.Call(h, uintptr(unsafe.Pointer(&ii))); r != 0 {
		hs.x = int32(ii.XHotspot)
		hs.y = int32(ii.YHotspot)
		if ii.HbmMask != 0 {
			pDeleteObject.Call(uintptr(ii.HbmMask))
		}
		if ii.HbmColor != 0 {
			pDeleteObject.Call(uintptr(ii.HbmColor))
		}
	}
	hotMu.Lock()
	if len(hotCache) > 64 {
		hotCache = map[uintptr]hotspot{}
	}
	hotCache[h] = hs
	hotMu.Unlock()
	return hs
}

// DrawCursor composites the cursor onto dc so that its hot spot lands on (x,y).
func DrawCursor(dc HDC, x, y int32, cursor uintptr) {
	if cursor == 0 {
		return
	}
	hs := cursorHotspot(cursor)
	pDrawIconEx.Call(uintptr(dc), uintptr(int(x-hs.x)), uintptr(int(y-hs.y)),
		cursor, 0, 0, 0, 0, DI_NORMAL)
}
