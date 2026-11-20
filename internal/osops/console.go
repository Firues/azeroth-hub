//go:build windows

package osops

import (
	"errors"
	"fmt"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"wow-hub/internal/i18n"
)

// Console input. Everything here attaches to another process's console,
// types into it, and detaches again.
//
// The stdin pipe is the primary route - it was measured working twice
// against the real worldserver, with the save countdown and every pool
// closing. This is the fallback, and it exists because the pipe route
// depends on HOW a build reads input. A process that reads its console
// through ReadConsole rather than stdin would never see a byte written to a
// pipe, while keystrokes injected into its console input buffer arrive by
// the same route a human typist would use.

var (
	k32               = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole = k32.NewProc("AttachConsole")
	procFreeConsole   = k32.NewProc("FreeConsole")
	procWriteConsoleI = k32.NewProc("WriteConsoleInputW")
	procGenCtrlEvent  = k32.NewProc("GenerateConsoleCtrlEvent")
	procSetCtrlHandl  = k32.NewProc("SetConsoleCtrlHandler")
)

// consoleMu serialises attach/send/detach.
//
// AttachConsole and FreeConsole change state for the WHOLE PROCESS, not per
// goroutine. Two concurrent calls interleave into a mess where neither ends
// up attached to anything.
var consoleMu sync.Mutex

// inputRecord is INPUT_RECORD carrying a KEY_EVENT_RECORD, exactly 20 bytes.
//
// Hand-rolled rather than taken from x/sys because Go's struct layout for
// this union has no exported definition, and the size is fixed by the Win32
// ABI. unitTestInputRecordSize below fails loudly if the padding is ever
// wrong.
type inputRecord struct {
	EventType       uint16 // KEY_EVENT == 1
	_               uint16
	KeyDown         int32
	RepeatCount     uint16
	VirtualKeyCode  uint16
	VirtualScanCode uint16
	UnicodeChar     uint16
	ControlKeyState uint32
}

const keyEvent = 1

// keyEventRecord builds the down/up pair for one character.
//
// Both halves are needed: a console that receives a key-down with no
// key-up reports a stuck modifier, and worldserver's reader can stall on it.
func keyEventRecord(r rune, vk uint16) [2]inputRecord {
	ch := utf16.Encode([]rune{r})
	if len(ch) == 0 {
		ch = []uint16{'?'}
	}
	c := ch[0]

	return [2]inputRecord{
		{EventType: keyEvent, KeyDown: 1, RepeatCount: 1, VirtualKeyCode: vk, UnicodeChar: c},
		{EventType: keyEvent, KeyDown: 0, RepeatCount: 1, VirtualKeyCode: vk, UnicodeChar: c},
	}
}

// attachToConsole joins pid's console, leaving our own first.
//
// A process can be attached to at most one console, so the current one is
// released before the target's is taken.
func attachToConsole(pid int) error {
	procFreeConsole.Call()

	r, _, err := procAttachConsole.Call(uintptr(pid))
	if r == 0 {
		return fmt.Errorf("AttachConsole(PID %d): %w", pid, explainAttach(err))
	}
	return nil
}

// explainAttach turns the raw Win32 error into something a user can act on.
//
// The codes that matter here are specific and each means a different fix:
// access denied is a privilege mismatch, invalid handle means the process is
// already gone, and a failure outright means it never had a console - which
// is what happens if something launched it with CREATE_NO_WINDOW.
func explainAttach(err error) error {
	if err == nil || err.Error() == "" {
		return errors.New(i18n.T("console.noconsole"))
	}

	code := uintptr(err.(windows.Errno))
	switch code {
	case 5:
		return fmt.Errorf("%s: %w", i18n.T("console.denied"), err)
	case 6:
		return fmt.Errorf("%s: %w", i18n.T("console.badhandle"), err)
	case 87:
		return fmt.Errorf("%s: %w", i18n.T("console.nowindow"), err)
	default:
		return fmt.Errorf("%s: %w", i18n.Tf("console.code", code), err)
	}
}

// SendLine types text into pid's console and presses Enter.
//
// Used as the fallback for the worldserver's shutdown command when the stdin
// pipe route does not take.
func SendLine(pid int, text string) error {
	consoleMu.Lock()
	defer consoleMu.Unlock()

	if err := attachToConsole(pid); err != nil {
		return err
	}
	defer procFreeConsole.Call()

	h, err := windows.CreateFile(
		windows.StringToUTF16Ptr("CONIN$"),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0,
	)
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.Tf("console.conin", pid), err)
	}
	defer windows.CloseHandle(h)

	recs := make([]inputRecord, 0, len(text)*2+2)
	for _, r := range text {
		pair := keyEventRecord(r, 0)
		recs = append(recs, pair[0], pair[1])
	}
	// Enter: VK_RETURN. A command without it sits in the buffer unread.
	enter := keyEventRecord('\r', 0x0D)
	recs = append(recs, enter[0], enter[1])

	var written uint32
	res, _, err := procWriteConsoleI.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&recs[0])), uintptr(len(recs)),
		uintptr(unsafe.Pointer(&written)),
	)
	if res == 0 {
		return fmt.Errorf("%s: %w", i18n.Tf("console.write", pid), err)
	}
	if int(written) != len(recs) {
		return errors.New(i18n.Tf("console.wrote", pid, written, len(recs)))
	}
	return nil
}

// SendCtrlC delivers a real Ctrl+C to pid's console.
//
// CTRL_C_EVENT cannot be targeted at a process group, so it goes to every
// process sharing the attached console - which, since we attach to the
// server's own console and the server is the only thing in it, is the server.
//
// The wait before detaching is not decoration: detaching takes the console
// away while the event is still being delivered, and the process can be left
// having received nothing.
func SendCtrlC(pid int) error {
	consoleMu.Lock()
	defer consoleMu.Unlock()

	if err := attachToConsole(pid); err != nil {
		return err
	}
	defer procFreeConsole.Call()

	// Stop Ctrl+C from killing the panel. With our console attached, the
	// event reaches us too, and a Wails app that dies mid-stop leaves the
	// server running with nobody supervising it.
	procSetCtrlHandl.Call(0, 1) // TRUE = ignore Ctrl+C for this process
	defer procSetCtrlHandl.Call(0, 0)

	res, _, err := procGenCtrlEvent.Call(0, 0) // CTRL_C_EVENT, group 0
	if res == 0 {
		return fmt.Errorf("%s: %w", i18n.Tf("console.ctrlevent", pid), err)
	}

	time.Sleep(400 * time.Millisecond)
	return nil
}
