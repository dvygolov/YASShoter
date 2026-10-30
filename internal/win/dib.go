//go:build windows

package win

import (
	"errors"
	"unsafe"
)

// Dib is a top-down 32bit BGRA DIB section together with a memory DC, so it can
// be used both for pixel work and for GDI/GDI+ drawing.
type Dib struct {
	W, H int32
	DC   HDC
	Bmp  HBITMAP
	Bits []byte // len = W*H*4, BGRA, premultiplied when used as layered frame

	old HGDIOBJ
}

// NewDibBottomUp creates a classic bottom-up DIB section (used to test
// UpdateLayeredWindow compatibility).
func NewDibBottomUp(w, h int32) (*Dib, error) {
	dc, _, _ := pCreateCompatibleDC.Call(0)
	bi := BITMAPINFO{BmiHeader: BITMAPINFOHEADER{
		BiSize:        uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
		BiWidth:       w,
		BiHeight:      h,
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: BI_RGB,
	}}
	var bits unsafe.Pointer
	bmp, _, _ := pCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&bi)), DIB_RGB_COLORS, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return nil, errors.New("CreateDIBSection failed")
	}
	old, _, _ := pSelectObject.Call(dc, bmp)
	d := &Dib{W: w, H: h, DC: HDC(dc), Bmp: HBITMAP(bmp), old: HGDIOBJ(old)}
	d.Bits = unsafe.Slice((*byte)(bits), int(w)*int(h)*4)
	for i := 3; i < len(d.Bits); i += 4 {
		d.Bits[i] = 255
	}
	return d, nil
}

func NewDib(w, h int32) (*Dib, error) {
	if w <= 0 || h <= 0 {
		return nil, errors.New("win: invalid DIB size")
	}
	dc, _, _ := pCreateCompatibleDC.Call(0)
	if dc == 0 {
		return nil, errors.New("win: CreateCompatibleDC failed")
	}
	bi := BITMAPINFO{BmiHeader: BITMAPINFOHEADER{
		BiSize:        uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
		BiWidth:       w,
		BiHeight:      -h, // top-down
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: BI_RGB,
	}}
	var bits unsafe.Pointer
	bmp, _, _ := pCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&bi)), DIB_RGB_COLORS, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		pDeleteDC.Call(dc)
		return nil, errors.New("win: CreateDIBSection failed")
	}
	old, _, _ := pSelectObject.Call(dc, bmp)
	d := &Dib{W: w, H: h, DC: HDC(dc), Bmp: HBITMAP(bmp), old: HGDIOBJ(old)}
	d.Bits = unsafe.Slice((*byte)(bits), int(w)*int(h)*4)
	return d, nil
}

func (d *Dib) Close() {
	if d == nil || d.DC == 0 {
		return
	}
	if d.old != 0 {
		pSelectObject.Call(uintptr(d.DC), uintptr(d.old))
	}
	pDeleteObject.Call(uintptr(d.Bmp))
	pDeleteDC.Call(uintptr(d.DC))
	d.DC = 0
	d.Bits = nil
}

// Index returns the byte offset of pixel (x,y).
func (d *Dib) Index(x, y int32) int { return (int(y)*int(d.W) + int(x)) * 4 }

// FillConstant sets every pixel to the given premultiplied BGRA value.
func (d *Dib) FillConstant(c uint32) {
	for i := 0; i < len(d.Bits); i += 4 {
		*(*uint32)(unsafe.Pointer(&d.Bits[i])) = c
	}
}

// SetOpaqueAlpha forces the alpha byte of every pixel to 255 (GDI blits leave it
// untouched/zeroed, which would make a layered window fully transparent).
func (d *Dib) SetOpaqueAlpha() {
	for i := 3; i < len(d.Bits); i += 4 {
		d.Bits[i] = 255
	}
}

// SetOpaqueAlphaRect forces alpha=255 inside r (clipped to the DIB).
func (d *Dib) SetOpaqueAlphaRect(r RECT) {
	r = d.Clip(r)
	for y := r.Top; y < r.Bottom; y++ {
		i := d.Index(r.Left, y) + 3
		for x := r.Left; x < r.Right; x++ {
			d.Bits[i] = 255
			i += 4
		}
	}
}

