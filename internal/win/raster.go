//go:build windows

package win

import (
	"math"
)

// Software rasterizer with analytic anti-aliasing, writing straight into a DIB.
// Everything the editor draws goes through these helpers: no GDI+ floats, no
// external image packages, no allocations per frame.

func colorRGB(c uint32) (float64, float64, float64) {
	return float64((c >> 16) & 0xFF), float64((c >> 8) & 0xFF), float64(c & 0xFF)
}

func (d *Dib) blend(x, y int32, r, g, b, a float64) {
	if a <= 0 {
		return
	}
	if a > 1 {
		a = 1
	}
	i := d.Index(x, y)
	dr := float64(d.Bits[i])
	dg := float64(d.Bits[i+1])
	db := float64(d.Bits[i+2])
	d.Bits[i] = byte(dr + (b-dr)*a + 0.5)
	d.Bits[i+1] = byte(dg + (g-dg)*a + 0.5)
	d.Bits[i+2] = byte(db + (r-db)*a + 0.5)
	d.Bits[i+3] = 255
}

type frect struct{ x0, y0, x1, y1 int32 }

func (d *Dib) bboxPad(minx, miny, maxx, maxy, pad float64) frect {
	r := frect{
		x0: int32(math.Floor(minx - pad)),
		y0: int32(math.Floor(miny - pad)),
		x1: int32(math.Ceil(maxx + pad)),
		y1: int32(math.Ceil(maxy + pad)),
	}
	if r.x0 < 0 {
		r.x0 = 0
	}
	if r.y0 < 0 {
		r.y0 = 0
	}
	if r.x1 > d.W-1 {
		r.x1 = d.W - 1
	}
	if r.y1 > d.H-1 {
		r.y1 = d.H - 1
	}
	return r
}

// Line draws a capsule (segment with round caps) of the given width.
func (d *Dib) Line(x1, y1, x2, y2, width float64, color uint32) {
	if width < 1 {
		width = 1
	}
	rad := width / 2
	half := rad + 0.5
	r, g, b := colorRGB(color)
	bb := d.bboxPad(math.Min(x1, x2), math.Min(y1, y2), math.Max(x1, x2), math.Max(y1, y2), half+1)
	dx := x2 - x1
	dy := y2 - y1
	len2 := dx*dx + dy*dy
	for y := bb.y0; y <= bb.y1; y++ {
		fy := float64(y) + 0.5
		for x := bb.x0; x <= bb.x1; x++ {
			fx := float64(x) + 0.5
			var dist float64
			if len2 < 1e-9 {
				dist = math.Hypot(fx-x1, fy-y1)
			} else {
				t := ((fx-x1)*dx + (fy-y1)*dy) / len2
				if t < 0 {
					t = 0
				} else if t > 1 {
					t = 1
				}
				dist = math.Hypot(fx-(x1+t*dx), fy-(y1+t*dy))
			}
			cov := half - dist
			if cov > 0 {
				d.blend(x, y, r, g, b, cov)
			}
		}
	}
}

// Polyline draws a connected stroke through the given points.
func (d *Dib) Polyline(pts []float64, width float64, color uint32) {
	if len(pts) < 4 {
		if len(pts) == 2 {
			d.Line(pts[0], pts[1], pts[0], pts[1], width, color)
		}
		return
	}
	for i := 0; i+3 < len(pts); i += 2 {
		d.Line(pts[i], pts[i+1], pts[i+2], pts[i+3], width, color)
	}
	// round the joins so dense polylines look continuous
	rad := width / 2
	dots := rad * rad
	if dots > 4 {
		for i := 2; i+1 < len(pts); i += 2 {
			d.FillCircle(pts[i], pts[i+1], rad, color)
		}
	}
}

// RectOutline draws an anti-aliased rectangle border.
func (d *Dib) RectOutline(x0, y0, x1, y1, width float64, color uint32) {
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	if width < 1 {
		width = 1
	}
	half := width / 2
	band := half + 0.5
	cx := (x0 + x1) / 2
	cy := (y0 + y1) / 2
	hw := (x1 - x0) / 2
	hh := (y1 - y0) / 2
	r, g, b := colorRGB(color)
	bb := d.bboxPad(x0, y0, x1, y1, band+1)
	for y := bb.y0; y <= bb.y1; y++ {
		fy := float64(y) + 0.5
		qy := math.Abs(fy-cy) - hh
		for x := bb.x0; x <= bb.x1; x++ {
			fx := float64(x) + 0.5
			qx := math.Abs(fx-cx) - hw
			// signed distance to the rectangle border
			var dist float64
			if qx > 0 || qy > 0 {
				dist = math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
			} else {
				dist = math.Min(math.Max(qx, qy), 0)
			}
			dist = math.Abs(dist)
			cov := band - dist
			if cov > 0 {
				d.blend(x, y, r, g, b, cov)
			}
		}
	}
}

