//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// The message box, for the failures that happen before there is a window.
//
// There is no non-Windows build of this app: osops.New exists only on
// Windows, because every operation it performs is a Win32 one. This file is
// therefore the only implementation, not one of a pair.
var (
	user32ForBox   = windows.NewLazySystemDLL("user32.dll")
	procMessageBox = user32ForBox.NewProc("MessageBoxW")
)

const (
	mbOK            = 0x00000000
	mbIconError     = 0x00000010
	mbSetForeground = 0x00010000
)

// messageBox shows a modal error dialog and returns once it is dismissed.
//
// It exists for the one failure with no other channel: if wails.Run does not
// return successfully there is no window to put a message in, and a GUI
// binary has no console for println to reach. Without this the app would
// exit silently, which is indistinguishable from "nothing happened".
func messageBox(title, text string) {
	t, err := windows.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	x, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	procMessageBox.Call(0,
		uintptr(unsafe.Pointer(x)),
		uintptr(unsafe.Pointer(t)),
		uintptr(mbOK|mbIconError|mbSetForeground))
}
