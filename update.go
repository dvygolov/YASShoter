//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dvygolov/yasshoter/internal/win"
)

// Self-update: ask the GitHub API for the newest release, download the asset and
// swap the running exe (MoveFileEx + relaunch). Stdlib only, no dependencies.

const (
	updateAPI  = "https://api.github.com/repos/dvygolov/YASShoter/releases/latest"
	updateRepo = "dvygolov/YASShoter"

	umUpdateAvailable = win.WM_APP + 10
	umUpdateReady     = win.WM_APP + 11
	umUpdateFailed    = win.WM_APP + 12
	umUpdateBusy      = win.WM_APP + 13
)

type ghRelease struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

type updater struct {
	mu       sync.Mutex
	busy     bool
	version  string
	path     string
	message  string
	manifest bool
}

var upd updater

func (a *App) updateInterval() time.Duration {
	if a.cfg.UpdateCheckHours <= 0 {
		return 6 * time.Hour
	}
	return time.Duration(a.cfg.UpdateCheckHours) * time.Hour
}

// startUpdateLoop schedules the first check shortly after startup and then once
// per configured interval.
func (a *App) startUpdateLoop() {
	if a.cfg.AutoUpdate == "off" {
		return
	}
	win.SetTimer(a.hwnd, 2, 8000) // first check, right after the tray is ready
}

func (a *App) requestUpdateCheck(manual bool) {
	upd.mu.Lock()
	if upd.busy {
		upd.mu.Unlock()
		return
	}
	upd.busy = true
	upd.mu.Unlock()
	a.hwnd.PostMessage(umUpdateBusy, 0, 0)

	go func() {
		// the check allocates TLS buffers and a copy of the exe: hand the pages
		// back to the OS once we are done so the idle footprint stays small
		defer debug.FreeOSMemory()
		rel, err := fetchLatestRelease()
		if err != nil {
			upd.setMessage("Не удалось проверить обновления: "+err.Error(), manual)
			a.hwnd.PostMessage(umUpdateFailed, 0, 0)
			return
		}
		newer, ok := versionLess(version, rel.TagName)
		if !ok || !newer {
			upd.mu.Lock()
			upd.busy = false
			upd.version = ""
			upd.mu.Unlock()
			if manual {
				logf("update: already up to date (%s)", version)
				upd.setMessage("Установлена последняя версия "+version, true)
				a.hwnd.PostMessage(umUpdateFailed, 0, 0)
			}
			return
		}
		var assetURL string
		var assetSize int64
		for _, as := range rel.Assets {
			if strings.EqualFold(as.Name, "yas-shotter.exe") {
				assetURL, assetSize = as.URL, as.Size
				break
			}
		}
		if assetURL == "" {
			upd.setMessage("В релизе "+rel.TagName+" нет exe", true)
			upd.mu.Lock()
			upd.busy = false
			upd.mu.Unlock()
			a.hwnd.PostMessage(umUpdateFailed, 0, 0)
			return
		}
		logf("update: %s available (%s)", rel.TagName, assetURL)

		// download in the same goroutine so the UI never blocks
		path, err := downloadAsset(assetURL, assetSize)
		upd.mu.Lock()
		upd.busy = false
		if err == nil {
			upd.version = strings.TrimPrefix(rel.TagName, "v")
			upd.path = path
		}
		upd.mu.Unlock()
		if err != nil {
			logf("update: download failed: %v", err)
			upd.setMessage("Не удалось скачать обновление: "+err.Error(), manual)
			a.hwnd.PostMessage(umUpdateFailed, 0, 0)
			return
		}
		logf("update: downloaded to %s", path)
		a.hwnd.PostMessage(umUpdateReady, 0, 0)
	}()
}

func (u *updater) setMessage(msg string, manual bool) {
	u.mu.Lock()
	u.busy = false
	u.message = msg
	u.manifest = manual
	u.mu.Unlock()
}

func fetchLatestRelease() (*ghRelease, error) {
	req, err := http.NewRequest("GET", updateAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "YASShoter/"+version)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, errors.New("релизов пока нет")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub вернул %s", resp.Status)
	}
	var rel ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	if rel.TagName == "" {
		return nil, errors.New("пустой ответ GitHub")
	}
	return &rel, nil
}

func downloadAsset(url string, size int64) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "YASShoter/"+version)
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("скачивание: %s", resp.Status)
	}
	dir := filepath.Join(os.TempDir(), "YASShoter-update")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "yas-shotter.exe")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, 32<<20))
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if size > 0 && n != size {
		return "", fmt.Errorf("размер файла %d вместо %d", n, size)
	}
	// sanity check: must be a Windows executable
	head := make([]byte, 2)
	fh, err := os.Open(path)
	if err != nil {
		return "", err
	}
	_, readErr := io.ReadFull(fh, head)
	fh.Close()
	if readErr != nil || head[0] != 'M' || head[1] != 'Z' {
		return "", errors.New("скачанный файл не является exe")
	}
	return path, nil
}

// applyUpdate swaps the running binary and restarts. Returns an error when the
// install directory is not writable (for example Program Files without rights).
func (a *App) applyUpdate() error {
	upd.mu.Lock()
	path := upd.path
	ver := upd.version
	upd.mu.Unlock()
	if path == "" {
		return errors.New("нет загруженного обновления")
	}
	if a.active {
		// never rip the app out from under an active capture
		a.pendingUpdate = true
		return nil
	}
	cur := win.ExePath()
	if cur == "" {
		return errors.New("не удалось определить путь exe")
	}
	old := cur + ".old"
	_ = os.Remove(old)
	if err := os.Rename(cur, old); err != nil {
		return fmt.Errorf("заменить файл нельзя (%v) — обновитесь вручную", err)
	}
	if err := copyFile(path, cur); err != nil {
		_ = os.Rename(old, cur)
		return err
	}
	_ = os.Remove(path)
	// hand the instance slot over before the new process starts, otherwise it
	// would wait for us and give up while we are still shutting down
	if instanceMutex != 0 {
		win.CloseHandle(instanceMutex)
		instanceMutex = 0
	}
	if !win.ShellExecuteOK("open", cur, "", filepath.Dir(cur), win.SW_SHOWNORMAL) {
		// keep running on the new binary instead of vanishing from the tray
		logf("update: relaunch failed, staying alive (restart manually)")
		a.notify("Обновление "+ver, "Файл обновлён, перезапустите YASShoter вручную")
		return nil
	}
	logf("update: installed %s, relaunched", ver)
	a.hwnd.PostMessage(win.WM_CLOSE, 0, 0)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// cleanupOldBinary drops the leftover from a previous self-update.
func cleanupOldBinary() {
	cur := win.ExePath()
	if cur == "" {
		return
	}
	old := cur + ".old"
	if _, err := os.Stat(old); err == nil {
		if err := os.Remove(old); err == nil {
			logf("update: removed previous binary")
		}
	}
}

// versionLess reports whether a is older than b. ok is false when either tag is
// not a plain numeric version, so we never offer a downgrade.
func versionLess(a, b string) (bool, bool) {
	pa, okA := parseVersion(a)
	pb, okB := parseVersion(b)
	if !okA || !okB {
		return false, false
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] < pb[i], true
		}
	}
	return false, true
}

func parseVersion(s string) ([3]int, bool) {
	var out [3]int
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "v"))
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return out, false
	}
	parts := strings.Split(s, ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
