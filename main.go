//go:build windows

package main

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync"
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

	taskbarMsg    uint32
	pendingUpdate bool
	menuDevices   []win.RenderDevice

	rec          *Recorder
	recIcon      win.HICON
	recMu        sync.Mutex
	recSavedPath string
	recSavedErr  string
	probePeak    int
	hotkeyID     int32
	altHotkeyID  int32
	hotkeyMods   uint32
	hotkeyVK     uint32
	videoMods    uint32
	videoVK      uint32
}

const (
	umHotkeyShot     = win.WM_APP + 20
	umHotkeyVideo    = win.WM_APP + 21
	umRecordingSaved = win.WM_APP + 22
	umProbeDone      = win.WM_APP + 23
)

const (
	cmdCapture       = 1
	cmdFolder        = 2
	cmdConfig        = 3
	cmdAuto          = 4
	cmdQuit          = 5
	cmdCheckUpd      = 6
	cmdDoUpdate      = 7
	cmdRecord        = 8
	cmdStopRec       = 9
	cmdAudioOn       = 10
	cmdCursorOn      = 11
	cmdAudioOff      = 12
	cmdAudioSystem   = 13
	cmdAudioMic      = 14
	cmdDeviceDefault = 15

	// device menu items live in this id range
	cmdDeviceBase = 200
)

func runApp() {
	win.SetProcessDPIAware()
	cfg := loadConfig()
	app := &App{cfg: cfg}
	cleanupOldBinary()

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
	app.startUpdateLoop()

	if demoEditorEnabled() {
		app.openDemoEditor()
	}
	if menuDumpEnabled() {
		runMenuDump(app)
	}
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
	vm, vk := parseHotkey(a.cfg.HotkeyVideo)
	a.videoMods, a.videoVK = vm, vk
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

// keyFilter swallows the configured hotkeys and arms the capture / recording.
// Matching is lenient about unexpected modifier bits for the video hotkey: a
// stuck Win key or a keyboard that reports extra modifiers must not kill it.
func (a *App) keyFilter(vk uint32, down bool, mods uint32) bool {
	// MOD_WIN is ignored on purpose: a stuck Windows key or a keyboard/driver that
	// reports it spuriously must not disable the hotkeys.
	const modMask = win.MOD_CONTROL | win.MOD_ALT | win.MOD_SHIFT
	if vk == win.VK_SNAPSHOT && down {
		logf("hook: PrintScreen down mods=%#x (video wants %#x, shot wants %#x)", mods, a.videoMods, a.hotkeyMods)
	}

	if vk == a.videoVK && down && (mods&modMask) == (a.videoMods&modMask) {
		if a.active {
			logf("hook: video hotkey ignored, capture in progress")
			return true
		}
		logf("hook: video hotkey -> recording (rec=%v)", a.rec != nil)
		a.hwnd.PostMessage(umHotkeyVideo, 0, 0)
		return true
	}
	if vk == a.videoVK && (mods&modMask) == (a.videoMods&modMask) {
		return true // swallow the key up as well
	}

	if vk != a.hotkeyVK || (mods&modMask) != (a.hotkeyMods&modMask) {
		return false
	}
	if down {
		if a.active {
			return true // already capturing: ignore the repeat
		}
		a.hwnd.PostMessage(umHotkeyShot, 0, 0)
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
			a.startCapture(modeShot)
		}
		return 0
	case umHotkeyShot:
		a.startCapture(modeShot)
		return 0
	case umHotkeyVideo:
		a.toggleRecording()
		return 0
	case umProbeDone:
		a.recMu.Lock()
		peak := a.probePeak
		a.recMu.Unlock()
		if peak < 300 {
			a.notify("Звук: тишина", "На выбранном устройстве сейчас тишина. Проверьте громкость и куда играет приложение.")
		} else {
			a.notify("Звук есть", "На выбранном устройстве есть сигнал (peak "+itoa(int64(peak))+").")
		}
		return 0
	case umRecordingSaved:
		a.onRecordingSaved()
		return 0
	case win.WM_CAPTURED:
		a.startCapture(modeShot)
		return 0
	case win.WM_TIMER:
		if wp == 4 {
			a.updateRecTooltip()
		}
		if wp == 2 {
			win.KillTimer(hwnd, 2)
			a.requestUpdateCheck(false)
			win.SetTimer(hwnd, 3, uint32(a.updateInterval()/time.Millisecond))
		}
		if wp == 3 {
			a.requestUpdateCheck(false)
		}
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
			if a.rec != nil {
				a.stopRecording()
			} else {
				a.startCapture(modeShot)
			}
		case win.WM_LBUTTONDBLCLK:
			a.startCapture(modeShot)
		case win.WM_RBUTTONUP, win.WM_CONTEXTMENU:
			a.showTrayMenu()
		}
		return 0
	case umUpdateBusy:
		return 0
	case umUpdateReady:
		a.onUpdateReady()
		return 0
	case umUpdateFailed:
		a.onUpdateFailed()
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
	m := a.buildTrayMenu()
	defer m.Destroy()
	cmd := m.Track(a.hwnd)
	m.Destroy()
	a.handleTrayCommand(cmd)
}

