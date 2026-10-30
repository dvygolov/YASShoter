//go:build windows

package main

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/dvygolov/yasshoter/internal/win"
)

// App is the resident core: one message-only window owns the hotkey and the
// tray icon, everything else is created on demand and freed right after.
type App struct {
	cfg  *Config
	hwnd win.HWND
	tray *win.Tray
	icon win.HICON

	src    *win.Dib
	frame  *win.Dib
	screen win.RECT

	overlay *Overlay
	editor  *Editor
	active  bool

	taskbarMsg  uint32
	hotkeyID    int32
	altHotkeyID int32
	hotkeyMods  uint32
	hotkeyVK    uint32
}

const (
	cmdCapture = 1
	cmdFolder  = 2
	cmdConfig  = 3
	cmdAuto    = 4
	cmdQuit    = 5
)

func runApp() {
	win.SetProcessDPIAware()
	cfg := loadConfig()
	app := &App{cfg: cfg}

	// one instance only: two of them would fight for the same hotkey
	if h, err := win.CreateMutexAndCheck(appName + "_single_instance"); err != nil {
		_ = h
		logf("another instance is already running, exiting")
		return
	}

	if cfg.AutoStart {
		_ = ensureAutoStart(true)
	}
	if cfg.DisableSnipping {
		win.DisableSnippingToolPrintScreen()
	}
	logf("startup: saveDir=%s hotkey=%s", cfg.SaveDir, cfg.Hotkey)

	name := "YASShoterHost"
	win.RegisterClass(name, 0)
	id := win.NewHandler(app.wndProc)
	hwnd := win.CreateWindowEx(0, name, appTitle, uint32(win.WS_POPUP), -100, -100, 1, 1, 0, 0, uintptr(id))
	if hwnd == 0 {
		win.MessageBox("Не удалось создать окно приложения.", appTitle, win.MB_OK|win.MB_ICONERROR)
		return
	}
	app.hwnd = hwnd
	win.Logf = logf
	app.icon = makeAppIcon()
	app.registerHotkeys()

	app.taskbarMsg = win.RegisterWindowMessage("TaskbarCreated")
	// pre-create the overlay so the first PrintScreen is instant too
	app.overlay = newOverlay(app)
	logf("overlay window pre-created")

	app.tray = win.NewTray(hwnd, app.icon, appTitle+" — PrintScreen: захват", win.WM_TRAYICON)
	if !cfg.TrayPromoted {
		// give the shell a moment to create the NotifyIconSettings entry, then
		// pin the icon to the taskbar (Windows 11 hides new icons by default)
		win.SetTimer(hwnd, 1, 1500)
	}
	var m win.MSG
	for {
		r := win.GetMessage(&m)
		if r == 0 || r == -1 {
			break
		}
		win.TranslateMessage(&m)
		win.DispatchMessage(&m)
	}
}

func (a *App) registerHotkeys() {
	a.hotkeyMods, a.hotkeyVK = parseHotkey(a.cfg.Hotkey)
	// The primary hotkey goes through a low level keyboard hook: on Windows 11 the
	// shell owns PrintScreen, so RegisterHotKey reports "already registered"
	// (error 1409) and the Snipping Tool would win the race.
	if !win.InstallKeyboardHook(a.keyFilter) {
		logf("keyboard hook install failed, falling back to RegisterHotKey")
		if ok, errno := win.RegisterHotKey(a.hwnd, 1, a.hotkeyMods|win.MOD_NOREPEAT, a.hotkeyVK); ok {
			a.hotkeyID = 1
		} else {
			logf("hotkey %q registration failed (err=%d)", a.cfg.Hotkey, errno)
		}
	} else {
		logf("keyboard hook installed for %q (mods=%x vk=%x)", a.cfg.Hotkey, a.hotkeyMods, a.hotkeyVK)
	}
	// Backup shortcut that never conflicts with the shell.
	a.altHotkeyID = 2
	if ok, _ := win.RegisterHotKey(a.hwnd, a.altHotkeyID, win.MOD_CONTROL|win.MOD_ALT|win.MOD_NOREPEAT, win.VK_S); !ok {
		a.altHotkeyID = 0
	}
}

// keyFilter swallows the configured hotkey and arms the capture.
func (a *App) keyFilter(vk uint32, down bool, mods uint32) bool {
	if vk != a.hotkeyVK || mods != a.hotkeyMods {
		return false
	}
	if down {
		if a.active {
			return true // already capturing: ignore the repeat
		}
		a.hwnd.PostMessage(win.WM_CAPTURED, 0, 0)
	}
	return true
}

