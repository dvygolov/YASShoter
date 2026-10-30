//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/dvygolov/yasshoter/internal/win"
)

const (
	appName  = "YASShoter"
	appTitle = "YASShoter"
)

// Config is stored in %APPDATA%\YASShoter\config.json and is intentionally tiny.
type Config struct {
	SaveDir          string  `json:"saveDir"`
	FileNameTemplate string  `json:"fileNameTemplate"`
	Color            string  `json:"color"`
	Thickness        float64 `json:"thickness"`
	Hotkey           string  `json:"hotkey"`
	AutoStart        bool    `json:"autoStart"`
	CopyToClipboard  bool    `json:"copyToClipboard"`
	DisableSnipping  bool    `json:"disableSnippingToolPrintScreen"`
	DimAlpha         int     `json:"dimAlpha"`
	TrayPromoted     bool    `json:"trayPromoted"`

	path string
}

func defaultConfig() *Config {
	dir := win.KnownFolderPath(win.FOLDERID_Downloads)
	if dir == "" {
		dir = filepath.Join(os.Getenv("USERPROFILE"), "Downloads")
	}
	return &Config{
		SaveDir:          dir,
		FileNameTemplate: "YAS_{date}_{time}",
		Color:            "FF3B30",
		Thickness:        4,
		Hotkey:           "PrintScreen",
		AutoStart:        true,
		CopyToClipboard:  true,
		DisableSnipping:  false,
		DimAlpha:         140,
	}
}

func configPath() string {
	dir := win.KnownFolderPath(win.FOLDERID_RoamingAppData)
	if dir == "" {
		dir = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
	}
	return filepath.Join(dir, appName, "config.json")
}

func loadConfig() *Config {
	cfg := defaultConfig()
	cfg.path = configPath()
	data, err := os.ReadFile(cfg.path)
	if err != nil {
		cfg.save()
		return cfg
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return cfg
	}
	if cfg.SaveDir == "" {
		cfg.SaveDir = defaultConfig().SaveDir
	}
	if cfg.Thickness < 1 {
		cfg.Thickness = 4
	}
	if cfg.Color == "" {
		cfg.Color = "FF3B30"
	}
	cfg.path = configPath()
	return cfg
}

func (c *Config) save() {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err == nil {
		_ = os.Rename(tmp, c.path)
	}
}

// parseColor converts "RRGGBB" / "#RRGGBB" / "AARRGGBB" into 0x00RRGGBB.
func parseColor(s string, fallback uint32) uint32 {
	if s == "" {
		return fallback
	}
	if s[0] == '#' {
		s = s[1:]
	}
	var v uint32
	for i := 0; i < len(s); i++ {
		c := s[i]
		var d uint32
		switch {
		case c >= '0' && c <= '9':
			d = uint32(c - '0')
		case c >= 'a' && c <= 'f':
			d = uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = uint32(c-'A') + 10
		default:
			return fallback
		}
		v = v<<4 | d
	}
	if len(s) == 6 {
		return v
	}
	if len(s) == 8 {
		return v & 0x00FFFFFF
	}
	return fallback
}

func expandTemplate(tpl string, t time.Time) string {
	out := make([]byte, 0, len(tpl)+16)
	repl := map[string]string{
		"{date}":     t.Format("2006-01-02"),
		"{time}":     t.Format("15-04-05"),
		"{datetime}": t.Format("2006-01-02_15-04-05"),
		"{yyyy}":     t.Format("2006"),
		"{MM}":       t.Format("01"),
		"{dd}":       t.Format("02"),
		"{HH}":       t.Format("15"),
		"{mm}":       t.Format("04"),
		"{ss}":       t.Format("05"),
		"{unix}":     itoa(t.Unix()),
	}
	for i := 0; i < len(tpl); {
		if tpl[i] == '{' {
			if j := indexByte(tpl[i:], '}'); j > 0 {
				key := tpl[i : i+j+1]
				if v, ok := repl[key]; ok {
					out = append(out, v...)
					i += j + 1
					continue
				}
			}
		}
		out = append(out, tpl[i])
		i++
	}
	return string(out)
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