// buildTrayMenu creates the whole menu (including the audio submenus).
func (a *App) buildTrayMenu() *win.Menu {
	m := win.NewPopupMenu()
	if a.rec != nil {
		m.Add(cmdStopRec, "Остановить запись\tCtrl+PrintScreen", false)
		m.Default(cmdStopRec)
	} else {
		m.Add(cmdCapture, "Снимок области\tPrintScreen", false)
		m.Default(cmdCapture)
		m.Add(cmdRecord, "Запись видео\tCtrl+PrintScreen", false)
	}

	// sound source
	srcMenu := win.NewPopupMenu()
	srcMenu.Add(cmdAudioOff, "Без звука", a.cfg.AudioSource == "off")
	srcMenu.Add(cmdAudioSystem, "Система (то, что играет)", a.cfg.AudioSource == "system")
	srcMenu.Add(cmdAudioMic, "Микрофон", a.cfg.AudioSource == "mic")
	srcLabel := "Звук: "
	switch a.cfg.AudioSource {
	case "mic":
		srcLabel += "микрофон"
	case "off":
		srcLabel += "выкл"
	default:
		srcLabel += "система"
	}
	m.Submenu(srcLabel, srcMenu)

	// device list for the current source
	a.menuDevices = listAudioDevices(a.cfg.AudioSource == "mic")
	devMenu := win.NewPopupMenu()
	devMenu.Add(cmdDeviceDefault, "По умолчанию (как в Windows)", a.cfg.AudioDevice == "")
	selectedName := "по умолчанию"
	for i, d := range a.menuDevices {
		checked := a.cfg.AudioDevice != "" && d.ID == a.cfg.AudioDevice
		if checked {
			selectedName = d.Name
		}
		name := d.Name
		if name == "" {
			name = "Устройство " + itoa(int64(i+1))
		}
		devMenu.Add(uint32(cmdDeviceBase+i), shorten(name, 48), checked)
	}
	m.Submenu("Устройство: "+selectedName, devMenu)

	m.Add(cmdCursorOn, "Показывать курсор", a.cfg.VideoCursor)
	m.Separator()
	m.Add(cmdFolder, "Открыть папку сохранения", false)
	m.Add(cmdConfig, "Настройки (config.json)", false)
	m.Add(cmdAuto, "Автозапуск при входе", a.cfg.AutoStart)
	m.Separator()
	upd.mu.Lock()
	updVer := upd.version
	upd.mu.Unlock()
	if updVer != "" {
		m.Add(cmdDoUpdate, "Обновить до "+updVer+" и перезапустить", false)
	}
	m.Add(cmdCheckUpd, "Проверить обновления (v"+version+")", false)
	m.Separator()
	m.Add(cmdQuit, "Выход", false)
	return m
}

