//go:build windows

package main

import (
	"os"

	"github.com/dvygolov/yasshoter/internal/win"
)

// Dev-only: YAS_DEV_DEMO_EDITOR=1 opens the annotation editor on a synthetic
// image so the toolbar can be inspected at any DPI without touching the desktop.
func demoEditorEnabled() bool {
	return os.Getenv("YAS_DEV_DEMO_EDITOR") != ""
}

func (a *App) openDemoEditor() {
	a.screen = win.VirtualScreen()
	src, err := win.NewDib(a.screen.W(), a.screen.H())
	if err != nil {
		logf("demo: %v", err)
		return
	}
	src.FillRectColor(win.RECT{0, 0, a.screen.W(), a.screen.H()}, 0xF2F4F8)
	grid := uint32(0xE3E8EF)
	for x := int32(0); x < a.screen.W(); x += 40 {
		src.Line(float64(x), 0, float64(x), float64(a.screen.H()), 1, grid)
	}
	for y := int32(0); y < a.screen.H(); y += 40 {
		src.Line(0, float64(y), float64(a.screen.W()), float64(y), 1, grid)
	}
	src.FillRectColor(win.RECT{60, 60, 520, 260}, 0xDCE7FF)
	src.FillRectColor(win.RECT{560, 60, 900, 160}, 0xFFE9DC)
	a.src = src
	region := win.RECT{80, 80, 80 + 900, 80 + 480}
	logf("demo: opening editor for %dx%d at dpi=%d", region.W(), region.H(),
		win.DpiForPoint(win.POINT{X: region.Left, Y: region.Top}))
	a.openEditor(region)
}
