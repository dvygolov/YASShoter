//go:build windows

package main

import (
	"github.com/dvygolov/yasshoter/internal/win"
)

// recBorder is the frame shown around the recorded area while recording: same
// blue as the selection, and strictly *outside* the captured rectangle so it can
// never end up in the video. It is a single layered window: the four strips are
// painted into an ARGB bitmap, everything inside stays fully transparent.
type recBorder struct {
	hwnd win.HWND
	dib  *win.Dib
}

const recBorderClass = "YASShoterRecBorder"

func newRecBorder(region win.RECT, thickness int32) *recBorder {
	if thickness < 1 {
		thickness = 1
	}
	screen := win.VirtualScreen()

	// outer box, clipped to the visible desktop
	x0 := region.Left - thickness
	y0 := region.Top - thickness
	x1 := region.Right + thickness
	y1 := region.Bottom + thickness
	if x0 < screen.Left {
		x0 = screen.Left
	}
	if y0 < screen.Top {
		y0 = screen.Top
	}
	if x1 > screen.Right {
		x1 = screen.Right
	}
	if y1 > screen.Bottom {
		y1 = screen.Bottom
	}
	w, h := x1-x0, y1-y0
	if w <= 0 || h <= 0 {
		return nil
	}

	dib, err := win.NewDib(w, h)
	if err != nil {
		return nil
	}
	dib.FillConstant(0) // fully transparent

	// the region in local coordinates: everything outside it is frame
	ix0 := region.Left - x0
	iy0 := region.Top - y0
	ix1 := region.Right - x0
	iy1 := region.Bottom - y0
	blue := uint32(0x3B82F6)
	if iy0 > 0 {
		dib.FillRectColor(win.RECT{0, 0, w, iy0}, blue)
	}
	if iy1 < h {
		dib.FillRectColor(win.RECT{0, iy1, w, h}, blue)
	}
	if ix0 > 0 {
		dib.FillRectColor(win.RECT{0, iy0, ix0, iy1}, blue)
	}
	if ix1 < w {
		dib.FillRectColor(win.RECT{ix1, iy0, w, iy1}, blue)
	}

	win.RegisterClass(recBorderClass, 0)
	id := win.NewHandler(func(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
		if msg == win.WM_MOUSEACTIVATE {
			return 3 // never steal focus from the app being recorded
		}
		return win.DefWindowProc(hwnd, msg, wp, lp)
	})
	hwnd := win.CreateWindowEx(
		uint32(win.WS_EX_LAYERED|win.WS_EX_TOPMOST|win.WS_EX_TOOLWINDOW|win.WS_EX_NOACTIVATE|win.WS_EX_TRANSPARENT),
		recBorderClass, appTitle, uint32(win.WS_POPUP),
		x0, y0, w, h, 0, 0, id)
	if hwnd == 0 {
		dib.Close()
		return nil
	}
	b := &recBorder{hwnd: hwnd, dib: dib}
	hwnd.Show(win.SW_SHOWNOACTIVATE)
	b.push()
	logf("border: frame %dx%d at %d,%d (region %dx%d)", w, h, x0, y0, region.W(), region.H())
	return b
}

func (b *recBorder) push() {
	sdc, release := win.ScreenDC()
	defer release()
	r := b.hwnd.Rect()
	win.UpdateLayeredWindow(b.hwnd, sdc, b.dib.DC, r.Left, r.Top, b.dib.W, b.dib.H, 255)
}

func (b *recBorder) Close() {
	if b.hwnd != 0 {
		h := b.hwnd
		b.hwnd = 0
		h.Destroy()
	}
	if b.dib != nil {
		b.dib.Close()
		b.dib = nil
	}
}
