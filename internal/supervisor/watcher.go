package supervisor

import (
	"io"
	"strings"
	"sync"

	"wow-hub/internal/logx"
)

// worldReadyMarker is the worldserver's initialization line.
//
// It is NOT the readiness signal. Measured on this repack, around 140 lines
// of module initialization follow it, and signalling into that window
// produces a shutdown that closes every database pool and exits with heap
// corruption while never running the main loop's save countdown. The world
// socket is what the start sequence waits for; see
// docs/superpowers/spikes/2026-09-29-ctrl-break.md.
//
// The line is still matched, because it is the best progress signal the UI
// can show while the port is closed.
const worldReadyMarker = "WORLD: World Initialized In"

// markerWatcher reads a process's output looking for one line, keeping the
// text so it can also be logged.
//
// It matches per completed line rather than per Write call: a pipe splits
// output at arbitrary byte boundaries, and matching per call would miss the
// marker about half the time.
type markerWatcher struct {
	mu      sync.Mutex
	marker  string
	partial string
	lines   int
	reached bool
	buffer  []string
	tap     io.Writer
}

func newMarkerWatcher(marker string) *markerWatcher {
	return &markerWatcher{marker: marker}
}

// newMarkerWatcherWith also forwards everything to out, which is where the
// app's own log gets its copy.
func newMarkerWatcherWith(marker string, out io.Writer) *markerWatcher {
	return &markerWatcher{marker: marker, tap: out}
}

func (w *markerWatcher) Write(p []byte) (int, error) {
	if w.tap != nil {
		// A failing tap must not become an error here: it would propagate
		// into the child's pipe and kill the process being supervised.
		w.tap.Write(p)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.partial += string(p)
	for {
		i := strings.IndexByte(w.partial, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(w.partial[:i], "\r")
		w.lines++
		w.buffer = append(w.buffer, line)
		if len(w.buffer) > 400 {
			w.buffer = w.buffer[len(w.buffer)-400:]
		}
		if !w.reached && strings.Contains(line, w.marker) {
			w.reached = true
		}
		w.partial = w.partial[i+1:]
	}
	return len(p), nil
}

func (w *markerWatcher) Reached() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.reached
}

func (w *markerWatcher) Lines() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lines
}

func (w *markerWatcher) Buffer() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.buffer, "\n")
}

// logTap forwards a process's output to the app's log, one line at a time.
//
// It exists so that everything the three servers print reaches
// logs\wow-hub.log. Without it, diagnosing a stopped server means opening
// three console windows, which is the thing this app is meant to replace.
type logTap struct {
	mu      sync.Mutex
	log     *logx.Logger
	prefix  string
	partial string
}

func newLogTap(l *logx.Logger, prefix string) *logTap {
	return &logTap{log: l, prefix: prefix}
}

func (t *logTap) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.partial += string(p)
	for {
		i := strings.IndexByte(t.partial, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(t.partial[:i], "\r")
		t.partial = t.partial[i+1:]
		if t.log != nil {
			t.log.Info(line, "svc", t.prefix)
		}
	}

	// Always (len(p), nil). A non-nil error here would reach the child's
	// stdout pipe and could kill the process the app is supervising - and a
	// full disk or a closed log file must never do that.
	return len(p), nil
}
