package platform

import (
	"context"
	"runtime"
	"slices"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procCreateWindowEx  = user32.NewProc("CreateWindowExW")
	procSendMessage     = user32.NewProc("SendMessageW")
	procIsWindow        = user32.NewProc("IsWindow")
	procIsWindowVisible = user32.NewProc("IsWindowVisible")
)

func newTrayTestWindow(t *testing.T) uintptr {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	class, _ := windows.UTF16PtrFromString("STATIC")
	const overlappedAndVisible = 0x10cf0000
	hwnd, _, err := procCreateWindowEx.Call(0, uintptr(unsafe.Pointer(class)), 0,
		overlappedAndVisible, 0, 0, 320, 200, 0, 0, 0, 0)
	if hwnd == 0 {
		t.Fatalf("create test window: %v", err)
	}
	t.Cleanup(func() { procDestroyWindow.Call(hwnd) })
	return hwnd
}

func TestWindowTrayLifecycle(t *testing.T) {
	hwnd := newTrayTestWindow(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var commands []uintptr
	tray, err := attachWindowTray(ctx, hwnd, func() bool { return true }, func(command uintptr, _ *notificationIcon) bool {
		commands = append(commands, command)
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	defer tray.detach()

	checkVisible := func(want bool) {
		t.Helper()
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if (visible != 0) != want {
			t.Fatalf("window visibility = %t, want %t", visible != 0, want)
		}
	}

	procSendMessage.Call(hwnd, wmSysCommand, scMinimize, 0)
	checkVisible(false)
	if !tray.inTray || !slices.Equal(commands, []uintptr{nimAdd, nimSetVersion}) {
		t.Fatalf("minimize: tray=%t, Shell commands=%v", tray.inTray, commands)
	}
	procSendMessage.Call(hwnd, trayEvent, 0, trayIconID<<16|ninSelect)
	checkVisible(true)
	if tray.inTray || commands[len(commands)-1] != nimDelete {
		t.Fatalf("restore: tray=%t, Shell commands=%v", tray.inTray, commands)
	}

	procSendMessage.Call(hwnd, wmClose, 0, 0)
	checkVisible(false)
	if tray.taskbarCreated == 0 {
		t.Fatal("TaskbarCreated message is unavailable")
	}
	commands = nil
	procSendMessage.Call(hwnd, tray.taskbarCreated, 0, 0)
	if !tray.inTray || !slices.Equal(commands, []uintptr{nimDelete, nimAdd, nimSetVersion}) {
		t.Fatalf("Explorer restart: tray=%t, Shell commands=%v", tray.inTray, commands)
	}

	// WebView2 posts WM_CLOSE on cancellation (e.g. an elevation restart).
	cancel()
	procSendMessage.Call(hwnd, wmClose, 0, 0)
	if live, _, _ := procIsWindow.Call(hwnd); live != 0 || tray.inTray {
		t.Fatal("shutdown left a native window or tray icon")
	}
}

func TestWindowTrayClosesWithoutAnIcon(t *testing.T) {
	for name, enabled := range map[string]bool{"disabled": false, "shell unavailable": true} {
		t.Run(name, func(t *testing.T) {
			hwnd := newTrayTestWindow(t)
			calls := 0
			tray, err := attachWindowTray(t.Context(), hwnd, func() bool { return enabled }, func(uintptr, *notificationIcon) bool {
				calls++
				return false
			})
			if err != nil {
				t.Fatal(err)
			}
			defer tray.detach()

			procSendMessage.Call(hwnd, wmClose, 0, 0)
			if live, _, _ := procIsWindow.Call(hwnd); live != 0 || tray.inTray {
				t.Fatal("close left an inaccessible window")
			}
			if (calls > 0) != enabled {
				t.Fatalf("Shell calls = %d with preference %t", calls, enabled)
			}
		})
	}
}

func TestWindowTrayExitDestroysWindow(t *testing.T) {
	hwnd := newTrayTestWindow(t)
	tray, err := attachWindowTray(t.Context(), hwnd, func() bool { return true }, func(uintptr, *notificationIcon) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	defer tray.detach()

	procSendMessage.Call(hwnd, wmClose, 0, 0)
	tray.menuCommand(trayExit)
	if live, _, _ := procIsWindow.Call(hwnd); live != 0 || tray.inTray {
		t.Fatal("tray exit left a native window or tray icon")
	}
}

func TestWindowTrayRegistersWithExplorer(t *testing.T) {
	class, _ := windows.UTF16PtrFromString("Shell_TrayWnd")
	if hwnd, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(class)), 0); hwnd == 0 {
		t.Skip("Windows Explorer notification area is not available on this desktop")
	}
	hwnd := newTrayTestWindow(t)
	tray, err := attachWindowTray(t.Context(), hwnd, func() bool { return true }, notifyTrayIcon)
	if err != nil {
		t.Fatal(err)
	}
	defer tray.detach()

	procSendMessage.Call(hwnd, wmClose, 0, 0)
	if !tray.inTray {
		t.Fatal("Explorer refused the notification icon")
	}
	procSendMessage.Call(hwnd, trayEvent, 0, trayIconID<<16|ninKeySelect)
	if visible, _, _ := procIsWindowVisible.Call(hwnd); visible == 0 || tray.inTray {
		t.Fatal("notification-area activation did not restore the window")
	}
}
