//go:build !windows

package i18n

// Detect is only reachable in tests and non-Windows builds; the panel is a
// Windows binary, so there is nothing to detect and English is the default.
func Detect() string {
	return En
}
