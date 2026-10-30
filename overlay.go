//go:build windows

package main

import (
	"github.com/dvygolov/yasshoter/internal/win"
)

const (
	selColor  = 0x3B82F6
	hoverEdge = 0x60A5FA
	labelBg   = 0x0B1220
	labelFg   = 0xE6EDF7
	mouseSlop = 5
)

// Overlay is the full screen frozen-frame selection window.
//
// It is a plain top most popup that paints from a prepared frame buffer and only
// invalidates the rectangle that actually changed. A layered window would push
// the whole ~18 MB frame to the compositor on every mouse move; this repaints a
// few thousand pixels instead, so dragging stays smooth even on old GPUs.
type Overlay struct {
	app  *App
	hwnd win.HWND

	windows []win.WindowInfo

	hover    win.RECT
	hasHover bool

	dragging bool
	start    win.POINT
	cur      win.POINT

	hoverTitle string

	drawn   win.RECT // highlight currently baked into the frame
	drawnLb win.RECT // size label currently baked into the frame

	font win.HFONT
	dim  uint32

	undimmed bool // first refresh has to dim the whole frozen screen
}

// newOverlay builds the overlay window once, hidden. Creating it during startup
// instead of on the first capture keeps PrintScreen -> visible well under 100 ms.
func newOverlay(a *App) *Overlay {
	o := &Overlay{app: a, dim: uint32(a.cfg.DimAlpha)}
	if o.dim < 60 || o.dim > 220 {
		o.dim = 140
	}
	o.font = win.CreateFont(15, win.FW_SEMIBOLD, "Segoe UI")

	name := "YASShoterOverlay"
	win.RegisterClass(name, win.IDC_CROSS)
	id := win.NewHandler(o.wndProc)
	scr := win.VirtualScreen()
	o.hwnd = win.CreateWindowEx(
		uint32(win.WS_EX_TOPMOST|win.WS_EX_TOOLWINDOW),
		name, appTitle, uint32(win.WS_POPUP),
		scr.Left, scr.Top, scr.W(), scr.H(), 0, 0, uintptr(id))
	return o
}

// fit makes sure the hidden window matches the current virtual screen.
func (o *Overlay) fit() {
	scr := o.app.screen
	if r := o.hwnd.Rect(); r != scr {
		o.hwnd.SetPos(win.HWND_TOPMOST, scr.Left, scr.Top, scr.W(), scr.H(),
			win.SWP_NOACTIVATE|win.SWP_NOZORDER)
	}
}

func (o *Overlay) show() {
	if o.hwnd == 0 {
		return
	}
	o.fit()
	o.windows = win.CapturableWindows(win.SelfPID(), o.app.screen)
	o.hover = win.RECT{}
	o.hasHover = false
	o.hoverTitle = ""
	o.drawn = win.RECT{}
	o.drawnLb = win.RECT{}
	o.dragging = false
	o.undimmed = true
	o.hwnd.Show(win.SW_SHOW)
	win.ForceForeground(o.hwnd)
	o.updateHover(win.GetCursorPos())
	o.refresh()
	o.hwnd.Update()
}

// hide parks the window; the DIBs are owned by App and released right away.
func (o *Overlay) hide() {
	if o.hwnd != 0 {
		o.hwnd.Show(win.SW_HIDE)
	}
}

func (o *Overlay) destroy() {
	o.hide()
	if o.hwnd != 0 {
		h := o.hwnd
		o.hwnd = 0
		h.Destroy()
	}
	if o.font != 0 {
		win.DeleteObject(win.HGDIOBJ(o.font))
		o.font = 0
	}
}

// local converts a screen point to frozen-frame coordinates.
func (o *Overlay) local(p win.POINT) win.POINT {
	return win.POINT{X: p.X - o.app.screen.Left, Y: p.Y - o.app.screen.Top}
}

func (o *Overlay) updateHover(screenPt win.POINT) {
	p := o.local(screenPt)
	o.hasHover = false
	for _, w := range o.windows {
		r := win.RECT{
			Left:   w.Rect.Left - o.app.screen.Left,
			Top:    w.Rect.Top - o.app.screen.Top,
			Right:  w.Rect.Right - o.app.screen.Left,
			Bottom: w.Rect.Bottom - o.app.screen.Top,
		}
		if p.X >= r.Left && p.X < r.Right && p.Y >= r.Top && p.Y < r.Bottom {
			o.hover = o.clip(r)
			o.hoverTitle = shorten(w.Title, 48)
			o.hasHover = true
			return
		}
	}
}

func (o *Overlay) clip(r win.RECT) win.RECT {
	s := win.RECT{0, 0, o.app.screen.W(), o.app.screen.H()}
	if r.Left < s.Left {
		r.Left = s.Left
	}
	if r.Top < s.Top {
		r.Top = s.Top
	}
	if r.Right > s.Right {
		r.Right = s.Right
	}
	if r.Bottom > s.Bottom {
		r.Bottom = s.Bottom
	}
	return r
}

func (o *Overlay) dragRect() win.RECT {
	cur := o.local(o.cur)
	start := o.local(o.start)
	return o.clip(win.NormalizeRect(start.X, start.Y, cur.X, cur.Y))
}

