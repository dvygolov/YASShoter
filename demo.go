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
	// detailed content so blur/pixelate are clearly visible
	for y := int32(60); y < 260; y += 12 {
		for x := int32(60); x < 520; x += 12 {
			if ((x/12)+(y/12))%2 == 0 {
				src.FillRectColor(win.RECT{x, y, x + 12, y + 12}, 0x2F6FEB)
			}
		}
	}
	for i := int32(0); i < 16; i++ {
		src.Line(float64(560+i*22), 60, float64(560), float64(60+i*14), 3, 0xE5484D)
	}
	src.TextOut("SENSITIVE DATA 1234-5678", &win.RECT{600, 300, 940, 340}, 0x111827,
		win.CreateFont(26, win.FW_BOLD, "Segoe UI"), win.DT_LEFT|win.DT_VCENTER|win.DT_SINGLELINE)
	a.src = src
	region := win.RECT{80, 80, 80 + 900, 80 + 480}
	logf("demo: opening editor for %dx%d at dpi=%d", region.W(), region.H(),
		win.DpiForPoint(win.POINT{X: region.Left, Y: region.Top}))
	a.openEditor(region)
}