// handleTrayCommand executes the chosen item.
func (a *App) handleTrayCommand(cmd uint32) {
	switch {
	case cmd == cmdCapture:
		a.startCapture(modeShot)
	case cmd == cmdRecord:
		a.toggleRecording()
	case cmd == cmdStopRec:
		a.stopRecording()
	case cmd == cmdAudioOff, cmd == cmdAudioSystem, cmd == cmdAudioMic:
		switch cmd {
		case cmdAudioOff:
			a.cfg.AudioSource = "off"
		case cmdAudioSystem:
			a.cfg.AudioSource = "system"
		default:
			a.cfg.AudioSource = "mic"
		}
		a.cfg.VideoAudio = a.cfg.AudioSource != "off"
		a.cfg.save()
		logf("menu: audio source -> %s", a.cfg.AudioSource)
		a.notify("Звук", "Источник: "+a.cfg.AudioSource)
	case cmd == cmdDeviceDefault:
		a.cfg.AudioDevice = ""
		a.cfg.save()
		logf("menu: audio device -> default")
		a.probeSelectedDevice("")
	case cmd >= cmdDeviceBase && int(cmd) < cmdDeviceBase+len(a.menuDevices):
		d := a.menuDevices[cmd-cmdDeviceBase]
		a.cfg.AudioDevice = d.ID
		a.cfg.save()
		logf("menu: audio device -> %s (%s)", d.Name, d.ID)
		a.probeSelectedDevice(d.ID)
	case cmd == cmdFolder:
		win.ShellExecute("open", a.cfg.SaveDir, "", "", win.SW_SHOWNORMAL)
	case cmd == cmdConfig:
		win.ShellExecute("open", a.cfg.path, "", "", win.SW_SHOWNORMAL)
	case cmd == cmdAuto:
		a.cfg.AutoStart = !a.cfg.AutoStart
		a.cfg.save()
		_ = ensureAutoStart(a.cfg.AutoStart)
	case cmd == cmdCheckUpd:
		a.requestUpdateCheck(true)
	case cmd == cmdDoUpdate:
		if err := a.applyUpdate(); err != nil {
			win.MessageBox("Не удалось обновиться:\n"+err.Error(), appTitle, win.MB_OK|win.MB_ICONERROR|win.MB_TOPMOST)
		}
	case cmd == cmdQuit:
		a.hwnd.PostMessage(win.WM_CLOSE, 0, 0)
	}
}

// listAudioDevices enumerates endpoints for the given source.
func listAudioDevices(mic bool) []win.RenderDevice {
	done := make(chan []win.RenderDevice, 1)
	go func() {
		if !win.ComInit() {
			done <- nil
			return
		}
		var devs []win.RenderDevice
		var err error
		if mic {
			devs, err = win.ListCaptureDevices()
		} else {
			devs, err = win.ListRenderDevices()
		}
		if err != nil {
			logf("devices: %v", err)
			devs = nil
		}
		done <- devs
	}()
	select {
	case d := <-done:
		return d
	case <-time.After(2 * time.Second):
		return nil
	}
}

// probeSelectedDevice tells the user whether the chosen endpoint carries sound
// right now, which is the fastest way to catch a wrong device.
func (a *App) probeSelectedDevice(deviceID string) {
	source := a.cfg.AudioSource
	if source == "off" {
		source = "system"
	}
	go func() {
		if !win.ComInit() {
			return
		}
		var cap *win.AudioCapture
		var err error
		if source == "mic" {
			cap, err = win.OpenMicrophoneAudio(deviceID)
		} else {
			cap, err = win.OpenLoopbackAudio(deviceID)
		}
		if err != nil {
			a.hwnd.PostMessage(umProbeDone, 0, 0)
			logf("menu: probe failed: %v", err)
			return
		}
		peak := probeCapture(cap, 1200*time.Millisecond)
		cap.Close()
		a.recMu.Lock()
		a.probePeak = peak
		a.recMu.Unlock()
		a.hwnd.PostMessage(umProbeDone, 0, 0)
	}()
}

