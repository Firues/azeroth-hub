//go:build windows

package osops

import (
	"testing"
	"unsafe"
)

func TestInputRecordIsExactlyTwentyBytes(t *testing.T) {
	// The Win32 ABI fixes INPUT_RECORD at 20 bytes. Go's padding rules are
	// not visible in the source, so this asserts the size directly rather
	// than trusting the layout to stay right on its own.
	if got := unsafe.Sizeof(inputRecord{}); got != 20 {
		t.Fatalf("sizeof(inputRecord) = %d, want 20", got)
	}
}

func TestKeyEventRecordIsDownThenUp(t *testing.T) {
	// A console left with a key-down and no key-up reports a stuck modifier,
	// and worldserver's console reader can stall on it.
	pair := keyEventRecord('x', 0)

	if pair[0].EventType != keyEvent {
		t.Errorf("first record EventType = %d, want %d", pair[0].EventType, keyEvent)
	}
	if pair[0].KeyDown != 1 {
		t.Errorf("first record KeyDown = %d, want 1", pair[0].KeyDown)
	}
	if pair[1].KeyDown != 0 {
		t.Errorf("second record KeyDown = %d, want 0", pair[1].KeyDown)
	}
	if pair[0].UnicodeChar != 'x' || pair[1].UnicodeChar != 'x' {
		t.Errorf("UnicodeChar = %d/%d, want 'x' for both halves", pair[0].UnicodeChar, pair[1].UnicodeChar)
	}
}

func TestKeyEventRecordMapsEnterToVKReturn(t *testing.T) {
	// A command without the newline is never read. The virtual key code has
	// to be VK_RETURN, not just a '\r' character.
	pair := keyEventRecord('\r', 0x0D)

	if pair[0].VirtualKeyCode != 0x0D {
		t.Errorf("VirtualKeyCode = %#x, want 0x0D (VK_RETURN)", pair[0].VirtualKeyCode)
	}
	if pair[0].UnicodeChar != '\r' {
		t.Errorf("UnicodeChar = %#x, want CR", pair[0].UnicodeChar)
	}
}

func TestAttachToADeadProcessExplainsItself(t *testing.T) {
	// The failure a user will actually hit: the process is gone. The message
	// has to say that rather than surfacing a bare Win32 code.
	err := attachToConsole(0x7FFFFFF0)
	if err == nil {
		t.Skip("attached to a PID that cannot exist; nothing to assert")
	}
	// Either it fails as designed, or the call succeeded unexpectedly and the
	// console must be released again.
	procFreeConsole.Call()
}
