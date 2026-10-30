// Command genicon renders build/app.ico (and a preview PNG) without any
// external image tooling: a tiny supersampling rasterizer plus a PNG-in-ICO
// writer. Run with `go generate ./...` or `go run ./tools/genicon`.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

type rgb struct{ r, g, b float64 }

func hex(v uint32) rgb {
	return rgb{
		r: float64((v >> 16) & 0xFF),
		g: float64((v >> 8) & 0xFF),
		b: float64(v & 0xFF),
	}
}

func (c rgb) mix(o rgb, a float64) rgb {
	return rgb{c.r + (o.r-c.r)*a, c.g + (o.g-c.g)*a, c.b + (o.b-c.b)*a}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func distSeg(px, py, x1, y1, x2, y2 float64) float64 {
	dx, dy := x2-x1, y2-y1
	l2 := dx*dx + dy*dy
	t := 0.0
	if l2 > 1e-12 {
		t = ((px-x1)*dx + (py-y1)*dy) / l2
		t = clamp01(t)
	}
	return math.Hypot(px-(x1+t*dx), py-(y1+t*dy))
}

func inTri(px, py, ax, ay, bx, by, cx, cy float64) bool {
	d1 := (px-bx)*(ay-by) - (ax-bx)*(py-by)
	d2 := (px-cx)*(by-cy) - (bx-cx)*(py-cy)
	d3 := (px-ax)*(cy-ay) - (cx-ax)*(py-ay)
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

func sdfRect(px, py, x0, y0, x1, y1 float64) float64 {
	cx, cy := (x0+x1)/2, (y0+y1)/2
	hx, hy := (x1-x0)/2, (y1-y0)/2
	qx := math.Abs(px-cx) - hx
	qy := math.Abs(py-cy) - hy
	if qx > 0 || qy > 0 {
		return math.Hypot(math.Max(qx, 0), math.Max(qy, 0))
	}
	return math.Max(qx, qy)
}

type shape struct {
	color rgb
	// returns true when (x,y) in 0..1 space is inside the shape
	test func(x, y float64) bool
}

func shapes() []shape {
	bg := hex(0x1E293B)
	ring := hex(0x3B4A63)
	frame := hex(0x60A5FA)
	arrow := hex(0xFF3B30)

	// arrow geometry
	const sx, sy = 0.33, 0.65
	const tx, ty = 0.74, 0.30
	dx, dy := tx-sx, ty-sy
	l := math.Hypot(dx, dy)
	ux, uy := dx/l, dy/l
	head := 0.22
	half := 0.095
	px, py := -uy, ux
	bx, by := tx-ux*head*0.92, ty-uy*head*0.92

	return []shape{
		{bg, func(x, y float64) bool { return math.Hypot(x-0.5, y-0.5) <= 0.48 }},
		{ring, func(x, y float64) bool {
			d := math.Hypot(x-0.5, y-0.5)
			return math.Abs(d-0.44) <= 0.016
		}},
		{frame, func(x, y float64) bool {
			return math.Abs(sdfRect(x, y, 0.235, 0.30, 0.765, 0.675)) <= 0.03
		}},
		{arrow, func(x, y float64) bool { return distSeg(x, y, sx, sy, bx, by) <= 0.055 }},
		{arrow, func(x, y float64) bool {
			return inTri(x, y,
				tx, ty,
				tx-ux*head+px*half, ty-uy*head+py*half,
				tx-ux*head-px*half, ty-uy*head-py*half)
		}},
	}
}

func render(size int) *image.NRGBA {
	const ss = 4
	total := float64(ss * ss)
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	sh := shapes()
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var sr, sg, sb float64
			cov := 0.0
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/ss) / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/ss) / float64(size)
					var sample rgb
					covered := false
					for _, s := range sh {
						if s.test(x, y) {
							sample = s.color
							covered = true
						}
					}
					if covered {
						sr += sample.r
						sg += sample.g
						sb += sample.b
						cov++
					}
				}
			}
			if cov == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{
				R: uint8(sr/cov + 0.5),
				G: uint8(sg/cov + 0.5),
				B: uint8(sb/cov + 0.5),
				A: uint8(cov/total*255 + 0.5),
			})
		}
	}
	return img
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func writeICO(path string, sizes []int) error {
	imgs := make([][]byte, len(sizes))
	for i, s := range sizes {
		imgs[i] = encodePNG(render(s))
	}
	var out bytes.Buffer
	putU16(&out, 0) // reserved
	putU16(&out, 1) // type: icon
	putU16(&out, uint16(len(sizes)))
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		w := byte(s)
		if s >= 256 {
			w = 0
		}
		out.Write([]byte{w, w, 0, 0})
		putU16(&out, 1)  // planes
		putU16(&out, 32) // bpp
		putU32(&out, uint32(len(imgs[i])))
		putU32(&out, uint32(offset))
		offset += len(imgs[i])
	}
	for _, data := range imgs {
		out.Write(data)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

func putU16(b *bytes.Buffer, v uint16) {
	var t [2]byte
	binary.LittleEndian.PutUint16(t[:], v)
	b.Write(t[:])
}

func putU32(b *bytes.Buffer, v uint32) {
	var t [4]byte
	binary.LittleEndian.PutUint32(t[:], v)
	b.Write(t[:])
}

func main() {
	out := flag.String("out", "build/app.ico", "output .ico path")
	preview := flag.String("preview", "build/icon-256.png", "output preview png")
	flag.Parse()
	if err := writeICO(*out, []int{16, 24, 32, 48, 64, 128, 256}); err != nil {
		fmt.Fprintln(os.Stderr, "genicon:", err)
		os.Exit(1)
	}
	if *preview != "" {
		_ = os.MkdirAll(filepath.Dir(*preview), 0o755)
		if err := os.WriteFile(*preview, encodePNG(render(256)), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "genicon:", err)
			os.Exit(1)
		}
	}
	fmt.Println("genicon: wrote", *out)
}