func probeCapture(cap *win.AudioCapture, dur time.Duration) int {
	deadline := time.Now().Add(dur)
	peak := 0
	for time.Now().Before(deadline) {
		pcm, err := cap.ReadPCM()
		if err != nil {
			break
		}
		for i := 0; i+1 < len(pcm); i += 2 {
			v := int(int16(uint16(pcm[i]) | uint16(pcm[i+1])<<8))
			if v < 0 {
				v = -v
			}
			if v > peak {
				peak = v
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return peak
}

// toggleRecording starts the video overlay or stops an active recording.
func (a *App) toggleRecording() {
	if a.rec != nil {
		logf("hotkey: stop requested")
		a.stopRecording()
		return
	}
	a.startCapture(modeVideo)
}

// enterRecordingUI switches the tray icon to the red "recording" state.
func (a *App) enterRecordingUI() {
	if a.recIcon == 0 {
		a.recIcon = makeRecordIcon()
	}
	if a.tray != nil {
		a.tray.SetIcon(a.recIcon)
		a.updateRecTooltip()
	}
	win.SetTimer(a.hwnd, 4, 1000)
}

func (a *App) leaveRecordingUI() {
	win.KillTimer(a.hwnd, 4)
	if a.tray != nil {
		a.tray.SetIcon(a.icon)
		a.tray.SetTip(appTitle + " — PrintScreen: снимок, Ctrl+PrintScreen: видео")
	}
}

func (a *App) updateRecTooltip() {
	r := a.rec
	if a.tray == nil || r == nil {
		return
	}
	el := r.elapsed()
	tip := "● Запись " + el.Truncate(time.Second).String() + " · Ctrl+PrintScreen — стоп"
	if !r.audio {
		tip += " · без звука"
	}
	a.tray.SetTip(tip)
}

func (a *App) onRecordingSaved() {
	a.recMu.Lock()
	path, errMsg := a.recSavedPath, a.recSavedErr
	a.recSavedPath, a.recSavedErr = "", ""
	a.recMu.Unlock()
	if errMsg != "" {
		a.notify("Запись", "Ошибка: "+errMsg)
		return
	}
	a.notify("Видео сохранено", filepath.Base(path))
}

// onUpdateReady reacts to a downloaded update.
func (a *App) onUpdateReady() {
	upd.mu.Lock()
	ver := upd.version
	msg := upd.message
	manual := upd.manifest
	upd.mu.Unlock()
	if ver == "" {
		return
	}
	logf("update: %s ready, mode=%s active=%v", ver, a.cfg.AutoUpdate, a.active)
	if a.cfg.AutoUpdate == "auto" {
		if err := a.applyUpdate(); err != nil {
			logf("update: apply failed: %v", err)
			a.notify("Обновление "+ver, "Не удалось обновиться автоматически: "+err.Error())
		}
		return
	}
	if manual {
		win.MessageBox("Скачана версия "+ver+".\nОбновить сейчас? Это перезапустит YASShoter.",
			appTitle, win.MB_OKCANCEL|win.MB_ICONINFORMATION|win.MB_TOPMOST)
		if err := a.applyUpdate(); err != nil {
			a.notify("Обновление "+ver, err.Error())
		}
		return
	}
	a.notify("Доступно обновление "+ver, "Меню трея → «Обновить до "+ver+"»")
	_ = msg
}

func (a *App) onUpdateFailed() {
	upd.mu.Lock()
	msg := upd.message
	manual := upd.manifest
	upd.mu.Unlock()
	if msg == "" {
		return
	}
	if manual {
		win.MessageBox(msg, appTitle, win.MB_OK|win.MB_ICONINFORMATION|win.MB_TOPMOST)
		return
	}
	logf("update: %s", msg)
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
