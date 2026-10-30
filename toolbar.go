//go:build windows

package main

import (
	"math"

	"github.com/dvygolov/yasshoter/internal/win"
)

const (
	tbH   = 42
	tbPad = 8
	tbBtn = 30
	tbGap = 4
)

type tbKind int

const (
	tbTool tbKind = iota
	tbWidth
	tbColor
	tbAction
)

const (
	actUndo = iota
	actCopy
	actSave
	actCancel
)

type tbButton struct {
	kind  tbKind
	tool  ToolKind
	width float64
	color uint32
	act   int
	rect  win.RECT
}

// Toolbar is the floating, never-activating tool strip below the capture area.
type Toolbar struct {
	ed    *Editor
	hwnd  win.HWND
	buf   *win.Dib
	w, h  int32
	btns  []tbButton
	seps  []int32
	hover int
	msg   string
	font  win.HFONT
	small win.HFONT
}

var palette = []uint32{
	0xFF3B30, // red (default)
	0xFF9500, // orange
	0xFFD60A, // yellow
	0x34C759, // green
	0x0A84FF, // blue
	0xFFFFFF, // white
	0x000000, // black
}

var widths = []float64{2, 4, 6, 9}

func nextWidth(cur float64, dir int) float64 {
	idx := 1
	for i, w := range widths {
		if w == cur {
			idx = i
		}
	}
	idx += dir
	if idx < 0 {
		idx = 0
	}
	if idx >= len(widths) {
		idx = len(widths) - 1
	}
	return widths[idx]
}

func formatColor(c uint32) string {
	const hex = "0123456789ABCDEF"
	b := make([]byte, 6)
	for i := 0; i < 3; i++ {
		shift := uint(16 - i*8)
		v := (c >> shift) & 0xFF
		b[i*2] = hex[v>>4]
		b[i*2+1] = hex[v&0xF]
	}
	return string(b)
}

func NewToolbar(ed *Editor) *Toolbar {
	tb := &Toolbar{ed: ed, h: tbH, hover: -1}
	tb.font = win.CreateFont(14, win.FW_SEMIBOLD, "Segoe UI")
	tb.small = win.CreateFont(13, win.FW_NORMAL, "Segoe UI")
	tb.layout()
	buf, err := win.NewDib(tb.w, tb.h)
	if err != nil {
		return nil
	}
	tb.buf = buf
	name := "YASShoterToolbar"
	win.RegisterClass(name, 0)
	id := win.NewHandler(tb.wndProc)
	tb.hwnd = win.CreateWindowEx(
		uint32(win.WS_EX_TOPMOST|win.WS_EX_TOOLWINDOW|win.WS_EX_NOACTIVATE),
		name, appTitle, uint32(win.WS_POPUP),
		0, 0, tb.w, tb.h, 0, 0, uintptr(id))
	tb.compose()
	return tb
}

func (tb *Toolbar) layout() {
	x := int32(tbPad)
	y := (int32(tbH) - tbBtn) / 2
	tb.seps = tb.seps[:0]
	sep := func() {
		tb.seps = append(tb.seps, x+3)
		x += 7
	}
	add := func(b tbButton, w int32) {
		b.rect = win.RECT{Left: x, Top: y, Right: x + w, Bottom: y + tbBtn}
		tb.btns = append(tb.btns, b)
		x += w + tbGap
	}
	tb.btns = tb.btns[:0]
	for _, t := range []ToolKind{ToolArrow, ToolRect, ToolEllipse, ToolPencil, ToolNumber} {
		add(tbButton{kind: tbTool, tool: t}, tbBtn)
	}
	sep()
	for _, wv := range widths {
		add(tbButton{kind: tbWidth, width: wv}, 26)
	}
	sep()
	for _, c := range palette {
		add(tbButton{kind: tbColor, color: c}, 26)
	}
	sep()
	for _, a := range []int{actUndo, actCopy, actSave, actCancel} {
		add(tbButton{kind: tbAction, act: a}, tbBtn)
	}
	x += 8
	tb.w = x + 210 + 6
}