func parseHotkey(s string) (uint32, uint32) {
	var mods uint32
	vk := uint32(win.VK_SNAPSHOT)
	up := upper(s)
	parts := splitPlus(up)
	for _, p := range parts {
		switch p {
		case "CTRL", "CONTROL":
			mods |= win.MOD_CONTROL
		case "ALT":
			mods |= win.MOD_ALT
		case "SHIFT":
			mods |= win.MOD_SHIFT
		case "WIN":
			mods |= win.MOD_WIN
		case "PRINTSCREEN", "PRTSC", "SNAPSHOT":
			vk = win.VK_SNAPSHOT
		case "":
		default:
			if len(p) == 1 {
				vk = uint32(p[0])
			}
		}
	}
	return mods, vk
}

func (a *App) wndProc(hwnd win.HWND, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case win.WM_HOTKEY:
		if !a.active {
			a.startCapture()
		}
		return 0
	case win.WM_CAPTURED:
		a.startCapture()
		return 0
	case win.WM_TIMER:
		if wp == 1 {
			win.KillTimer(hwnd, 1)
			exe := win.ExePath()
			if win.PromoteTrayIcon(exe) {
				a.tray.Refresh()
				logf("tray: promoted to taskbar")
			} else {
				logf("tray: promotion entry not found yet")
			}
			a.cfg.TrayPromoted = true
			a.cfg.save()
		}
		return 0
	case win.WM_TRAYICON:
		switch uint32(lp) & 0xFFFF {
		case win.WM_LBUTTONUP:
			a.startCapture()
		case win.WM_LBUTTONDBLCLK:
			a.startCapture()
		case win.WM_RBUTTONUP, win.WM_CONTEXTMENU:
			a.showTrayMenu()
		}
		return 0
	case a.taskbarMsg:
		// explorer restarted: the tray icon has to be registered again
		if a.tray != nil {
			ok, errno := a.tray.Add()
			logf("tray: re-added after explorer restart ok=%v err=%d", ok, errno)
		}
		return 0
	case win.WM_DESTROY:
		win.PostQuitMessage(0)
		return 0
	}
	return win.DefWindowProc(hwnd, msg, wp, lp)
}

func (a *App) showTrayMenu() {
	m := win.NewPopupMenu()
	defer m.Destroy()
	m.Add(cmdCapture, "Захват области\tPrintScreen", false)
	m.Default(cmdCapture)
	m.Separator()
	m.Add(cmdFolder, "Открыть папку сохранения", false)
	m.Add(cmdConfig, "Настройки (config.json)", false)
	m.Add(cmdAuto, "Автозапуск при входе", a.cfg.AutoStart)
	m.Separator()
	m.Add(cmdQuit, "Выход", false)
	switch m.Track(a.hwnd) {
	case cmdCapture:
		a.startCapture()
	case cmdFolder:
		win.ShellExecute("open", a.cfg.SaveDir, "", "", win.SW_SHOWNORMAL)
	case cmdConfig:
		win.ShellExecute("open", a.cfg.path, "", "", win.SW_SHOWNORMAL)
	case cmdAuto:
		a.cfg.AutoStart = !a.cfg.AutoStart
		a.cfg.save()
		_ = ensureAutoStart(a.cfg.AutoStart)
	case cmdQuit:
		a.hwnd.PostMessage(win.WM_CLOSE, 0, 0)
	}
}

// startCapture freezes the screen and opens the selection overlay. This is the
// hot path: capture + dim + show must stay in the tens of milliseconds.
func (a *App) startCapture() {
	if a.active {
		return
	}
	start := win.GetTickCount()
	defer func() {
		logf("capture pipeline %d ms", win.GetTickCount()-start)
	}()
	a.active = true
	screen := win.VirtualScreen()
	a.screen = screen

	t0 := win.GetTickCount()
	src, err := win.NewDib(screen.W(), screen.H())
	if err != nil {
		a.active = false
		return
	}
	sdc, release := win.ScreenDC()
	src.BlitRaw(sdc, screen.Left, screen.Top, screen.W(), screen.H())
	release()
	t1 := win.GetTickCount()
	a.src = src

	frame, err := win.NewDib(screen.W(), screen.H())
	if err != nil {
		src.Close()
		a.src = nil
		a.active = false
		return
	}
	a.frame = frame

	t2 := win.GetTickCount()

	if a.overlay == nil {
		a.overlay = newOverlay(a)
	}
	a.overlay.show()
	t3 := win.GetTickCount()
	logf("timings: capture=%dms dim=%dms overlay=%dms (total %dms)", t1-t0, t2-t1, t3-t2, t3-t0)
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
}

