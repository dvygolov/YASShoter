//go:build windows

package win

// Image effects for the editor: blur and pixelation are applied to a rectangle
// of the composed buffer, so they cover whatever is underneath (screenshot plus
// earlier annotations).

// Blur applies a separable box blur (two passes look close enough to a gaussian
// for redaction) inside r. Radius is in pixels.
func (d *Dib) Blur(r RECT, radius int32) {
	r = d.Clip(r)
	w, h := int(r.W()), int(r.H())
	if w <= 0 || h <= 0 || radius < 1 {
		return
	}
	if int(radius)*2+1 > w {
		radius = int32(w / 3)
	}
	if radius < 1 {
		return
	}
	src := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		copy(src[y*w*4:(y+1)*w*4], d.Bits[d.Index(r.Left, r.Top+int32(y)):][:w*4])
	}
	tmp := make([]byte, len(src))
	boxBlurH(src, tmp, w, h, int(radius))
	boxBlurV(tmp, src, w, h, int(radius))
	for y := 0; y < h; y++ {
		copy(d.Bits[d.Index(r.Left, r.Top+int32(y)):][:w*4], src[y*w*4:(y+1)*w*4])
	}
}

// Pixelate replaces r with blocks of the given size (average colour per block).
func (d *Dib) Pixelate(r RECT, block int32) {
	r = d.Clip(r)
	w, h := int(r.W()), int(r.H())
	if w <= 0 || h <= 0 || block < 2 {
		return
	}
	if block > int32(w) {
		block = int32(w)
	}
	if block > int32(h) {
		block = int32(h)
	}
	for y := 0; y < h; y += int(block) {
		y1 := y + int(block)
		if y1 > h {
			y1 = h
		}
		for x := 0; x < w; x += int(block) {
			x1 := x + int(block)
			if x1 > w {
				x1 = w
			}
			var sb, sg, sr int
			n := 0
			for yy := y; yy < y1; yy++ {
				row := d.Index(r.Left+int32(x), r.Top+int32(yy))
				for xx := x; xx < x1; xx++ {
					sb += int(d.Bits[row])
					sg += int(d.Bits[row+1])
					sr += int(d.Bits[row+2])
					n++
					row += 4
				}
			}
			if n == 0 {
				continue
			}
			b, g, rr := byte(sb/n), byte(sg/n), byte(sr/n)
			for yy := y; yy < y1; yy++ {
				row := d.Index(r.Left+int32(x), r.Top+int32(yy))
				for xx := x; xx < x1; xx++ {
					d.Bits[row] = b
					d.Bits[row+1] = g
					d.Bits[row+2] = rr
					d.Bits[row+3] = 255
					row += 4
				}
			}
		}
	}
}

// boxBlurH is a horizontal box blur with a sliding window (O(w*h)).
func boxBlurH(src, dst []byte, w, h, rad int) {
	win := 2*rad + 1
	for y := 0; y < h; y++ {
		base := y * w * 4
		var sb, sg, sr int
		// prime the window: clamp at the edges
		for i := -rad; i <= rad; i++ {
			x := i
			if x < 0 {
				x = 0
			} else if x >= w {
				x = w - 1
			}
			p := base + x*4
			sb += int(src[p])
			sg += int(src[p+1])
			sr += int(src[p+2])
		}
		for x := 0; x < w; x++ {
			p := base + x*4
			dst[p] = byte(sb / win)
			dst[p+1] = byte(sg / win)
			dst[p+2] = byte(sr / win)
			dst[p+3] = 255
			// slide
			out := x - rad
			if out < 0 {
				out = 0
			}
			in := x + rad + 1
			if in >= w {
				in = w - 1
			}
			po := base + out*4
			pi := base + in*4
			sb += int(src[pi]) - int(src[po])
			sg += int(src[pi+1]) - int(src[po+1])
			sr += int(src[pi+2]) - int(src[po+2])
		}
	}
}

// boxBlurV is the vertical counterpart.
func boxBlurV(src, dst []byte, w, h, rad int) {
	win := 2*rad + 1
	for x := 0; x < w; x++ {
		var sb, sg, sr int
		for i := -rad; i <= rad; i++ {
			y := i
			if y < 0 {
				y = 0
			} else if y >= h {
				y = h - 1
			}
			p := (y*w + x) * 4
			sb += int(src[p])
			sg += int(src[p+1])
			sr += int(src[p+2])
		}
		for y := 0; y < h; y++ {
			p := (y*w + x) * 4
			dst[p] = byte(sb / win)
			dst[p+1] = byte(sg / win)
			dst[p+2] = byte(sr / win)
			dst[p+3] = 255
			out := y - rad
			if out < 0 {
				out = 0
			}
			in := y + rad + 1
			if in >= h {
				in = h - 1
			}
			po := (out*w + x) * 4
			pi := (in*w + x) * 4
			sb += int(src[pi]) - int(src[po])
			sg += int(src[pi+1]) - int(src[po+1])
			sr += int(src[pi+2]) - int(src[po+2])
		}
	}
}