// EllipseOutline draws an anti-aliased ellipse border using a first order
// distance estimate from the implicit ellipse equation.
func (d *Dib) EllipseOutline(cx, cy, rx, ry, width float64, color uint32) {
	if rx < 0.5 || ry < 0.5 {
		return
	}
	if width < 1 {
		width = 1
	}
	band := width/2 + 0.5
	r, g, b := colorRGB(color)
	a2 := rx * rx
	b2 := ry * ry
	bb := d.bboxPad(cx-rx, cy-ry, cx+rx, cy+ry, band+1)
	for y := bb.y0; y <= bb.y1; y++ {
		fy := float64(y) + 0.5 - cy
		y2 := fy * fy
		for x := bb.x0; x <= bb.x1; x++ {
			fx := float64(x) + 0.5 - cx
			x2 := fx * fx
			f := x2/a2 + y2/b2 - 1
			// gradient magnitude of the implicit function
			gx := 2 * fx / a2
			gy := 2 * fy / b2
			gm := math.Hypot(gx, gy)
			if gm < 1e-9 {
				continue
			}
			dist := math.Abs(f) / gm
			cov := band - dist
			if cov > 0 {
				d.blend(x, y, r, g, b, cov)
			}
		}
	}
}

// FillCircle fills a disc with anti-aliased edge.
func (d *Dib) FillCircle(cx, cy, rad float64, color uint32) {
	if rad <= 0 {
		return
	}
	r, g, b := colorRGB(color)
	bb := d.bboxPad(cx-rad, cy-rad, cx+rad, cy+rad, 1)
	for y := bb.y0; y <= bb.y1; y++ {
		fy := float64(y) + 0.5 - cy
		for x := bb.x0; x <= bb.x1; x++ {
			fx := float64(x) + 0.5 - cx
			cov := rad + 0.5 - math.Hypot(fx, fy)
			if cov > 0 {
				d.blend(x, y, r, g, b, cov)
			}
		}
	}
}

// FillTriangle fills a triangle with anti-aliased edges (used for arrow heads).
func (d *Dib) FillTriangle(ax, ay, bx, by, cx, cy float64, color uint32) {
	area := (bx-ax)*(cy-ay) - (by-ay)*(cx-ax)
	if math.Abs(area) < 1e-9 {
		return
	}
	orient := 1.0
	if area < 0 {
		orient = -1
	}
	type edge struct{ dx, dy, px, py float64 }
	edges := []edge{
		{bx - ax, by - ay, ax, ay},
		{cx - bx, cy - by, bx, by},
		{ax - cx, ay - cy, cx, cy},
	}
	for i := range edges {
		l := math.Hypot(edges[i].dx, edges[i].dy)
		if l < 1e-9 {
			return
		}
		edges[i].dx /= l
		edges[i].dy /= l
	}
	r, g, b := colorRGB(color)
	bb := d.bboxPad(math.Min(ax, math.Min(bx, cx)), math.Min(ay, math.Min(by, cy)),
		math.Max(ax, math.Max(bx, cx)), math.Max(ay, math.Max(by, cy)), 1.5)
	for y := bb.y0; y <= bb.y1; y++ {
		fy := float64(y) + 0.5
		for x := bb.x0; x <= bb.x1; x++ {
			fx := float64(x) + 0.5
			inside := math.Inf(1)
			for _, e := range edges {
				dist := orient * (e.dx*(fy-e.py) - e.dy*(fx-e.px))
				if dist < inside {
					inside = dist
				}
			}
			cov := inside + 0.5
			if cov > 0 {
				d.blend(x, y, r, g, b, cov)
			}
		}
	}
}

// FillRectColor fills an axis aligned rectangle with no anti-aliasing.
func (d *Dib) FillRectColor(r RECT, color uint32) {
	r = d.Clip(r)
	rgb := color & 0x00FFFFFF
	for y := r.Top; y < r.Bottom; y++ {
		i := d.Index(r.Left, y)
		for x := r.Left; x < r.Right; x++ {
			*(*uint32)(ptrAt(&d.Bits, i)) = rgb | 0xFF000000
			i += 4
		}
	}
}

// FillRectBlend blends a solid color over a rectangle with the given alpha.
func (d *Dib) FillRectBlend(r RECT, color uint32, alpha float64) {
	if alpha <= 0 {
		return
	}
	if alpha > 1 {
		alpha = 1
	}
	r = d.Clip(r)
	sr, sg, sb := colorRGB(color)
	for y := r.Top; y < r.Bottom; y++ {
		i := d.Index(r.Left, y)
		for x := r.Left; x < r.Right; x++ {
			dr := float64(d.Bits[i])
			dg := float64(d.Bits[i+1])
			db := float64(d.Bits[i+2])
			d.Bits[i] = byte(db + (sb-db)*alpha + 0.5)
			d.Bits[i+1] = byte(dg + (sg-dg)*alpha + 0.5)
			d.Bits[i+2] = byte(dr + (sr-dr)*alpha + 0.5)
			d.Bits[i+3] = 255
			i += 4
		}
	}
}