func (tb *Toolbar) compose() {
	b := tb.buf
	b.FillRectColor(win.RECT{0, 0, tb.w, tb.h}, 0x20242B)
	b.StrokeRect(win.RECT{0, 0, tb.w, tb.h}, 1, 0x3B4250)
	ed := tb.ed
	for i, btn := range tb.btns {
		hovered := i == tb.hover
		active := false
		switch btn.kind {
		case tbTool:
			active = ed.tool == btn.tool
		case tbWidth:
			active = ed.width == btn.width
		case tbColor:
			active = ed.color == btn.color
		}
		r := btn.rect
		if active {
			b.FillRectColor(win.RECT{r.Left - 1, r.Top - 1, r.Right + 1, r.Bottom + 1}, 0x2B3A55)
		} else if hovered {
			b.FillRectColor(win.RECT{r.Left - 1, r.Top - 1, r.Right + 1, r.Bottom + 1}, 0x2C323D)
		}
		tb.drawButtonIcon(btn, r, active)
	}
	for _, sx := range tb.seps {
		b.FillRectColor(win.RECT{sx, 9, sx + 1, tbH - 9}, 0x3B4250)
	}
	// hint text
	hr := win.RECT{Left: tb.w - 214, Top: 0, Right: tb.w - 8, Bottom: tbH}
	txt := "Enter — сохранить · Esc — отмена"
	if tb.msg != "" {
		txt = tb.msg
	}
	b.TextOut(txt, &hr, 0x9AA4B2, tb.small, win.DT_RIGHT|win.DT_VCENTER|win.DT_SINGLELINE|win.DT_NOPREFIX|win.DT_END_ELLIPSIS)
}

func (tb *Toolbar) drawButtonIcon(btn tbButton, r win.RECT, active bool) {
	b := tb.buf
	var col uint32 = 0xE8EDF5
	if active {
		col = 0xFFFFFF
	}
	cx := (r.Left + r.Right) / 2
	cy := (r.Top + r.Bottom) / 2
	fx := float64(cx)
	fy := float64(cy)
	switch btn.kind {
	case tbTool:
		switch btn.tool {
		case ToolArrow:
			s := Shape{Tool: ToolArrow, Color: col, Width: 2.6, X0: fx - 7, Y0: fy + 7, X1: fx + 7, Y1: fy - 7}
			drawArrow(b, &s)
		case ToolRect:
			b.RectOutline(fx-7, fy-5.5, fx+7, fy+5.5, 2.2, col)
		case ToolEllipse:
			b.EllipseOutline(fx, fy, 7, 5.5, 2.2, col)
		case ToolPencil:
			b.Polyline([]float64{
				fx - 7, fy + 6,
				fx - 3, fy - 2,
				fx + 1, fy + 4,
				fx + 7, fy - 6,
			}, 2.2, col)
		case ToolNumber:
			b.FillCircle(fx, fy, 7, col)
			box := win.RECT{int32(fx) - 7, int32(fy) - 7, int32(fx) + 7, int32(fy) + 7}
			b.TextOut("1", &box, 0x20242B, tb.font, win.DT_CENTER|win.DT_VCENTER|win.DT_SINGLELINE|win.DT_NOPREFIX)
		}
	case tbWidth:
		b.FillCircle(fx, fy, btn.width/2, col)
		if !active {
			b.FillCircle(fx, fy, btn.width/2, 0xE8EDF5)
		}
	case tbColor:
		rr := win.RECT{int32(fx) - 8, int32(fy) - 8, int32(fx) + 8, int32(fy) + 8}
		b.FillRectColor(rr, btn.color)
		if active {
			b.StrokeRect(win.RECT{rr.Left - 2, rr.Top - 2, rr.Right + 2, rr.Bottom + 2}, 1, 0xFFFFFF)
		} else {
			b.StrokeRect(rr, 1, 0x11151A)
		}
	case tbAction:
		switch btn.act {
		case actUndo:
			pts := make([]float64, 0, 24)
			for a := 205.0; a <= 335.0; a += 13 {
				rad := a * 3.14159265 / 180
				pts = append(pts, fx+5.5*math.Cos(rad), fy+5.5*math.Sin(rad))
			}
			b.Polyline(pts, 2.1, col)
			hx := fx + 5.5*math.Cos(205*3.14159265/180)
			hy := fy + 5.5*math.Sin(205*3.14159265/180)
			b.FillTriangle(hx-3.5, hy+1.5, hx+3.0, hy-3.0, hx+3.0, hy+4.0, col)
		case actCopy:
			b.RectOutline(fx-6, fy-6, fx+3, fy+3, 2, col)
			b.RectOutline(fx-2, fy-2, fx+7, fy+7, 2, col)
		case actSave:
			b.Polyline([]float64{fx - 6, fy, fx - 2, fy + 5, fx + 7, fy - 6}, 2.6, 0x34C759)
		case actCancel:
			b.Line(fx-6, fy-6, fx+6, fy+6, 2.4, 0xFF5C57)
			b.Line(fx+6, fy-6, fx-6, fy+6, 2.4, 0xFF5C57)
		}
	}
}