func (a *App) startEditor(region win.RECT) {
	a.openEditor(region)
}

func (a *App) notify(title, text string) {
	if a.tray != nil {
		a.tray.Balloon(title, text, win.NIIF_INFO)
	}
}

// saveImage encodes the composed image to PNG inside the configured folder.
func (a *App) saveImage(bgra []byte, w, h int) (string, error) {
	dir := a.cfg.SaveDir
	if dir == "" {
		dir = defaultConfig().SaveDir
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := expandTemplate(a.cfg.FileNameTemplate, time.Now())
	if base == "" {
		base = "YAS_" + time.Now().Format("2006-01-02_15-04-05")
	}
	path := uniquePath(dir, base)
	data, err := encodePNG(bgra, w, h, 0)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	logf("saved %s (%dx%d, %d bytes)", path, w, h, len(data))
	return path, nil
}

func uniquePath(dir, base string) string {
	ext := ".png"
	name := base
	p := filepath.Join(dir, name+ext)
	if !exists(p) {
		return p
	}
	for i := 2; i < 10000; i++ {
		p = filepath.Join(dir, name+"_"+itoa(int64(i))+ext)
		if !exists(p) {
			return p
		}
	}
	return filepath.Join(dir, name+"_"+itoa(time.Now().UnixNano())+ext)
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// encodePNG converts top-down BGRA bytes into a PNG. bottomUp flips rows (used
// for CF_DIB clipboard data).
func encodePNG(bgra []byte, w, h int, _ int) ([]byte, error) {
	if len(bgra) < w*h*4 {
		return nil, errors.New("encode: short buffer")
	}
	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		si := y * w * 4
		di := y * nrgba.Stride
		for x := 0; x < w; x++ {
			nrgba.Pix[di+0] = bgra[si+2]
			nrgba.Pix[di+1] = bgra[si+1]
			nrgba.Pix[di+2] = bgra[si+0]
			nrgba.Pix[di+3] = 255
			si += 4
			di += 4
		}
	}
	var buf bytes.Buffer
	buf.Grow(w * h / 2)
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, nrgba); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func copyToClipboard(bgra []byte, w, h int) error {
	pngData, err := encodePNG(bgra, w, h, 0)
	if err != nil {
		return err
	}
	if !win.OpenClipboard(0) {
		return errors.New("clipboard busy")
	}
	defer win.CloseClipboard()
	win.EmptyClipboard()

	// DIB (bottom-up, 32bpp) for classic apps
	dib := make([]byte, 40+w*h*4)
	putLE32(dib[0:], 40)
	putLE32(dib[4:], uint32(w))
	putLE32(dib[8:], uint32(h))
	putLE16(dib[12:], 1)
	putLE16(dib[14:], 32)
	putLE32(dib[16:], 0)
	putLE32(dib[20:], uint32(w*h*4))
	for y := 0; y < h; y++ {
		srcRow := (h - 1 - y) * w * 4
		dst := 40 + y*w*4
		for x := 0; x < w*4; x += 4 {
			dib[dst+x] = bgra[srcRow+x]
			dib[dst+x+1] = bgra[srcRow+x+1]
			dib[dst+x+2] = bgra[srcRow+x+2]
			dib[dst+x+3] = 255
		}
	}
	win.SetClipboardData(win.CF_DIB, dib)

	if f := win.RegisterClipboardFormat("PNG"); f != 0 {
		win.SetClipboardData(f, pngData)
	}
	return nil
}

func putLE32(b []byte, v uint32) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
	b[3] = byte(v >> 24)
}

func putLE16(b []byte, v uint16) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
}

func upper(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 32
		}
	}
	return string(b)
}

func splitPlus(s string) []string {
	var out []string
	cur := ""
	for i := 0; i < len(s); i++ {
		if s[i] == '+' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(s[i])
	}
	out = append(out, cur)
	return out
}

func ensureAutoStart(on bool) error {
	exe := win.ExePath()
	if exe == "" {
		return errors.New("exe path unknown")
	}
	if on {
		win.SetRunKey(appName, exe)
	} else {
		win.DeleteRunKey(appName)
	}
	return nil
}
