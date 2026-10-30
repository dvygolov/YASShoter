//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/dvygolov/yasshoter/internal/win"
)

// Tiny diagnostic log: %APPDATA%\YASShoter\log.txt, truncated on every start.
// Cheap (a handful of lines per capture) and invaluable when something on the
// user's machine behaves differently.

var (
	logMu   sync.Mutex
	logFile *os.File
	logOn   bool
	logPath string
)

func initLog() {
	dir := win.KnownFolderPath(win.FOLDERID_RoamingAppData)
	if dir == "" {
		return
	}
	logPath = filepath.Join(dir, appName, "log.txt")
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	f, err := os.Create(logPath)
	if err != nil {
		return
	}
	logFile = f
	logOn = true
	logf("start %s pid=%d", version, win.SelfPID())
}

func logf(format string, args ...any) {
	if !logOn {
		return
	}
	logMu.Lock()
	defer logMu.Unlock()
	fmt.Fprintf(logFile, "%s  %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
	_ = logFile.Sync()
}
