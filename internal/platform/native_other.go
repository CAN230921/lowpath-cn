//go:build !windows

package platform

import (
	"os"
	"runtime"

	"lowpath/internal/compat"
	"lowpath/internal/detect"
	"lowpath/internal/diag"
	"lowpath/internal/owner"
)

// Native assembles the Unix desktop platform. Linux uses TUN for route
// mode; macOS uses the signed Network Extension when bundled.
func Native() *Platform {
	owners := owner.NewTable()
	home, _ := os.UserHomeDir() // none: no game folders to look in
	games := detect.NewSystem(owners, detect.UnixLocations(runtime.GOOS, home, os.Getenv))
	return &Platform{
		OS:         runtime.GOOS,
		Capture:    nativeCapture(owners),
		Owners:     owners,
		Privileges: nativePrivileges(),
		Games:      games,
		Icons:      games,
		Paths:      NewUnixPaths(),
		Flows:      detect.NewKernelNetwork(),
		Inventory:  compat.NewSystem(games, diag.NewNetStats()),
		Service:    NewServiceManager(),
		// The socket's folder is root's, so only a root service can take the
		// name, and the window checks that root serves it. Every account may
		// connect (0666); the service only serves root and the accounts
		// signed in at the computer (NewSessionPolicy).
		ServiceAPI: NewSocketAPI(DefaultSocketPath, 0o666, NewSessionPolicy()).WithServer(0),
		WiFi:       diag.NewWLAN(),
		Conns:      diag.NewConnStats(),
	}
}
