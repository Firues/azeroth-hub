package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wow-hub/internal/osops"
)

// osopsNewFake and newFakeClock are thin wrappers so the fixture above reads
// as one line.
func osopsNewFake() osops.Ops   { return osops.NewFake() }
func newFakeClock() osops.Clock { return osops.NewFakeClock() }

// These cover the step the spike proved is necessary and the plan left out.
//
// Measured on the real server: after CTRL_BREAK this worldserver closes all
// four database pools and exits 0xc0000374, but never runs the main loop's
// "Server shutdown in N second(s)" save countdown. Judging a stop by "the
// process is gone" calls that a success, and the user loses up to
// PlayerSaveInterval (15 minutes) of their character without being told.

func writeServerLog(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

func TestClassifyCleanShutdownAfterTheSaveCountdown(t *testing.T) {
	// The real sequence from logs/Server_2026-09-29_16_54_49.log.
	dir := t.TempDir()
	path := writeServerLog(t, dir, "Server_2026-09-29_16_54_49.log", "starting up\n")

	sup := newLogSup(t, dir)
	note := sup.noteWorldLog()

	appendTo(t, path, "Server shutdown in 3 second(s).\r\n"+
		"Server shutdown in 2 second(s).\r\n"+
		"Server shutdown in 1 second(s).\r\n"+
		"DungeonMaster: Shutdown - 0 sessions active.\r\n"+
		"Halting process...\r\n"+
		"Closing down DatabasePool 'acore_characters'. Waiting for 0 queries to finish...\r\n"+
		"All connections on DatabasePool 'acore_characters' closed.\r\n"+
		"All connections on DatabasePool 'acore_world' closed.\r\n"+
		"All connections on DatabasePool 'acore_auth' closed.\r\n"+
		"All connections on DatabasePool 'acore_playerbots' closed.\r\n")

	if got := sup.classifyWorldShutdown(note); got != ShutdownClean {
		t.Fatalf("classify = %q, want %q", got, ShutdownClean)
	}
}

func TestClassifyInterruptedShutdownAsPartial(t *testing.T) {
	// What the spike actually produced, every time: pools closed, no
	// countdown, heap corruption on the way out. A naive "some pool closed"
	// check calls this a success.
	dir := t.TempDir()
	path := writeServerLog(t, dir, "Server_2026-09-29_19_47_39.log", "starting\n")

	sup := newLogSup(t, dir)
	note := sup.noteWorldLog()

	appendTo(t, path, "DungeonMaster: Shutdown - 0 sessions active.\r\n"+
		"Halting process...\r\n"+
		"All connections on DatabasePool 'acore_characters' closed.\r\n"+
		"All connections on DatabasePool 'acore_world' closed.\r\n"+
		"All connections on DatabasePool 'acore_auth' closed.\r\n")

	if got := sup.classifyWorldShutdown(note); got != ShutdownPartial {
		t.Fatalf("classify = %q, want %q - the save countdown never ran", got, ShutdownPartial)
	}
}

func TestClassifyNoShutdownEvidence(t *testing.T) {
	dir := t.TempDir()
	path := writeServerLog(t, dir, "Server_2026-09-29.log", "starting\n")

	sup := newLogSup(t, dir)
	note := sup.noteWorldLog()

	appendTo(t, path, "nothing useful happened\r\n")

	if got := sup.classifyWorldShutdown(note); got != ShutdownUnknown {
		t.Fatalf("classify = %q, want %q", got, ShutdownUnknown)
	}
}

func TestClassifyIgnoresTextFromBeforeTheStop(t *testing.T) {
	// An earlier run's log still sits in the directory. Without an offset,
	// its "Server shutdown in 3 second(s)" would make the current stop look
	// clean.
	dir := t.TempDir()
	path := writeServerLog(t, dir, "Server_old.log",
		"Server shutdown in 3 second(s).\r\nAll connections on DatabasePool 'acore_auth' closed.\r\n")

	sup := newLogSup(t, dir)
	note := sup.noteWorldLog()

	appendTo(t, path, "Halting process...\r\nAll connections on DatabasePool 'acore_auth' closed.\r\n")

	if got := sup.classifyWorldShutdown(note); got != ShutdownPartial {
		t.Fatalf("classify = %q, want %q - pre-stop text must not count", got, ShutdownPartial)
	}
}

func TestClassifyUsesTheNewestLogFile(t *testing.T) {
	// logs\ holds one Server_*.log per run, and the previous run's file is
	// still sitting there. The note has to point at this run's file, not the
	// older one - otherwise its "Server shutdown in 3 second(s)" makes every
	// subsequent stop look clean.
	//
	// The live log exists before the note is taken, because worldserver
	// creates it at startup, not at shutdown.
	dir := t.TempDir()
	writeServerLog(t, dir, "Server_2026-09-29_10_00_00.log", "old run, clean shutdown\n")

	sup := newLogSup(t, dir)
	live := writeServerLog(t, dir, "Server_2026-09-29_11_00_00.log", "starting up\n")
	note := sup.noteWorldLog()

	appendTo(t, live, "Server shutdown in 1 second(s).\r\nHalting process...\r\n"+
		"All connections on DatabasePool 'acore_auth' closed.\r\n")

	if got := sup.classifyWorldShutdown(note); got != ShutdownClean {
		t.Fatalf("classify = %q, want %q - the newest log is the live one", got, ShutdownClean)
	}
}

func TestClassifyWithoutAnyLogIsUnknown(t *testing.T) {
	// A repack with no logs\ yet must not make the stop path panic.
	sup := newLogSup(t, filepath.Join(t.TempDir(), "does-not-exist"))
	note := sup.noteWorldLog()

	if got := sup.classifyWorldShutdown(note); got != ShutdownUnknown {
		t.Fatalf("classify = %q, want %q when there is no log to read", got, ShutdownUnknown)
	}
}

func TestClassifyTruncatedPoolLineIsNotAClosedPool(t *testing.T) {
	// A process killed mid-write leaves a half-finished line. That must not
	// be mistaken for a completed pool shutdown: the verdict stays unknown,
	// which the app reports as "cannot confirm" rather than as either
	// success or a known-bad stop.
	dir := t.TempDir()
	path := writeServerLog(t, dir, "Server_x.log", "starting\n")

	sup := newLogSup(t, dir)
	note := sup.noteWorldLog()
	appendTo(t, path, "Halting process...\r\nAll connections on DatabasePool 'acore_aut")

	if got := sup.classifyWorldShutdown(note); got != ShutdownUnknown {
		t.Fatalf("classify = %q, want %q - a truncated line proves nothing", got, ShutdownUnknown)
	}
}

func TestPartialShutdownMessageExplainsTheRisk(t *testing.T) {
	// The user needs to know what is actually at stake, not just that
	// something was off. "Server shutdown in N second(s)" is what saves
	// online characters, and PlayerSaveInterval is how stale they can get.
	msg := PartialShutdownWarning(ShutdownPartial)
	if !strings.Contains(msg, "PlayerSaveInterval") {
		t.Errorf("message = %q, want it to name the save interval", msg)
	}
	if !strings.Contains(msg, "Ctrl+C") {
		t.Errorf("message = %q, want it to say what to do about it", msg)
	}
}

func appendTo(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
}

func newLogSup(t *testing.T, logsDir string) *Supervisor {
	t.Helper()
	lay := testLayout()
	lay.LogsDir = logsDir
	return New(osopsNewFake(), newFakeClock(), lay, nil, nil, nil, Conf{})
}
