//go:build windows

package main

import (
	"math"

	"github.com/dvygolov/yasshoter/internal/win"
)

// Design values are given for 96 DPI (100% scaling) and multiplied by the
// monitor scale factor, so the strip keeps its physical size on 125/150/200%.
const (
	tbDesignH     = 42
	tbDesignPad   = 8
	tbDesignBtn   = 30
	tbDesignGap   = 4
	tbDesignWBtn  = 26
	tbDesignHint  = 214
	tbDesignFont  = 14
	tbDesignSmall = 13
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
	width float64 // logical (96 DPI) thickness
	color uint32
	act   int
	rect  win.RECT
}

// Toolbar is the floating, never-activating tool strip below the capture area.
type Toolbar struct {
	ed   *Editor
	hwnd win.HWND
	buf  *win.Dib
	w, h int32

	s   float64 // dpi / 96
	dpi uint32
	btn int32
	pad int32
	gap int32

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
	s := win.ScaleFactor(ed.dpi)
	tb := &Toolbar{
		ed:    ed,
		s:     s,
		dpi:   ed.dpi,
		h:     win.Scale(tbDesignH, ed.dpi),
		btn:   win.Scale(tbDesignBtn, ed.dpi),
		pad:   win.Scale(tbDesignPad, ed.dpi),
		gap:   win.Scale(tbDesignGap, ed.dpi),
		hover: -1,
	}
	tb.font = win.CreateFont(win.Scale(tbDesignFont, ed.dpi), win.FW_SEMIBOLD, "Segoe UI")
	tb.small = win.CreateFont(win.Scale(tbDesignSmall, ed.dpi), win.FW_NORMAL, "Segoe UI")
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
	logf("toolbar: dpi=%d scale=%.2f size=%dx%d btn=%d font=%d", tb.dpi, tb.s, tb.w, tb.h, tb.btn, win.Scale(tbDesignFont, tb.dpi))
	return tb
}

func (tb *Toolbar) layout() {
	x := tb.pad
	y := (tb.h - tb.btn) / 2
	tb.seps = tb.seps[:0]
	sep := func() {
		tb.seps = append(tb.seps, x+tb.gap)
		x += tb.gap * 2
	}
	add := func(b tbButton, w int32) {
		b.rect = win.RECT{Left: x, Top: y, Right: x + w, Bottom: y + tb.btn}
		tb.btns = append(tb.btns, b)
		x += w + tb.gap
	}
	tb.btns = tb.btns[:0]
	for _, t := range []ToolKind{ToolArrow, ToolRect, ToolEllipse, ToolPencil, ToolNumber} {
		add(tbButton{kind: tbTool, tool: t}, tb.btn)
	}
	sep()
	for _, wv := range widths {
		add(tbButton{kind: tbWidth, width: wv}, win.Scale(tbDesignWBtn, tb.dpi))
	}
	sep()
	for _, c := range palette {
		add(tbButton{kind: tbColor, color: c}, win.Scale(tbDesignWBtn, tb.dpi))
	}
	sep()
	for _, a := range []int{actUndo, actCopy, actSave, actCancel} {
		add(tbButton{kind: tbAction, act: a}, tb.btn)
	}
	x += tb.gap
	tb.w = x + win.Scale(tbDesignHint, tb.dpi) + tb.pad
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
			active = ed.widthLogical == btn.width
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
	sepTop := win.Scale(9, tb.dpi)
	for _, sx := range tb.seps {
		b.FillRectColor(win.RECT{sx, sepTop, sx + 1, tb.h - sepTop}, 0x3B4250)
	}
	// hint text
	hr := win.RECT{Left: tb.w - win.Scale(tbDesignHint, tb.dpi) - tb.pad, Top: 0, Right: tb.w - tb.pad, Bottom: tb.h}
	txt := "Enter — сохранить · Esc — отмена"
	if tb.msg != "" {
		txt = tb.msg
	}
	b.TextOut(txt, &hr, 0x9AA4B2, tb.small, win.DT_RIGHT|win.DT_VCENTER|win.DT_SINGLELINE|win.DT_NOPREFIX|win.DT_END_ELLIPSIS)
}

