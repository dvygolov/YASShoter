//go:build windows

package main

import (
	"math"

	"github.com/dvygolov/yasshoter/internal/win"
)

type ToolKind int

const (
	ToolArrow ToolKind = iota
	ToolRect
	ToolEllipse
	ToolPencil
	ToolNumber
)

// Shape is a single annotation. Coordinates are in image pixels.
type Shape struct {
	Tool  ToolKind
	Color uint32
	Width float64
	X0    float64
	Y0    float64
	X1    float64
	Y1    float64
	Pts   []float64
	Num   int
}

func (s *Shape) Bounds() win.RECT {
	pad := s.Width + 16
	var minx, miny, maxx, maxy float64
	if s.Tool == ToolPencil && len(s.Pts) >= 2 {
		minx, maxx = s.Pts[0], s.Pts[0]
		miny, maxy = s.Pts[1], s.Pts[1]
		for i := 2; i+1 < len(s.Pts); i += 2 {
			minx = math.Min(minx, s.Pts[i])
			maxx = math.Max(maxx, s.Pts[i])
			miny = math.Min(miny, s.Pts[i+1])
			maxy = math.Max(maxy, s.Pts[i+1])
		}
	} else {
		minx = math.Min(s.X0, s.X1)
		maxx = math.Max(s.X0, s.X1)
		miny = math.Min(s.Y0, s.Y1)
		maxy = math.Max(s.Y0, s.Y1)
	}
	return win.RECT{
		Left:   int32(math.Floor(minx - pad)),
		Top:    int32(math.Floor(miny - pad)),
		Right:  int32(math.Ceil(maxx + pad)),
		Bottom: int32(math.Ceil(maxy + pad)),
	}
}

func drawShape(d *win.Dib, s *Shape, font win.HFONT) {
	switch s.Tool {
	case ToolArrow:
		drawArrow(d, s)
	case ToolRect:
		d.RectOutline(s.X0, s.Y0, s.X1, s.Y1, s.Width, s.Color)
	case ToolEllipse:
		rx := math.Abs(s.X1-s.X0) / 2
		ry := math.Abs(s.Y1-s.Y0) / 2
		cx := (s.X0 + s.X1) / 2
		cy := (s.Y0 + s.Y1) / 2
		d.EllipseOutline(cx, cy, rx, ry, s.Width, s.Color)
	case ToolPencil:
		d.Polyline(s.Pts, s.Width, s.Color)
	case ToolNumber:
		rad := 10 + s.Width*1.2
		d.FillCircle(s.X0, s.Y0, rad, s.Color)
		txt := itoa(int64(s.Num))
		box := win.RECT{
			Left:   int32(s.X0 - rad),
			Top:    int32(s.Y0 - rad),
			Right:  int32(s.X0 + rad),
			Bottom: int32(s.Y0 + rad),
		}
		d.TextOut(txt, &box, 0xFFFFFF, font, win.DT_CENTER|win.DT_VCENTER|win.DT_SINGLELINE|win.DT_NOPREFIX)
	}
}

func drawArrow(d *win.Dib, s *Shape) {
	dx := s.X1 - s.X0
	dy := s.Y1 - s.Y0
	l := math.Hypot(dx, dy)
	if l < 1 {
		return
	}
	ux := dx / l
	uy := dy / l
	head := 4.2 * s.Width
	if head < 8 {
		head = 8
	}
	if head > l*0.5 {
		head = l * 0.5
	}
	// shaft stops where the head starts so the joint stays clean
	bx := s.X1 - ux*head*0.9
	by := s.Y1 - uy*head*0.9
	d.Line(s.X0, s.Y0, bx, by, s.Width, s.Color)

	// arrow head: filled triangle
	half := head * 0.55
	px := -uy
	py := ux
	tipX := s.X1
	tipY := s.Y1
	d.FillTriangle(
		tipX, tipY,
		tipX-ux*head+px*half, tipY-uy*head+py*half,
		tipX-ux*head-px*half, tipY-uy*head-py*half,
		s.Color,
	)
}
