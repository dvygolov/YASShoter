//go:build windows

package main

import (
	"unsafe"

	"github.com/dvygolov/yasshoter/internal/win"
)

// Editor is the annotation window that appears exactly on top of the captured
// region: no extra chrome, 1:1 pixels, instant.
type Editor struct {
	app  *App
	hwnd win.HWND
	rect win.RECT

	img *win.Dib // cropped screenshot
	buf *win.Dib // double buffer, same size

	w, h int32

	shapes  []Shape
	cur     *Shape
	drawing bool
	moved   bool

	tool         ToolKind
	color        uint32
	widthLogical float64 // 96 DPI units, shown in the toolbar
	nextNum      int
	dpi          uint32
	s            float64
	font         win.HFONT

	tb *Toolbar
}

func (a *App) openEditor(region win.RECT) {
	ed := &Editor{
		app:     a,
		tool:    ToolArrow,
		color:   parseColor(a.cfg.Color, 0xFF3B30),
		nextNum: 1,
	}
	w := region.W()
	h := region.H()

	// The capture is in physical pixels; the toolbar and the pen follow the
	// monitor scale so nothing looks tiny on a 150% laptop display.
	ed.dpi = win.DpiForPoint(win.POINT{X: region.Left + w/2, Y: region.Top + h/2})
	ed.s = win.ScaleFactor(ed.dpi)
	ed.widthLogical = a.cfg.Thickness
	if ed.widthLogical < 1 {
		ed.widthLogical = 4
	}

	img, err := win.NewDib(w, h)
	if err != nil {
		return
	}
	buf, err := win.NewDib(w, h)
	if err != nil {
		img.Close()
		return
	}
	// crop from the pristine screenshot: the overlay frame carries the dim and
	// the size label, which must never end up in the saved image
	img.Blit(a.src, region.Left-a.screen.Left, region.Top-a.screen.Top, w, h, 0, 0)

	logf("editor: dpi=%d scale=%.2f region=%dx%d thickness=%.1f", ed.dpi, ed.s, w, h, ed.widthLogical*ed.s)
	ed.img = img
	ed.buf = buf
	ed.w, ed.h = w, h
	ed.font = win.CreateFont(win.Scale(16, ed.dpi), win.FW_BOLD, "Segoe UI")

	name := "YASShoterEditor"
	win.RegisterClass(name, 0)
	id := win.NewHandler(ed.wndProc)
	flag := uintptr(id)
	style := uint32(win.WS_POPUP)
	ex := uint32(win.WS_EX_TOPMOST | win.WS_EX_TOOLWINDOW)
	hwnd := win.CreateWindowEx(ex, name, appTitle, style, region.Left, region.Top, w, h, 0, 0, flag)
	if hwnd == 0 {
		ed.release()
		return
	}
	ed.hwnd = hwnd
	ed.rect = region
	a.editor = ed

	hwnd.Show(win.SW_SHOW)
	win.ForceForeground(hwnd)

	ed.tb = NewToolbar(ed)
	ed.tb.Show()
	ed.compose()
}

func (ed *Editor) release() {
	if ed.img != nil {
		ed.img.Close()
		ed.img = nil
	}
	if ed.buf != nil {
		ed.buf.Close()
		ed.buf = nil
	}
	if ed.font != 0 {
		win.DeleteObject(win.HGDIOBJ(ed.font))
		ed.font = 0
	}
}

func (ed *Editor) close() {
	if ed.tb != nil {
		ed.tb.Close()
		ed.tb = nil
	}
	if ed.hwnd != 0 {
		h := ed.hwnd
		ed.hwnd = 0
		h.Destroy()
	}
	ed.release()
	if ed.app.editor == ed {
		ed.app.editor = nil
	}
}

func (ed *Editor) cancel() {
	ed.close()
	ed.app.afterCapture()
}

// compose renders the image plus annotations into the double buffer.
func (ed *Editor) compose() {
	copy(ed.buf.Bits, ed.img.Bits)
	for i := range ed.shapes {
		drawShape(ed.buf, &ed.shapes[i], ed.font)
	}
	if ed.cur != nil {
		drawShape(ed.buf, ed.cur, ed.font)
	}
}

func (ed *Editor) paint(dc win.HDC) {
	ed.compose()
	win.BlitCopy(dc, ed.buf, ed.w, ed.h)
}

func (ed *Editor) invalidateAll() {
	if ed.hwnd != 0 {
		ed.hwnd.Invalidate(false)
	}
}

func (ed *Editor) wndProc(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case win.WM_PAINT:
		dc, done := hwnd.Paint()
		ed.paint(dc)
		done()
		return 0
	case win.WM_ERASEBKGND:
		return 1
	case win.WM_SETCURSOR:
		win.SetCursor(win.LoadCursor(0, win.IDC_CROSS))
		return 1
	case win.WM_LBUTTONDOWN:
		x, y := win.LParamXY(lp)
		ed.onDown(win.POINT{X: x, Y: y})
		return 0
	case win.WM_MOUSEMOVE:
		x, y := win.LParamXY(lp)
		ed.onMove(win.POINT{X: x, Y: y})
		return 0
	case win.WM_LBUTTONUP:
		x, y := win.LParamXY(lp)
		ed.onUp(win.POINT{X: x, Y: y})
		return 0
	case win.WM_RBUTTONUP:
		ed.cancel()
		return 0
	case win.WM_KEYDOWN:
		ed.onKey(uint32(wp))
		return 0
	case win.WM_DESTROY:
		return 0
	}
	return win.DefWindowProc(hwnd, msg, wp, lp)
}

