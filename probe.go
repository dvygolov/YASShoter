//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dvygolov/yasshoter/internal/win"
)

// audioProbeMode: `yas-shotter.exe --audio-probe` records one second from every
// active playback device and reports the peak level, so it is obvious which
// endpoint actually carries sound (the loopback default is not always the one
// the user hears).

func audioProbeEnabled() bool {
	for _, a := range os.Args[1:] {
		if a == "--audio-probe" || a == "-audio-probe" {
			return true
		}
	}
	return false
}

func runAudioProbe() {
	win.ComInit()
	defer win.ComUninit()

	var report []string
	report = append(report, "YASShoter audio probe "+version+"  "+time.Now().Format("2006-01-02 15:04:05"))
	report = append(report, "")

	devices, err := win.ListRenderDevices()
	if err != nil {
		report = append(report, "не удалось получить список устройств: "+err.Error())
	} else if len(devices) == 0 {
		report = append(report, "активных устройств воспроизведения нет")
	}
	for _, d := range devices {
		peak, perr := probeDevice(d.ID, 900*time.Millisecond)
		name := d.Name
		if name == "" {
			name = "(без имени)"
		}
		line := fmt.Sprintf("%-40s peak=%5d  %s", name, peak, d.ID)
		if perr != nil {
			line += "  [" + perr.Error() + "]"
		} else if peak < 300 {
			line += "  (тишина)"
		} else {
			line += "  ← здесь есть звук"
		}
		report = append(report, line)
		logf("probe: %s", line)
	}
	report = append(report, "", "Чтобы закрепить устройство, впишите фрагмент его id в config.json:")
	report = append(report, `  "audioDevice": "{0.0.0.00000000}.{...}"`)

	out := filepath.Join(os.TempDir(), "YASShoter-audio-probe.txt")
	text := ""
	for _, l := range report {
		text += l + "\r\n"
	}
	_ = os.WriteFile(out, []byte(text), 0o644)
	win.ShellExecute("open", out, "", filepath.Dir(out), win.SW_SHOWNORMAL)
}

func probeDevice(id string, dur time.Duration) (int, error) {
	cap, err := win.OpenLoopbackAudio(id)
	if err != nil {
		return 0, err
	}
	defer cap.Close()
	deadline := time.Now().Add(dur)
	peak := 0
	for time.Now().Before(deadline) {
		pcm, rerr := cap.ReadPCM()
		if rerr != nil {
			return peak, rerr
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
	return peak, nil
}

// menuDumpMode: `yas-shotter.exe --menu-dump` builds the tray menu and writes it
// to the log, a cheap sanity check for the device submenus without clicking.
func menuDumpEnabled() bool {
	for _, a := range os.Args[1:] {
		if a == "--menu-dump" {
			return true
		}
	}
	return false
}

func runMenuDump(app *App) {
	m := app.buildTrayMenu()
	defer m.Destroy()
	logf("menu-dump: built menu with %d devices for source=%s device=%q",
		len(app.menuDevices), app.cfg.AudioSource, app.cfg.AudioDevice)
	for i, d := range app.menuDevices {
		logf("menu-dump: system device %d = %q %s", i, d.Name, d.ID)
	}
	for i, d := range app.menuMicDevices {
		logf("menu-dump: mic device %d = %q %s", i, d.Name, d.ID)
	}
}
