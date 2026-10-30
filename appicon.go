//go:build windows

package main

import "github.com/dvygolov/yasshoter/internal/win"

// makeAppIcon returns the tray icon: the icon compiled into the exe resource
// when available (crisp at every DPI), otherwise one drawn at runtime.
func makeAppIcon() win.HICON {
	if exe := win.ExePath(); exe != "" {
		if h := win.IconFromExe(exe, true); h != 0 {
			logf("icon: loaded from exe resource (%d)", uintptr(h))
			return h
		}
		logf("icon: exe resource icon unavailable, drawing one")
	}
	return drawAppIcon()
}

// drawAppIcon renders the tray icon at runtime: dark disc, blue selection frame,
// red arrow. No image assets needed.
func drawAppIcon() win.HICON {
	const s = 32
	d, err := win.NewDib(s, s)
	if err != nil {
		return 0
	}
	defer d.Close()
	d.FillConstant(0)
	cx, cy := float64(s)/2, float64(s)/2

	d.FillCircle(cx, cy, 15, 0x1E293B)
	d.FillCircle(cx, cy, 15, 0x1E293B)
	// inner highlight ring
	d.EllipseOutline(cx, cy, 15, 15, 1, 0x334155)
	d.RectOutline(7.5, 9.5, 24.5, 21.5, 1.8, 0x60A5FA)
	drawArrow(d, &Shape{
		Tool:  ToolArrow,
		Color: 0xFF3B30,
		Width: 2.8,
		X0:    11, Y0: 20,
		X1: 23, Y1: 11,
	})
	icon := win.IconFromDib(d)
	return icon
}
