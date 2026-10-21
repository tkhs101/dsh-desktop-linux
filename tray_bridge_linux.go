//go:build linux

package main

// The tray menu lives in C, but what it starts is Go work: reloading the page,
// respawning the host, replacing this process. This file is the bridge back into
// Go, and it exists on its own because a file that uses //export may not define
// any C in its preamble -- tray_linux.go is all definitions.

import "C"

//export dshTrayAction
func dshTrayAction(action C.int) {
	if trayApp == nil {
		return
	}
	trayApp.trayAction(int(action))
}
