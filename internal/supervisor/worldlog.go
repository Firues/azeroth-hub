package supervisor

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"wow-hub/internal/i18n"
)

// ShutdownKind is what a finished stop actually did.
type ShutdownKind string

const (
	// ShutdownClean: the main loop ran its save countdown and the database
	// pools closed. This is what a manual Ctrl+C produces.
	ShutdownClean ShutdownKind = "clean"
	// ShutdownPartial: the pools closed but the countdown never ran, so the
	// online-player save pass did not happen. Measured on the real server -
	// see docs/superpowers/spikes/2026-09-29-ctrl-break.md.
	ShutdownPartial ShutdownKind = "partial"
	// ShutdownUnknown: the log says nothing either way. Never treat this as
	// success: it is also what a log we failed to read looks like.
	ShutdownUnknown ShutdownKind = "unknown"
)

var (
	poolClosedRe = regexp.MustCompile(`^All connections on DatabasePool '.*' closed\.$`)
	haltRe       = regexp.MustCompile(`^Halting process\.\.\.$`)
	// The main loop counting down to save online players. Its absence is
	// what separates a real shutdown from an interrupted one - the pools
	// close either way.
	saveCountdownRe = regexp.MustCompile(`^Server shutdown in \d+ second\(s\)\.$`)
)

// noteWorldLog records where the worldserver's log was before a stop.
//
// The offset matters: logs\ holds one Server_*.log per run, so an earlier
// run's "Server shutdown in 3 second(s)" would otherwise make this stop look
// clean.
type shutdownNote struct {
	path string
	size int64
}

// noteWorldLog captures the newest Server_*.log and its current length.
func (s *Supervisor) noteWorldLog() shutdownNote {
	path := s.newestServerLogPath()
	if path == "" {
		return shutdownNote{}
	}
	size := int64(0)
	if st, err := os.Stat(path); err == nil {
		size = st.Size()
	}
	return shutdownNote{path: path, size: size}
}

// gradeShutdown judges a worldserver log from the given byte offset.
//
// An offset of 0 reads the whole log, which is what a post-hoc check wants:
// it asks whether the countdown appeared anywhere in this run, not only after
// some later point.
func gradeShutdown(path string, fromOffset int64) ShutdownKind {
	if path == "" {
		return ShutdownUnknown
	}

	f, err := os.Open(path)
	if err != nil {
		return ShutdownUnknown
	}
	defer f.Close()

	if fromOffset > 0 {
		if _, err := f.Seek(fromOffset, 0); err != nil {
			return ShutdownUnknown
		}
	}

	data := make([]byte, 4<<20)
	n, _ := f.Read(data)
	text := strings.ReplaceAll(string(data[:n]), "\r\n", "\n")

	var pools, countdown, halt bool
	for _, line := range strings.Split(text, "\n") {
		switch {
		case poolClosedRe.MatchString(line):
			pools = true
		case saveCountdownRe.MatchString(line):
			countdown = true
		case haltRe.MatchString(line):
			halt = true
		}
	}

	switch {
	case pools && countdown && halt:
		return ShutdownClean
	case pools:
		return ShutdownPartial
	default:
		return ShutdownUnknown
	}
}

// classifyWorldShutdown reads what the worldserver appended after the note
// was taken and judges the shutdown.
//
// It reads the log file rather than the console pipe: worldserver stops
// echoing to the console before it shuts down, so "Halting process..." and
// the pool closures exist only in the file. Judging from the console
// reported a completed shutdown as a failure.
func (s *Supervisor) classifyWorldShutdown(note shutdownNote) ShutdownKind {
	return gradeShutdown(note.path, note.size)
}

// newestServerLogPath returns the newest Server_*.log.
//
// It picks the lexicographically LAST name, not the newest mtime. The names
// embed a zero-padded timestamp (Server_2026-09-29_11_00_00.log), so
// lexical order is chronological order - and unlike mtime it is exact.
// Comparing mtimes ties whenever two files land in the same clock tick,
// which on this machine happens often enough that the classifier
// intermittently read the previous run's log and judged the current stop by
// it. That is precisely the mistake the read offset exists to prevent.
func (s *Supervisor) newestServerLogPath() string {
	if s.lay.LogsDir == "" {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(s.lay.LogsDir, "Server_*.log"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	// filepath.Glob returns sorted results.
	return matches[len(matches)-1]
}

// PartialShutdownWarning says what is actually at stake after a partial stop.
//
// Naming the countdown and the save interval matters more than sounding
// alarming: the user has to know that up to PlayerSaveInterval of character
// progress may not have been written, and that Ctrl+C in the console is the
// thing that would have saved it.
func PartialShutdownWarning(kind ShutdownKind) string {
	switch kind {
	case ShutdownPartial:
		return i18n.T("worldlog.partial")
	default:
		return i18n.T("worldlog.unconfirmed")
	}
}
