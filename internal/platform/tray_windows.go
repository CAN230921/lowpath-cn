package platform

import (
	"context"
	"errors"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	trayIconID = 1
	trayEvent  = 0x8001 // WM_APP + 1 (WebView2 uses WM_APP)

	nimAdd        = 0
	nimDelete     = 2
	nimSetFocus   = 3
	nimSetVersion = 4

	wmClose       = 0x0010
	wmContextMenu = 0x007b
	wmNCDestroy   = 0x0082
	wmSysCommand  = 0x0112
	scMinimize    = 0xf020
	ninSelect     = 0x0400
	ninKeySelect  = 0x0401
	wmLButtonUp   = 0x0202

	swHide = 0
	swShow = 5

	trayOpen = 1
	trayExit = 2
)

var (
	comctl32                      = windows.NewLazySystemDLL("comctl32.dll")
	shell32                       = windows.NewLazySystemDLL("shell32.dll")
	procSetWindowSubclass         = comctl32.NewProc("SetWindowSubclass")
	procRemoveWindowSubclass      = comctl32.NewProc("RemoveWindowSubclass")
	procDefSubclassProc           = comctl32.NewProc("DefSubclassProc")
	procShellNotifyIcon           = shell32.NewProc("Shell_NotifyIconW")
	procRegisterWindowMessage     = user32.NewProc("RegisterWindowMessageW")
	procChangeWindowMessageFilter = user32.NewProc("ChangeWindowMessageFilterEx")
	procLoadIcon                  = user32.NewProc("LoadIconW")
	procCreatePopupMenu           = user32.NewProc("CreatePopupMenu")
	procDestroyMenu               = user32.NewProc("DestroyMenu")
	procAppendMenu                = user32.NewProc("AppendMenuW")
	procSetMenuDefaultItem        = user32.NewProc("SetMenuDefaultItem")
	procTrackPopupMenuEx          = user32.NewProc("TrackPopupMenuEx")
	procPostMessage               = user32.NewProc("PostMessageW")
	procDestroyWindow             = user32.NewProc("DestroyWindow")

	trayWindows  sync.Map // native window handle -> *windowTray
	traySubclass uintptr
)

func init() { traySubclass = windows.NewCallback(trayWindowProc) }

// notificationIcon matches Win32 NOTIFYICONDATAW, including the unused
// balloon fields needed for the Shell to accept the full structure.
type notificationIcon struct {
	size                uint32
	hwnd                uintptr
	id, flags, callback uint32
	icon                uintptr
	tip                 [128]uint16
	state, stateMask    uint32
	info                [256]uint16
	version             uint32
	infoTitle           [64]uint16
	infoFlags           uint32
	guid                windows.GUID
	balloonIcon         uintptr
}

// windowTray and its subclass run on the window's OS thread.
type windowTray struct {
	ctx            context.Context
	enabled        func() bool
	notify         func(uintptr, *notificationIcon) bool
	data           notificationIcon
	taskbarCreated uintptr
	inTray         bool
}

func attachWindowTray(ctx context.Context, hwnd uintptr, enabled func() bool, notify func(uintptr, *notificationIcon) bool) (*windowTray, error) {
	var module windows.Handle
	_ = windows.GetModuleHandleEx(windows.GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT, nil, &module)
	icon, _, _ := procLoadIcon.Call(uintptr(module), iconID)
	if icon == 0 {
		icon, _, _ = procLoadIcon.Call(0, 32512) // IDI_APPLICATION
	}
	if icon == 0 {
		return nil, errors.New("could not load the system tray icon")
	}
	message, _ := windows.UTF16PtrFromString("TaskbarCreated")
	taskbarCreated, _, _ := procRegisterWindowMessage.Call(uintptr(unsafe.Pointer(message)))
	t := &windowTray{ctx: ctx, enabled: enabled, notify: notify, taskbarCreated: taskbarCreated}
	t.data = notificationIcon{
		size: uint32(unsafe.Sizeof(t.data)), hwnd: hwnd, id: trayIconID, icon: icon,
		flags: 0x87, callback: trayEvent, version: 4, // NIF_MESSAGE | ICON | TIP | SHOWTIP
	}
	tip, _ := windows.UTF16FromString("Lowpath CN")
	copy(t.data.tip[:], tip)
	trayWindows.Store(hwnd, t)
	if ok, _, _ := procSetWindowSubclass.Call(hwnd, traySubclass, 1, 0); ok == 0 {
		trayWindows.Delete(hwnd)
		return nil, errors.New("could not subclass the desktop window for the system tray")
	}
	// Explorer runs at medium integrity, including when Lowpath was started
	// as administrator for WinDivert. Permit only the tray's two messages.
	procChangeWindowMessageFilter.Call(hwnd, trayEvent, 1, 0) // MSGFLT_ALLOW
	if taskbarCreated != 0 {
		procChangeWindowMessageFilter.Call(hwnd, taskbarCreated, 1, 0)
	}
	return t, nil
}