func (tb *Toolbar) drawButtonIcon(btn tbButton, r win.RECT, active bool) {
	b := tb.buf
	s := tb.s
	var col uint32 = 0xE8EDF5
	if active {
		col = 0xFFFFFF
	}
	cx := (r.Left + r.Right) / 2
	cy := (r.Top + r.Bottom) / 2
	fx := float64(cx)
	fy := float64(cy)
	stroke := 2.2 * s
	switch btn.kind {
	case tbTool:
		switch btn.tool {
		case ToolArrow:
			sd := Shape{Tool: ToolArrow, Color: col, Width: 2.6 * s, X0: fx - 7*s, Y0: fy + 7*s, X1: fx + 7*s, Y1: fy - 7*s}
			drawArrow(b, &sd)
		case ToolRect:
			b.RectOutline(fx-7*s, fy-5.5*s, fx+7*s, fy+5.5*s, stroke, col)
		case ToolEllipse:
			b.EllipseOutline(fx, fy, 7*s, 5.5*s, stroke, col)
		case ToolPencil:
			b.Polyline([]float64{
				fx - 7*s, fy + 6*s,
				fx - 3*s, fy - 2*s,
				fx + 1*s, fy + 4*s,
				fx + 7*s, fy - 6*s,
			}, stroke, col)
		case ToolNumber:
			rad := 7 * s
			b.FillCircle(fx, fy, rad, col)
			box := win.RECT{int32(fx - rad), int32(fy - rad), int32(fx + rad), int32(fy + rad)}
			b.TextOut("1", &box, 0x20242B, tb.font, win.DT_CENTER|win.DT_VCENTER|win.DT_SINGLELINE|win.DT_NOPREFIX)
		}
	case tbWidth:
		b.FillCircle(fx, fy, btn.width*s/2, col)
	case tbColor:
		half := 8 * s
		rr := win.RECT{int32(fx - half), int32(fy - half), int32(fx + half), int32(fy + half)}
		b.FillRectColor(rr, btn.color)
		if active {
			b.StrokeRect(win.RECT{rr.Left - win.Scale(2, tb.dpi), rr.Top - win.Scale(2, tb.dpi), rr.Right + win.Scale(2, tb.dpi), rr.Bottom + win.Scale(2, tb.dpi)}, 1, 0xFFFFFF)
		} else {
			b.StrokeRect(rr, 1, 0x11151A)
		}
	case tbAction:
		switch btn.act {
		case actUndo:
			pts := make([]float64, 0, 24)
			for a := 205.0; a <= 335.0; a += 13 {
				rad := a * math.Pi / 180
				pts = append(pts, fx+5.5*s*math.Cos(rad), fy+5.5*s*math.Sin(rad))
			}
			b.Polyline(pts, 2.1*s, col)
			hx := fx + 5.5*s*math.Cos(205*math.Pi/180)
			hy := fy + 5.5*s*math.Sin(205*math.Pi/180)
			b.FillTriangle(hx-3.5*s, hy+1.5*s, hx+3.0*s, hy-3.0*s, hx+3.0*s, hy+4.0*s, col)
		case actCopy:
			b.RectOutline(fx-6*s, fy-6*s, fx+3*s, fy+3*s, 2*s, col)
			b.RectOutline(fx-2*s, fy-2*s, fx+7*s, fy+7*s, 2*s, col)
		case actSave:
			b.Polyline([]float64{fx - 6*s, fy, fx - 2*s, fy + 5*s, fx + 7*s, fy - 6*s}, 2.6*s, 0x34C759)
		case actCancel:
			b.Line(fx-6*s, fy-6*s, fx+6*s, fy+6*s, 2.4*s, 0xFF5C57)
			b.Line(fx+6*s, fy-6*s, fx-6*s, fy+6*s, 2.4*s, 0xFF5C57)
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
	y := r.Bottom + tb.gap*3
	if y+tb.h > scr.Bottom-2 {
		y = r.Top - tb.h - tb.gap*3
	}
	if y < scr.Top+2 {
		y = r.Bottom - tb.h - tb.gap*4
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
		tb.font = 0
	}
	if tb.small != 0 {
		win.DeleteObject(win.HGDIOBJ(tb.small))
		tb.small = 0
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
		r := hwnd.Rect()
		h := tb.hit(win.POINT{X: p.X - r.Left, Y: p.Y - r.Top})
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
	case win.WM_KEYDOWN:
		// forward to the editor so shortcuts keep working while hovering the bar
		if tb.ed.hwnd != 0 {
			tb.ed.onKey(uint32(wp))
		}
		return 0
	}
	return win.DefWindowProc(hwnd, msg, wp, lp)
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
