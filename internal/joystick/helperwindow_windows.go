//go:build windows

package joystick

import (
	"fmt"
	"syscall"
	"unsafe"
)

// DirectInput's SetCooperativeLevel needs a top-level window handle. We make
// our own message-only window rather than reaching into Wails for the native
// handle: it keeps this package independent of the UI toolkit, and it keeps
// working when the main window is hidden to the tray -- which matters,
// because close-to-tray is a shipped feature.
//
// This mirrors what SDL itself does (SDL_HelperWindow) for the same reason.

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procRegisterClss = user32.NewProc("RegisterClassExW")
	procCreateWindow = user32.NewProc("CreateWindowExW")
	procDestroyWin   = user32.NewProc("DestroyWindow")
	procDefWindowRaw = user32.NewProc("DefWindowProcW")
	procGetModuleHnd = kernel32.NewProc("GetModuleHandleW")
)

// hwndMessage parents a message-only window: never visible, never in the
// z-order, not enumerated as a top-level window.
const hwndMessage = ^uintptr(2) // (HWND)-3

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type helperWindow struct {
	hwnd syscall.Handle
	inst syscall.Handle
}

func newHelperWindow() (*helperWindow, error) {
	name, err := syscall.UTF16PtrFromString("VCSJoystickHelper")
	if err != nil {
		return nil, err
	}
	inst, _, _ := procGetModuleHnd.Call(0)

	wc := wndClassExW{
		lpfnWndProc:   procDefWindowRaw.Addr(),
		hInstance:     syscall.Handle(inst),
		lpszClassName: name,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	// A duplicate class registration is not an error for us: the process may
	// have created a helper window before (a Source rebuilt after an error).
	procRegisterClss.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, callErr := procCreateWindow.Call(
		0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)),
		0, 0, 0, 0, 0,
		hwndMessage, 0, inst, 0,
	)
	if hwnd == 0 {
		return nil, fmt.Errorf("joystick: create helper window: %w", callErr)
	}
	return &helperWindow{hwnd: syscall.Handle(hwnd), inst: syscall.Handle(inst)}, nil
}

func (h *helperWindow) Close() {
	if h == nil || h.hwnd == 0 {
		return
	}
	procDestroyWin.Call(uintptr(h.hwnd))
	h.hwnd = 0
}