func notifyTrayIcon(command uintptr, data *notificationIcon) bool {
	ok, _, _ := procShellNotifyIcon.Call(command, uintptr(unsafe.Pointer(data)))
	return ok != 0
}

func (t *windowTray) addIcon() bool {
	if !t.notify(nimAdd, &t.data) {
		return false
	}
	if !t.notify(nimSetVersion, &t.data) {
		t.notify(nimDelete, &t.data)
		return false
	}
	t.inTray = true
	return true
}

func (t *windowTray) removeIcon() {
	if t.inTray {
		t.notify(nimDelete, &t.data)
		t.inTray = false
	}
}

func (t *windowTray) restore() {
	t.removeIcon()
	procShowWindow.Call(t.data.hwnd, swShow)
	procSetForegroundWindow.Call(t.data.hwnd)
}

func (t *windowTray) detach() {
	if _, attached := trayWindows.LoadAndDelete(t.data.hwnd); attached {
		t.removeIcon()
		procRemoveWindowSubclass.Call(t.data.hwnd, traySubclass, 1)
	}
}

func trayWindowProc(hwnd, msg, wParam, lParam, _, _ uintptr) uintptr {
	if entry, ok := trayWindows.Load(hwnd); ok && entry.(*windowTray).handle(msg, wParam, lParam) {
		return 0
	}
	result, _, _ := procDefSubclassProc.Call(hwnd, msg, wParam, lParam)
	return result
}

func (t *windowTray) handle(msg, wParam, lParam uintptr) bool {
	switch {
	case msg == wmNCDestroy:
		t.detach()
	case msg == wmClose || msg == wmSysCommand && wParam&0xfff0 == scMinimize:
		if t.ctx.Err() != nil || !t.enabled() {
			break
		}
		if t.inTray || t.addIcon() {
			procShowWindow.Call(t.data.hwnd, swHide)
			return true
		}
	case msg == t.taskbarCreated && t.taskbarCreated != 0:
		if t.inTray {
			t.removeIcon()
			if !t.addIcon() {
				t.restore()
			}
		}
	case msg == trayEvent:
		if t.inTray && t.ctx.Err() == nil && uint16(lParam>>16) == trayIconID {
			switch uint16(lParam) {
			case ninSelect, ninKeySelect, wmLButtonUp:
				t.restore()
			case wmContextMenu:
				t.showMenu(wParam)
			}
		}
		return true
	}
	return false
}

func (t *windowTray) showMenu(position uintptr) {
	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	open, _ := windows.UTF16PtrFromString("打开 Lowpath")
	exit, _ := windows.UTF16PtrFromString("退出")
	procAppendMenu.Call(menu, 0, trayOpen, uintptr(unsafe.Pointer(open)))
	procAppendMenu.Call(menu, 0, trayExit, uintptr(unsafe.Pointer(exit)))
	procSetMenuDefaultItem.Call(menu, trayOpen, 0)

	hwnd := t.data.hwnd
	procSetForegroundWindow.Call(hwnd)
	x, y := int16(position), int16(position>>16)
	// TPM_RIGHTBUTTON | TPM_NONOTIFY | TPM_RETURNCMD
	command, _, _ := procTrackPopupMenuEx.Call(menu, 0x182, uintptr(x), uintptr(y), hwnd, 0)
	procPostMessage.Call(hwnd, 0, 0, 0) // WM_NULL lets the menu dismiss normally next time
	t.menuCommand(command)
}

func (t *windowTray) menuCommand(command uintptr) {
	switch command {
	case trayOpen:
		t.restore()
	case trayExit:
		// DestroyWindow goes straight to teardown. WM_CLOSE would put the
		// window back in the tray while the preference is still enabled.
		procDestroyWindow.Call(t.data.hwnd)
	default:
		t.notify(nimSetFocus, &t.data)
	}
}
