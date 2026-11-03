//go:build windows

package main

import (
	"github.com/dvygolov/yasshoter/internal/win"
)

// recBorder is a thin red frame around the recorded area: four borderless,
// click-through strips that never take focus.
type recBorder struct {
	hwnds [4]win.HWND
	brush win.HBRUSH
}

const recBorderClass = "YASShoterRecBorder"

func newRecBorder(region win.RECT, thickness int32) *recBorder {
	if thickness < 1 {
		thickness = 1
	}
	win.RegisterClass(recBorderClass, 0)
	b := &recBorder{brush: win.CreateSolidBrush(win.ColorRef(0xE5484D))}
	ex := uint32(win.WS_EX_TOPMOST | win.WS_EX_TOOLWINDOW | win.WS_EX_NOACTIVATE | win.WS_EX_TRANSPARENT)
	style := uint32(win.WS_POPUP)
	r := region
	// drawn inside the recorded area so the frame is always on screen
	rects := [4]win.RECT{
		{r.Left, r.Top, r.Right, r.Top + thickness},       // top
		{r.Left, r.Bottom - thickness, r.Right, r.Bottom}, // bottom
		{r.Left, r.Top, r.Left + thickness, r.Bottom},     // left
		{r.Right - thickness, r.Top, r.Right, r.Bottom},   // right
	}
	for i, rr := range rects {
		if rr.W() <= 0 || rr.H() <= 0 {
			continue
		}
		id := win.NewHandler(b.wndProc)
		h := win.CreateWindowEx(ex, recBorderClass, appTitle, style,
			rr.Left, rr.Top, rr.W()-rr.Left, rr.Bottom-rr.Top, 0, 0, id)
		if h == 0 {
			continue
		}
		b.hwnds[i] = h
		h.Show(win.SW_SHOWNOACTIVATE)
	}
	return b
}

func (b *recBorder) Close() {
	for _, h := range b.hwnds {
		if h != 0 {
			h.Destroy()
		}
	}
	if b.brush != 0 {
		win.DeleteObject(win.HGDIOBJ(b.brush))
		b.brush = 0
	}
}

func (b *recBorder) wndProc(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case win.WM_PAINT:
		dc, r, done := hwnd.PaintBegin()
		win.FillRect(dc, &r, b.brush)
		done()
		return 0
	case win.WM_ERASEBKGND:
		return 1
	case win.WM_MOUSEACTIVATE:
		return 3
	}
	return win.DefWindowProc(hwnd, msg, wp, lp)
}

func clipTo(r win.RECT, clip win.RECT) win.RECT {
	if r.Left < clip.Left {
		r.Left = clip.Left
	}
	if r.Top < clip.Top {
		r.Top = clip.Top
	}
	if r.Right > clip.Right {
		r.Right = clip.Right
	}
	if r.Bottom > clip.Bottom {
		r.Bottom = clip.Bottom
	}
	if r.Right < r.Left {
		r.Right = r.Left
	}
	if r.Bottom < r.Top {
		r.Bottom = r.Top
	}
	return r
}
