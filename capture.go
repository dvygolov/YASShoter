//go:build windows

package main

import (
	"github.com/dvygolov/yasshoter/internal/win"
)

// startCapture freezes the screen and opens the selection overlay. This is the
// hot path: capture + dim + show must stay in the tens of milliseconds.
func (a *App) startCapture(mode overlayMode) {
	if a.active || a.rec != nil {
		// a screenshot while recording would freeze the screen into the video
		return
	}
	start := win.GetTickCount()
	defer func() {
		logf("capture pipeline %d ms", win.GetTickCount()-start)
	}()
	a.active = true
	screen := win.VirtualScreen()
	a.screen = screen

	src, err := win.NewDib(screen.W(), screen.H())
	if err != nil {
		a.active = false
		return
	}
	sdc, release := win.ScreenDC()
	src.BlitRaw(sdc, screen.Left, screen.Top, screen.W(), screen.H())
	release()
	src.SetOpaqueAlpha()
	a.src = src

	frame, err := win.NewDib(screen.W(), screen.H())
	if err != nil {
		src.Close()
		a.src = nil
		a.active = false
		return
	}
	a.frame = frame

	if a.overlay == nil {
		a.overlay = newOverlay(a)
	}
	audioMode := a.cfg.AudioSource
	if !a.cfg.VideoAudio {
		audioMode = ""
	}
	a.overlay.show(mode, audioMode)
	logf("overlay shown screen=%d,%d %dx%d windows=%d", screen.Left, screen.Top, screen.W(), screen.H(), len(a.overlay.windows))
}

// afterCapture releases every heavy buffer: the resident footprint stays small.
func (a *App) afterCapture() {
	if a.overlay != nil {
		a.overlay.hide()
	}
	if a.src != nil {
		a.src.Close()
		a.src = nil
	}
	if a.frame != nil {
		a.frame.Close()
		a.frame = nil
	}
	a.active = false
	win.TrimWorkingSet()
	if a.pendingUpdate {
		a.pendingUpdate = false
		if err := a.applyUpdate(); err != nil {
			a.notify("Обновление", err.Error())
		}
	}
}
