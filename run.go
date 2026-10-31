//go:build windows

package main

import (
	"runtime"

	"github.com/dvygolov/yasshoter/internal/win"
)

//go:generate go run ./tools/genicon -out build/app.ico

// version is stamped at build time: -X main.version=<tag> (see Makefile).
var version = "0.1.2"

func main() {
	// Win32 windows, hooks and the message loop must all live on one OS thread.
	runtime.LockOSThread()

	// Claim the single instance slot before touching the log: a second process
	// would otherwise truncate the file the first one is writing to. The retry
	// window lets a self-update relaunch land while the old process exits.
	if !acquireSingleInstance() {
		return
	}
	initLog()
	defer func() {
		if r := recover(); r != nil {
			logf("PANIC %v", r)
			panic(r)
		}
	}()
	runApp()
}

// acquireSingleInstance returns true when this process owns the instance mutex.
func acquireSingleInstance() bool {
	if _, err := win.CreateMutexAndCheck(appName + "_single_instance"); err == nil {
		return true
	}
	for i := 0; i < 30; i++ {
		win.Sleep(100)
		if _, err := win.CreateMutexAndCheck(appName + "_single_instance"); err == nil {
			return true
		}
	}
	return false
}