// refresh rebuilds only the changed part of the frame and asks the window to
// repaint exactly that rectangle.
func (o *Overlay) refresh() {
	frame := o.app.frame
	src := o.app.src
	if frame == nil || src == nil || o.hwnd == 0 {
		return
	}
	var target win.RECT
	if o.dragging {
		target = o.dragRect()
	} else if o.hasHover {
		target = o.hover
	}

	dirty := win.Inflate(win.Union(win.Union(o.drawn, target), o.drawnLb), 4)
	if o.undimmed {
		dirty = win.RECT{0, 0, o.app.screen.W(), o.app.screen.H()}
		o.undimmed = false
	}
	if dirty.Empty() {
		return
	}
	// restore the dim background, then paint the new highlight on top
	frame.Dim(src, dirty, o.dim)
	o.drawnLb = win.RECT{}

	if !target.Empty() {
		if o.dragging {
			// bright, unmodified pixels: what will actually be captured
			frame.CopyRegion(src, target)
			frame.StrokeRect(target, 1, selColor)
		} else {
			frame.StrokeRect(win.Inflate(target, -1), 1, hoverEdge)
			frame.StrokeRect(win.Inflate(target, -2), 1, 0x1D4ED8)
		}
		o.drawnLb = o.drawLabel(target, o.dragging)
	}
	o.drawn = target

	dirty = win.Inflate(win.Union(dirty, o.drawnLb), 2)
	o.hwnd.InvalidateRect(&dirty, false)
	o.hwnd.Update()
}

// drawLabel renders the "w × h" pill and returns its rectangle.
func (o *Overlay) drawLabel(target win.RECT, dragging bool) win.RECT {
	text := itoa(int64(target.W())) + " × " + itoa(int64(target.H()))
	if !dragging && o.hoverTitle != "" {
		text = o.hoverTitle + "   " + text
	}
	old := win.SelectObject(o.app.frame.DC, win.HGDIOBJ(o.font))
	sz := win.TextExtent(o.app.frame.DC, text)
	win.SelectObject(o.app.frame.DC, old)

	bw := sz.CX + 20
	if bw > o.app.screen.W()-8 {
		bw = o.app.screen.W() - 8
	}
	bh := int32(24)
	x := target.Right - bw
	y := target.Bottom + 8
	if y+bh > o.app.screen.H()-2 {
		y = target.Bottom - bh - 8
	}
	if x < 2 {
		x = 2
	}
	if y < 2 {
		y = 2
	}
	r := win.RECT{x, y, x + bw, y + bh}
	o.app.frame.FillRectBlend(r, labelBg, 0.88)
	tr := r
	o.app.frame.TextOut(text, &tr, labelFg, o.font, win.DT_CENTER|win.DT_VCENTER|win.DT_SINGLELINE|win.DT_NOPREFIX)
	return r
}

func (o *Overlay) finish(sel win.RECT) {
	if sel.Empty() || sel.W() < 2 || sel.H() < 2 {
		o.cancel()
		return
	}
	screen := win.RECT{
		Left:   sel.Left + o.app.screen.Left,
		Top:    sel.Top + o.app.screen.Top,
		Right:  sel.Right + o.app.screen.Left,
		Bottom: sel.Bottom + o.app.screen.Top,
	}
	o.hide()
	o.app.startEditor(screen)
}

func (o *Overlay) cancel() {
	o.app.afterCapture()
}

func (o *Overlay) wndProc(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case win.WM_PAINT:
		dc, r, done := hwnd.PaintBegin()
		o.app.frame.BlitRectToDC(dc, r)
		done()
		return 0
	case win.WM_ERASEBKGND:
		return 1
	case win.WM_MOUSEMOVE:
		p := win.GetCursorPos()
		if o.dragging {
			o.cur = p
			o.refresh()
		} else {
			old := o.hover
			oldOK := o.hasHover
			o.updateHover(p)
			if old != o.hover || oldOK != o.hasHover {
				o.refresh()
			}
		}
		return 0
	case win.WM_LBUTTONDOWN:
		p := win.GetCursorPos()
		o.start = p
		o.cur = p
		o.dragging = true
		o.hasHover = false
		win.SetCapture(hwnd)
		o.refresh()
		return 0
	case win.WM_LBUTTONUP:
		if !o.dragging {
			return 0
		}
		o.dragging = false
		win.ReleaseCapture()
		o.cur = win.GetCursorPos()
		r := o.dragRect()
		if r.W() <= mouseSlop || r.H() <= mouseSlop {
			o.updateHover(o.cur)
			if o.hasHover {
				o.finish(o.hover)
				return 0
			}
			o.cancel()
			return 0
		}
		o.finish(r)
		return 0
	case win.WM_RBUTTONDOWN, win.WM_RBUTTONUP:
		o.cancel()
		return 0
	case win.WM_KEYDOWN:
		switch uint32(wp) {
		case win.VK_ESCAPE:
			o.cancel()
		case win.VK_RETURN:
			if o.hasHover {
				o.finish(o.hover)
			} else if !o.dragRect().Empty() {
				o.finish(o.dragRect())
			}
		}
		return 0
	case win.WM_SETCURSOR:
		win.SetCursor(win.LoadCursor(0, win.IDC_CROSS))
		return 1
	}
	return win.DefWindowProc(hwnd, msg, wp, lp)
}