func (d *Dib) Clip(r RECT) RECT {
	if r.Left < 0 {
		r.Left = 0
	}
	if r.Top < 0 {
		r.Top = 0
	}
	if r.Right > d.W {
		r.Right = d.W
	}
	if r.Bottom > d.H {
		r.Bottom = d.H
	}
	if r.Right < r.Left {
		r.Right = r.Left
	}
	if r.Bottom < r.Top {
		r.Bottom = r.Top
	}
	return r
}

// Blit copies src rect (in src coords) to dx,dy inside d.
func (d *Dib) Blit(src *Dib, sx, sy, sw, sh, dx, dy int32) {
	pBitBlt.Call(uintptr(d.DC), uintptr(int(dx)), uintptr(int(dy)), uintptr(int(sw)), uintptr(int(sh)),
		uintptr(src.DC), uintptr(int(sx)), uintptr(int(sy)), SRCCOPY)
}

// CopyRect copies a rectangle within the same DIB.
func (d *Dib) CopyRect(src, dst RECT) {
	pBitBlt.Call(uintptr(d.DC), uintptr(int(dst.Left)), uintptr(int(dst.Top)),
		uintptr(int(src.W())), uintptr(int(src.H())),
		uintptr(d.DC), uintptr(int(src.Left)), uintptr(int(src.Top)), SRCCOPY)
}

// AlphaBlendFrom blends src into d at (dx,dy) with constant alpha 0..255.
func (d *Dib) AlphaBlendFrom(src *Dib, sx, sy, sw, sh, dx, dy int32, alpha byte) {
	if sw <= 0 || sh <= 0 {
		return
	}
	bf := BLENDFUNCTION{BlendOp: AC_SRC_OVER, BlendFlags: 0, SourceConstantAlpha: alpha, AlphaFormat: 0}
	pAlphaBlend.Call(uintptr(d.DC), uintptr(int(dx)), uintptr(int(dy)), uintptr(int(sw)), uintptr(int(sh)),
		uintptr(src.DC), uintptr(int(sx)), uintptr(int(sy)), uintptr(int(sw)), uintptr(int(sh)),
		uintptr(*(*uint32)(unsafe.Pointer(&bf))))
}

// Dim paints src (region r) into dst as an opaque, darkened copy: the frozen
// overlay background. Pure GDI, so a full dual monitor frame costs a few ms.
func (d *Dib) Dim(src *Dib, r RECT, alpha uint32) {
	r = d.Clip(r)
	if r.Empty() {
		return
	}
	pPatBlt.Call(uintptr(d.DC), uintptr(int(r.Left)), uintptr(int(r.Top)),
		uintptr(int(r.W())), uintptr(int(r.H())), BLACKNESS)
	a := byte(alpha)
	if a == 0 || a >= 255 {
		a = 0xF0
	}
	d.AlphaBlendFrom(src, r.Left, r.Top, r.W(), r.H(), r.Left, r.Top, a)
}

// CopyRegion blits a region of src into the same place in d.
func (d *Dib) CopyRegion(src *Dib, r RECT) {
	r = d.Clip(r)
	if r.Empty() {
		return
	}
	d.Blit(src, r.Left, r.Top, r.W(), r.H(), r.Left, r.Top)
}

// StrokeRect draws an anti-nothing 1px rectangle outline directly in pixels.
func (d *Dib) StrokeRect(r RECT, thickness int32, color uint32) {
	r = d.Clip(r)
	if r.Empty() {
		return
	}
	for t := int32(0); t < thickness; t++ {
		r0 := RECT{r.Left + t, r.Top + t, r.Right - t, r.Bottom - t}
		if r0.Empty() {
			return
		}
		for x := r0.Left; x < r0.Right; x++ {
			d.SetPixel(x, r0.Top, color)
			d.SetPixel(x, r0.Bottom-1, color)
		}
		for y := r0.Top; y < r0.Bottom; y++ {
			d.SetPixel(r0.Left, y, color)
			d.SetPixel(r0.Right-1, y, color)
		}
	}
}

