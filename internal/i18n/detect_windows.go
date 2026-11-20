//go:build windows

package i18n

import "golang.org/x/sys/windows"

var (
	kernel32                     = windows.NewLazySystemDLL("kernel32.dll")
	procGetUserDefaultUILanguage = kernel32.NewProc("GetUserDefaultUILanguage")
)

// Detect reads the OS UI language. The panel is Windows-only, so this is the
// voice of the person who just launched it - a Russian Windows gets Russian,
// anything else gets English. It runs once, before the window exists, and the
// picker on the More screen overrules it from then on.
func Detect() string {
	lang, _, _ := procGetUserDefaultUILanguage.Call()
	// LANG_RUSSIAN = 0x19; the low byte of the LCID is the primary language.
	if lang&0xFF == 0x19 {
		return Ru
	}
	return En
}
