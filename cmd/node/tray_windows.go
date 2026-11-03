//go:build windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	_ "embed"

	"golang.org/x/sys/windows"
)

//go:embed icon.ico
var iconICO []byte

const (
	idOpen     = 1
	idSettings = 2
	idQuit     = 3

	wmTray         = 0x8001
	nimAdd         = 0x00000000
	nimModify      = 0x00000001
	nimDelete      = 0x00000002
	nifMessage     = 0x00000001
	nifIcon        = 0x00000002
	nifTip         = 0x00000004
	nifInfo        = 0x00000010
	niifInfo       = 0x00000001
	imageIcon      = 1
	lrLoadFromFile = 0x00000010
	lrDefaultSize  = 0x00000040
	mfString       = 0x00000000
	mfSeparator    = 0x00000800
	tpmBottomAlign = 0x0020
	tpmRightAlign  = 0x0008
	wmCommand      = 0x0111
	wmDestroy      = 0x0002
	wmLButtonUp    = 0x0202
	wmRButtonUp    = 0x0205
)

var (
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	shellNotifyIcon       = shell32.NewProc("Shell_NotifyIconW")
	createPopupMenu       = user32.NewProc("CreatePopupMenu")
	appendMenu            = user32.NewProc("AppendMenuW")
	trackPopupMenu        = user32.NewProc("TrackPopupMenu")
	destroyMenu           = user32.NewProc("DestroyMenu")
	createWindowEx        = user32.NewProc("CreateWindowExW")
	defWindowProc         = user32.NewProc("DefWindowProcW")
	dispatchMessage       = user32.NewProc("DispatchMessageW")
	getCursorPos          = user32.NewProc("GetCursorPos")
	getMessage            = user32.NewProc("GetMessageW")
	loadImage             = user32.NewProc("LoadImageW")
	postQuitMessage       = user32.NewProc("PostQuitMessage")
	registerClassEx       = user32.NewProc("RegisterClassExW")
	setForegroundWindow   = user32.NewProc("SetForegroundWindow")
	showWindow            = user32.NewProc("ShowWindow")
	translateMessage      = user32.NewProc("TranslateMessage")
	getModuleHandle          = kernel32.NewProc("GetModuleHandleW")
	getConsoleWindow         = kernel32.NewProc("GetConsoleWindow")
	getConsoleProcessList    = kernel32.NewProc("GetConsoleProcessList")
	getUserDefaultUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")
)

var (
	trayUIURL  string
	trayCancel context.CancelFunc
	trayNID    *notifyIconData
	trayHWND   windows.Handle
	wndProcCB  = syscall.NewCallback(trayWndProc)
)

type wndClassEx struct {
	Size, Style                        uint32
	WndProc                            uintptr
	ClsExtra, WndExtra                 int32
	Instance, Icon, Cursor, Background windows.Handle
	MenuName, ClassName                *uint16
	IconSm                             windows.Handle
}

// notifyIconData matches NOTIFYICONDATAW including balloon fields (Vista+).
type notifyIconData struct {
	Size                       uint32
	Wnd                        windows.Handle
	ID, Flags, CallbackMessage uint32
	Icon                       windows.Handle
	Tip                        [128]uint16
	State, StateMask           uint32
	Info                       [256]uint16
	Timeout                    uint32
	InfoTitle                  [64]uint16
	InfoFlags                  uint32
}

type point struct {
	X, Y int32
}

type msg struct {
	HWND    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

func setAppUserModelID(id string) {
	p, err := windows.UTF16PtrFromString(id)
	if err != nil {
		return
	}
	proc := shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
	_, _, _ = proc.Call(uintptr(unsafe.Pointer(p)))
}

func runTray(ctx context.Context, cancel context.CancelFunc, uiURL string) error {
	setAppUserModelID("AgentMesh.Node")
	trayUIURL = uiURL
	trayCancel = cancel
	className, err := windows.UTF16PtrFromString("AgentMeshNodeTray")
	if err != nil {
		return err
	}
	hInst, _, _ := getModuleHandle.Call(0)
	wc := wndClassEx{
		WndProc:   wndProcCB,
		Instance:  windows.Handle(hInst),
		ClassName: className,
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := registerClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return err
	}
	hwnd, _, err := createWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		0,
		0,
		0, 0, 0, 0,
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		return err
	}
	trayHWND = windows.Handle(hwnd)
	hIcon := loadTrayIcon(windows.Handle(hInst))
	nid := &notifyIconData{
		Wnd:             trayHWND,
		ID:              1,
		Flags:           nifMessage | nifIcon | nifTip,
		CallbackMessage: wmTray,
		Icon:            hIcon,
	}
	nid.Size = uint32(unsafe.Sizeof(*nid))
	tip, _ := windows.UTF16FromString("Agent Mesh")
	copy(nid.Tip[:], tip)
	if r, _, err := shellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(nid))); r == 0 {
		return err
	}
	trayNID = nid
	showTrayBalloon(nid)
	go func() {
		<-ctx.Done()
		postQuitMessage.Call(0)
	}()
	var m msg
	for {
		r, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&m)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
	if trayNID != nil {
		shellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(trayNID)))
	}
	return nil
}

func trayWndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmTray:
		switch lParam {
		case wmLButtonUp:
			// 左键打开界面，不退出。
			openUI(trayUIURL)
		case wmRButtonUp:
			showTrayMenu(windows.Handle(hwnd))
		}
		return 0
	case wmCommand:
		switch wParam {
		case idOpen:
			openUI(trayUIURL)
		case idSettings:
			openUIAt(trayUIURL, traySettingsPath)
		case idQuit:
			if trayCancel != nil {
				trayCancel()
			}
			postQuitMessage.Call(0)
		}
		return 0
	case wmDestroy:
		postQuitMessage.Call(0)
		return 0
	}
	r, _, _ := defWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return r
}

func trayChineseUI() bool {
	r, _, _ := getUserDefaultUILanguage.Call()
	// LANGID primary language is the low 10 bits; LANG_CHINESE == 0x04.
	return uint16(r)&0x3ff == 0x04
}

func showTrayBalloon(nid *notifyIconData) {
	if nid == nil {
		return
	}
	title, body := trayStartBalloon(trayChineseUI())
	titleU, _ := windows.UTF16FromString(title)
	bodyU, _ := windows.UTF16FromString(body)
	copy(nid.InfoTitle[:], titleU)
	copy(nid.Info[:], bodyU)
	nid.InfoFlags = niifInfo
	nid.Timeout = 8000
	nid.Flags = nifMessage | nifIcon | nifTip | nifInfo
	shellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(nid)))
	nid.Flags = nifMessage | nifIcon | nifTip
}

func showTrayMenu(hwnd windows.Handle) {
	hMenu, _, _ := createPopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer destroyMenu.Call(hMenu)
	openLabel, settingsLabel, quitLabel := trayMenuLabels(trayChineseUI())
	openText, _ := windows.UTF16PtrFromString(openLabel)
	settingsText, _ := windows.UTF16PtrFromString(settingsLabel)
	quitText, _ := windows.UTF16PtrFromString(quitLabel)
	appendMenu.Call(hMenu, mfString, idOpen, uintptr(unsafe.Pointer(openText)))
	appendMenu.Call(hMenu, mfString, idSettings, uintptr(unsafe.Pointer(settingsText)))
	appendMenu.Call(hMenu, mfSeparator, 0, 0)
	appendMenu.Call(hMenu, mfString, idQuit, uintptr(unsafe.Pointer(quitText)))
	setForegroundWindow.Call(uintptr(hwnd))
	var pt point
	getCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	trackPopupMenu.Call(hMenu, tpmBottomAlign|tpmRightAlign, uintptr(pt.X), uintptr(pt.Y), 0, uintptr(hwnd), 0)
}

func loadTrayIcon(hInst windows.Handle) windows.Handle {
	name, _ := windows.UTF16PtrFromString("APPICON")
	h, _, _ := loadImage.Call(uintptr(hInst), uintptr(unsafe.Pointer(name)), imageIcon, 0, 0, lrDefaultSize)
	if h != 0 {
		return windows.Handle(h)
	}
	path := filepath.Join(os.TempDir(), "agent-mesh-node.ico")
	if err := os.WriteFile(path, iconICO, 0o644); err == nil {
		p, _ := windows.UTF16PtrFromString(path)
		h, _, _ = loadImage.Call(0, uintptr(unsafe.Pointer(p)), imageIcon, 0, 0, lrLoadFromFile|lrDefaultSize)
		if h != 0 {
			return windows.Handle(h)
		}
	}
	h, _, _ = loadImage.Call(0, 32512, imageIcon, 0, 0, lrDefaultSize) // IDI_APPLICATION
	return windows.Handle(h)
}