func (tb *Toolbar) Show() {
	if tb.hwnd == 0 {
		return
	}
	tb.place()
	tb.hwnd.Show(win.SW_SHOWNOACTIVATE)
	tb.hwnd.SetPos(win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	tb.compose()
	tb.hwnd.Invalidate(false)
}

func (tb *Toolbar) place() {
	r := tb.ed.rect
	scr := win.VirtualScreen()
	x := r.Left + (r.W()-tb.w)/2
	y := r.Bottom + 10
	if y+tb.h > scr.Bottom-2 {
		y = r.Top - tb.h - 10
	}
	if y < scr.Top+2 {
		y = r.Bottom - tb.h - 12
		if y < scr.Top+2 {
			y = scr.Top + 2
		}
	}
	if x+tb.w > scr.Right-2 {
		x = scr.Right - tb.w - 2
	}
	if x < scr.Left+2 {
		x = scr.Left + 2
	}
	tb.hwnd.SetPos(win.HWND_TOPMOST, x, y, tb.w, tb.h, win.SWP_NOACTIVATE)
}

func (tb *Toolbar) Close() {
	if tb.hwnd != 0 {
		h := tb.hwnd
		tb.hwnd = 0
		h.Destroy()
	}
	if tb.buf != nil {
		tb.buf.Close()
		tb.buf = nil
	}
	if tb.font != 0 {
		win.DeleteObject(win.HGDIOBJ(tb.font))
	}
	if tb.small != 0 {
		win.DeleteObject(win.HGDIOBJ(tb.small))
	}
}

func (tb *Toolbar) Invalidate() {
	if tb.hwnd != 0 {
		tb.compose()
		tb.hwnd.Invalidate(false)
	}
}

func (tb *Toolbar) flashMsg(s string) {
	tb.msg = s
	tb.Invalidate()
}

func (tb *Toolbar) hit(p win.POINT) int {
	for i, b := range tb.btns {
		if p.X >= b.rect.Left && p.X < b.rect.Right && p.Y >= b.rect.Top && p.Y < b.rect.Bottom {
			return i
		}
	}
	return -1
}

func (tb *Toolbar) wndProc(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case win.WM_PAINT:
		dc, done := hwnd.Paint()
		win.BlitCopy(dc, tb.buf, tb.w, tb.h)
		done()
		return 0
	case win.WM_ERASEBKGND:
		return 1
	case win.WM_MOUSEACTIVATE:
		return 3 // MA_NOACTIVATE: keep keyboard focus on the editor
	case win.WM_SETCURSOR:
		win.SetCursor(win.LoadCursor(0, win.IDC_ARROW))
		return 1
	case win.WM_MOUSEMOVE:
		p := win.GetCursorPos()
		cp := win.POINT{p.X - tb.rectLeft(), p.Y - tb.rectTop()}
		h := tb.hit(cp)
		if h != tb.hover {
			tb.hover = h
			tb.Invalidate()
		}
		return 0
	case win.WM_LBUTTONDOWN:
		x, y := win.LParamXY(lp)
		i := tb.hit(win.POINT{X: x, Y: y})
		if i >= 0 {
			tb.click(tb.btns[i])
		}
		return 0
	case win.WM_LBUTTONUP:
		return 0
	case win.WM_KEYDOWN:
		// forward to the editor so shortcuts keep working while hovering the bar
		if tb.ed.hwnd != 0 {
			tb.ed.onKey(uint32(wp))
		}
		return 0
	}
	return win.DefWindowProc(hwnd, msg, wp, lp)
}

func (tb *Toolbar) rectLeft() int32 {
	r := tb.hwnd.Rect()
	return r.Left
}

func (tb *Toolbar) rectTop() int32 {
	r := tb.hwnd.Rect()
	return r.Top
}

func (tb *Toolbar) click(b tbButton) {
	ed := tb.ed
	switch b.kind {
	case tbTool:
		ed.setTool(b.tool)
	case tbWidth:
		ed.setWidth(b.width)
	case tbColor:
		ed.setColor(b.color)
	case tbAction:
		switch b.act {
		case actUndo:
			ed.undo()
		case actCopy:
			ed.copy()
		case actSave:
			ed.save()
		case actCancel:
			ed.cancel()
		}
	}
	tb.Invalidate()
}
