//go:build windows

package main

import "runtime"

//go:generate go run ./tools/genicon -out build/app.ico

const version = "0.1.0"

func main() {
	// Win32 windows, hooks and the message loop must all live on one OS thread.
	runtime.LockOSThread()
	initLog()
	defer func() {
		if r := recover(); r != nil {
			logf("PANIC %v", r)
			panic(r)
		}
	}()
	runApp()
}