func (ed *Editor) ctrl() bool {
	return uint16(win.GetKeyState(win.VK_CONTROL))&0x8000 != 0
}

func (ed *Editor) onKey(vk uint32) {
	switch vk {
	case win.VK_ESCAPE:
		ed.cancel()
		return
	case win.VK_RETURN:
		ed.save()
		return
	}
	if ed.ctrl() {
		switch vk {
		case win.VK_S:
			ed.save()
		case win.VK_C:
			ed.copy()
		case win.VK_Z:
			ed.undo()
		}
		return
	}
	switch vk {
	case win.VK_1:
		ed.setTool(ToolArrow)
	case win.VK_2:
		ed.setTool(ToolRect)
	case win.VK_3:
		ed.setTool(ToolEllipse)
	case win.VK_4:
		ed.setTool(ToolPencil)
	case win.VK_5:
		ed.setTool(ToolNumber)
	case win.VK_OEM_PLUS:
		ed.setWidth(nextWidth(ed.widthLogical, 1))
	case win.VK_OEM_MINUS:
		ed.setWidth(nextWidth(ed.widthLogical, -1))
	case win.VK_BACK:
		ed.undo()
	}
}

func (ed *Editor) setTool(t ToolKind) {
	ed.tool = t
	if ed.tb != nil {
		ed.tb.Invalidate()
	}
}

func (ed *Editor) setWidth(w float64) {
	ed.widthLogical = w
	ed.app.cfg.Thickness = w
	ed.app.cfg.save()
	if ed.tb != nil {
		ed.tb.Invalidate()
	}
}

func (ed *Editor) setColor(c uint32) {
	ed.color = c
	ed.app.cfg.Color = formatColor(c)
	ed.app.cfg.save()
	if ed.tb != nil {
		ed.tb.Invalidate()
	}
}

func (ed *Editor) undo() {
	if len(ed.shapes) == 0 {
		return
	}
	last := ed.shapes[len(ed.shapes)-1]
	if last.Tool == ToolNumber && ed.nextNum > 1 {
		ed.nextNum--
	}
	ed.shapes = ed.shapes[:len(ed.shapes)-1]
	ed.invalidateAll()
	if ed.tb != nil {
		ed.tb.Invalidate()
	}
}

func (ed *Editor) onDown(p win.POINT) {
	x, y := float64(p.X), float64(p.Y)
	if ed.tool == ToolNumber {
		s := Shape{Tool: ToolNumber, Color: ed.color, Width: ed.widthLogical * ed.s, X0: x, Y0: y, Num: ed.nextNum}
		ed.nextNum++
		ed.shapes = append(ed.shapes, s)
		ed.invalidateAll()
		return
	}
	ed.cur = &Shape{Tool: ed.tool, Color: ed.color, Width: ed.widthLogical * ed.s, X0: x, Y0: y, X1: x, Y1: y}
	if ed.tool == ToolPencil {
		ed.cur.Pts = append(ed.cur.Pts, x, y)
	}
	ed.drawing = true
	ed.moved = false
	win.SetCapture(ed.hwnd)
}

func (ed *Editor) onMove(p win.POINT) {
	if !ed.drawing || ed.cur == nil {
		return
	}
	x, y := float64(p.X), float64(p.Y)
	ed.cur.X1 = x
	ed.cur.Y1 = y
	if ed.tool == ToolPencil {
		n := len(ed.cur.Pts)
		if n < 2 || absf(ed.cur.Pts[n-2]-x) > 1.2 || absf(ed.cur.Pts[n-1]-y) > 1.2 {
			ed.cur.Pts = append(ed.cur.Pts, x, y)
		}
	}
	ed.moved = true
	ed.invalidateAll()
}

func (ed *Editor) onUp(p win.POINT) {
	if !ed.drawing || ed.cur == nil {
		return
	}
	win.ReleaseCapture()
	ed.drawing = false
	s := *ed.cur
	ed.cur = nil
	if !ed.moved || (absf(s.X1-s.X0) < 3 && absf(s.Y1-s.Y0) < 3 && s.Tool != ToolPencil) {
		ed.invalidateAll()
		return
	}
	ed.shapes = append(ed.shapes, s)
	ed.invalidateAll()
}

func absf(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// rasterImage returns the composed image as premultiplied BGRA bytes.
func (ed *Editor) rasterImage() []byte {
	ed.compose()
	out := make([]byte, len(ed.buf.Bits))
	copy(out, ed.buf.Bits)
	return out
}

func (ed *Editor) save() {
	bgra := ed.rasterImage()
	path, err := ed.app.saveImage(bgra, int(ed.w), int(ed.h))
	if err != nil {
		win.MessageBox("Не удалось сохранить снимок:\n"+err.Error(), appTitle, win.MB_OK|win.MB_ICONERROR|win.MB_TOPMOST)
		return
	}
	if ed.app.cfg.CopyToClipboard {
		_ = copyToClipboard(bgra, int(ed.w), int(ed.h))
	}
	ed.close()
	ed.app.afterCapture()
	logf("editor: saved %s", path)
}

func (ed *Editor) copy() {
	if err := copyToClipboard(ed.rasterImage(), int(ed.w), int(ed.h)); err == nil {
		if ed.tb != nil {
			ed.tb.flashMsg("Скопировано в буфер")
		}
	}
}

var _ = unsafe.Pointer(nil)
