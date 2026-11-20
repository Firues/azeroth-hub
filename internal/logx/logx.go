// Package logx is the application's own log.
//
// It exists because a user reporting a problem should not have to open
// three console windows and screenshot them. Everything the app knows -
// which process it started, what it decided, why an operation failed - is
// written here, into one file, in the repack's own logs directory.
//
// One rule above all: this package never panics and never returns an
// error to its caller for the sake of logging. A logger that takes the
// app down at the exact moment a server is misbehaving destroys the
// evidence along with the session.
package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"wow-hub/internal/i18n"
)

// LogFileName is fixed on purpose. The integration check in Task 11 greps
// logs\wow-hub.log for mysqld's "Shutdown complete", so rotating by date
// would make that check depend on the day of the month.
const LogFileName = "wow-hub.log"

// timestampFormat is local time: the person reading this file is sitting
// next to the machine it describes.
const timestampFormat = "2006-01-02 15:04:05"

// Logger writes timestamped lines to a file.
//
// Safe for concurrent use: the pipe reader goroutines of three child
// processes will all be logging through the same instance.
type Logger struct {
	mu   sync.Mutex
	file *os.File
}

// New opens dir/LogFileName for appending, creating dir if needed.
func New(dir string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.Tf("logx.mkdir", dir), err)
	}

	path := filepath.Join(dir, LogFileName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.Tf("logx.open", path), err)
	}
	return &Logger{file: f}, nil
}

func (l *Logger) Info(msg string, kv ...any) { l.write("INFO", msg, kv) }

func (l *Logger) Error(msg string, kv ...any) { l.write("ERROR", msg, kv) }

// write emits one line. Every failure here is swallowed on purpose: see
// the package comment. A logger that cannot log must not become the
// reason the app dies.
func (l *Logger) write(level, msg string, kv []any) {
	if l == nil || l.file == nil {
		return
	}

	line := fmt.Sprintf("%s %s %s%s\n",
		time.Now().Format(timestampFormat),
		level,
		msg,
		formatPairs(kv),
	)

	l.mu.Lock()
	defer l.mu.Unlock()
	l.file.WriteString(line)
}

// formatPairs renders key/value pairs as " k=v k=v". A trailing key with
// no value is kept as-is rather than dropped: losing it would hide a
// caller's mistake, and showing it costs nothing.
func formatPairs(kv []any) string {
	if len(kv) == 0 {
		return ""
	}

	var b strings.Builder
	for i := 0; i < len(kv); i += 2 {
		b.WriteByte(' ')
		if i+1 < len(kv) {
			fmt.Fprintf(&b, "%v=%v", kv[i], kv[i+1])
		} else {
			fmt.Fprintf(&b, "%v", kv[i])
		}
	}
	return b.String()
}

// Close releases the file. Calling it twice is harmless, and so is
// logging afterwards - the line goes nowhere rather than crashing.
func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
