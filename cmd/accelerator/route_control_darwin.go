//go:build cgo

package main

import (
	"os"
	"runtime"

	"lowpath/internal/capture"
)

func init() {
	if isRouteController() {
		runtime.LockOSThread()
	}
}

func isRouteController() bool {
	return len(os.Args) == 2 && os.Args[1] == capture.MacProxyControlArg
}

func runRouteController() error { return capture.RunMacProxyController() }