func (d *Dib) SetPixel(x, y int32, color uint32) {
	if x < 0 || y < 0 || x >= d.W || y >= d.H {
		return
	}
	*(*uint32)(unsafe.Pointer(&d.Bits[d.Index(x, y)])) = color | 0xFF000000
}

// TextOut draws text with a GDI font and then repairs the alpha channel inside
// the text box so the layered window stays visible there.
func (d *Dib) TextOut(text string, r *RECT, color uint32, font HFONT, flags uint32) {
	if text == "" {
		return
	}
	prevFont, _, _ := pSelectObject.Call(uintptr(d.DC), uintptr(font))
	pSetBkMode.Call(uintptr(d.DC), TRANSPARENT)
	pSetTextColor.Call(uintptr(d.DC), uintptr(color&0x00FFFFFF))
	DrawText(d.DC, text, r, flags)
	pSelectObject.Call(uintptr(d.DC), prevFont)
	d.SetOpaqueAlphaRect(*r)
}

func CreateFont(size int32, weight int32, face string) HFONT {
	h, _, _ := pCreateFontW.Call(
		uintptr(int(-size)), 0, 0, 0, uintptr(weight),
		0, 0, 0, DEFAULT_CHARSET, 0, 0, CLEARTYPE_QUALITY, DEFAULT_PITCH|FF_DONTCARE,
		uintptr(unsafe.Pointer(UTF16Ptr(face))))
	return HFONT(h)
}

func CreateSolidBrush(c uint32) HBRUSH {
	h, _, _ := pCreateSolidBrush.Call(uintptr(c & 0x00FFFFFF))
	return HBRUSH(h)
}

func CreatePen(width int32, c uint32) HPEN {
	h, _, _ := pCreatePen.Call(0, uintptr(width), uintptr(c&0x00FFFFFF))
	return HPEN(h)
}

func DeleteObject(o HGDIOBJ) {
	if o != 0 {
		pDeleteObject.Call(uintptr(o))
	}
}

// SaveBMPHeader/ToBGRA conversion helpers for encoding live in the main package.

// Bounds helpers ------------------------------------------------------------

func Union(a, b RECT) RECT {
	if a.Empty() {
		return b
	}
	if b.Empty() {
		return a
	}
	return RECT{
		Left:   min32(a.Left, b.Left),
		Top:    min32(a.Top, b.Top),
		Right:  max32(a.Right, b.Right),
		Bottom: max32(a.Bottom, b.Bottom),
	}
}

func Inflate(r RECT, d int32) RECT {
	return RECT{r.Left - d, r.Top - d, r.Right + d, r.Bottom + d}
}

func NormalizeRect(x1, y1, x2, y2 int32) RECT {
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	return RECT{x1, y1, x2, y2}
}

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

// BlitCopy blits the whole DIB onto an arbitrary DC (window paint, etc).
func BlitCopy(dst HDC, src *Dib, w, h int32) {
	pBitBlt.Call(uintptr(dst), 0, 0, uintptr(int(w)), uintptr(int(h)),
		uintptr(src.DC), 0, 0, SRCCOPY)
}

func SelectObject(dc HDC, o HGDIOBJ) HGDIOBJ {
	r, _, _ := pSelectObject.Call(uintptr(dc), uintptr(o))
	return HGDIOBJ(r)
}

// BlitRaw copies pixels straight from a screen DC into the DIB.
func (d *Dib) BlitRaw(src HDC, x, y, w, h int32) {
	pBitBlt.Call(uintptr(d.DC), 0, 0, uintptr(int(w)), uintptr(int(h)),
		uintptr(src), uintptr(int(x)), uintptr(int(y)), SRCCOPY)
}

// BlitRectToDC blits one rectangle of the DIB onto an arbitrary DC.
func (d *Dib) BlitRectToDC(dst HDC, r RECT) {
	r = d.Clip(r)
	if r.Empty() {
		return
	}
	pBitBlt.Call(uintptr(dst), uintptr(int(r.Left)), uintptr(int(r.Top)),
		uintptr(int(r.W())), uintptr(int(r.H())),
		uintptr(d.DC), uintptr(int(r.Left)), uintptr(int(r.Top)), SRCCOPY)
}
