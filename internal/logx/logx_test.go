package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readLog(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, LogFileName))
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	return string(data)
}

func TestLoggerWritesLine(t *testing.T) {
	dir := t.TempDir()

	l, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	l.Info("hello", "pid", 1)
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	out := readLog(t, dir)
	if !strings.Contains(out, "hello") {
		t.Fatalf("log = %q, want it to contain %q", out, "hello")
	}
	if !strings.Contains(out, "pid=1") {
		t.Fatalf("log = %q, want it to contain %q", out, "pid=1")
	}
}

func TestLoggerSurvivesClosedFile(t *testing.T) {
	// The log is how a user reports a bug without screenshots. A logger
	// that panics on a closed file would take the app down with the server
	// still running - the worst possible time to lose the window.
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	l.Info("after close")
	l.Error("after close", "code", 7)

	if err := l.Close(); err != nil {
		t.Fatalf("second Close: %v, want it to be harmless", err)
	}
}

func TestLoggerErrorMarksTheLevel(t *testing.T) {
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	l.Error("something broke", "pid", 42)
	l.Close()

	out := readLog(t, dir)
	if !strings.Contains(out, "ERROR") {
		t.Fatalf("log = %q, want it marked ERROR", out)
	}
	if !strings.Contains(out, "something broke") {
		t.Fatalf("log = %q, want the message", out)
	}
	if !strings.Contains(out, "pid=42") {
		t.Fatalf("log = %q, want it to contain %q", out, "pid=42")
	}
}

func TestLoggerToleratesDanglingKey(t *testing.T) {
	// Key/value pairs arrive from callers who should know better and do not
	// always. A dangling key is a formatting wart, not a crash.
	dir := t.TempDir()
	l, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	l.Info("message", "lonely")
	l.Close()

	out := readLog(t, dir)
	if !strings.Contains(out, "lonely") {
		t.Fatalf("log = %q, want the dangling key preserved", out)
	}
}

func TestLoggerCreatesDirectory(t *testing.T) {
	// The app locates logs/ relative to the repack root; on a fresh unpack
	// it may not exist yet, and failing to start the logger over that would
	// mean no logs at all.
	dir := filepath.Join(t.TempDir(), "logs")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("precondition: %s already exists", dir)
	}

	l, err := New(dir)
	if err != nil {
		t.Fatalf("New on a missing directory: %v", err)
	}
	l.Info("created")
	l.Close()

	if !strings.Contains(readLog(t, dir), "created") {
		t.Fatal("log file was not created in the new directory")
	}
}
