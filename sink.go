package main

import (
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"wow-hub/internal/notify"
)

// eventSink owns the panel's window and the events that go to it.
//
// Two locks, and the split is deliberate:
//
//   - mu guards the window and the building flag. It is held across nothing
//     slow: Wails brings a webview up by pumping messages, so anything that
//     waits on a lock while inside that pump is waiting on itself.
type eventSink struct {
	mu  sync.RWMutex
	app *application.App
	win *application.WebviewWindow
	// building says a window is being made right now, so that the second of two
	// quick clicks stands aside instead of building a second one. Cleared by
	// showPanel on every way out.
	building bool
}

func newEventSink(a *application.App) *eventSink { return &eventSink{app: a} }

// Emit must never block. The supervisor blocks on this path while waiting for
// the page's answer to a force-kill question, and with no window to answer it
// a blocking Emit would hang it forever.
//
// An event emitted while no window exists is dropped, and that is not a failure
// to work around: there is no page to deliver it to, and the page re-reads
// everything on open - Data, Status, Health, BackupStatus, Schedule - so the
// event carried nothing the new window would not ask for anyway.
//
//wails:ignore
func (s *eventSink) Emit(e notify.Event) {
	s.mu.RLock()
	win := s.win
	s.mu.RUnlock()

	if win == nil {
		return
	}
	win.EmitEvent(e.Type, e.Data)
}

// showPanel returns the panel's window: the current one, shown, or a new one
// built by build. It returns nil when a build is already under way and this
// call had nothing to do but stand aside.
//
// build and present are injected rather than called directly because both touch
// Wails state that exists only while an application is running - Show reads a
// package-level global - and a sink that can only be exercised inside a live
// GUI is a sink whose interesting behaviour goes untested. That is how two
// windows got built in the first place.
//
// There is deliberately NO lock held across build, and that is the whole design
// rather than a simplification. Wails drains the message queue while it brings
// a webview up, so a second tray click dispatched during the build lands here
// re-entrantly, on the thread that is building. A mutex across the build
// deadlocks: the second call waits for a lock the first one holds, and the first
// one is waiting for the pump the second call is sitting in. The panel freezes
// with a created-but-invisible window and nothing recovers.
//
// So the decision is made once, under a short lock, and the slow part happens
// outside it. A caller that arrives while a build is running gets nil and shows
// nothing - which is right, because the build that is already running is going
// to present the panel when it finishes.
//
// build must not itself record the window; that happens here, or two builders
// would both believe they own it.
func (s *eventSink) showPanel(
	build func() *application.WebviewWindow,
	present func(*application.WebviewWindow),
) *application.WebviewWindow {
	s.mu.Lock()
	if s.win != nil {
		w := s.win
		s.mu.Unlock()
		present(w)
		return w
	}
	if s.building {
		s.mu.Unlock()
		return nil
	}
	s.building = true
	s.mu.Unlock()

	// Cleared on every path out, including a panic in build: a sink that
	// believed it was still building would refuse every later click, and the
	// panel could never be opened again.
	defer func() {
		s.mu.Lock()
		s.building = false
		s.mu.Unlock()
	}()

	w := build()
	s.setWindow(w)
	// Shown like any other window. Leaving that to Wails' default is how a
	// freshly built panel ends up created and not visible - a panel nobody can
	// get back, with the sink now refusing to build a second one.
	present(w)
	return w
}

// setWindow records which window is current.
func (s *eventSink) setWindow(w *application.WebviewWindow) {
	s.mu.Lock()
	s.win = w
	s.mu.Unlock()
}

// window is the current window, for callers that only need to look.
func (s *eventSink) window() *application.WebviewWindow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.win
}

// forget clears the window, but only if it is still the current one.
//
// By identity, deliberately. The panel is rebuilt often, and a close that
// arrives late from a window that was already replaced would otherwise clear
// the newer one - which is the same "two panels, one of them deaf" failure one
// step later.
//
// This is what newWindow registers on each window it builds.
func (s *eventSink) forget(w *application.WebviewWindow) {
	s.mu.Lock()
	if s.win == w {
		s.win = nil
	}
	s.mu.Unlock()
}

// clear drops whatever window is current, unconditionally. For shutdown only -
// everywhere else a close must go through forget, which checks identity.
func (s *eventSink) clear() { s.setWindow(nil) }

// attach records a window and arranges to forget it when that window closes.
//
// Forgetting on close is the whole point: without it the sink would keep
// pointing at a dead window, and showPanel would take that dead window for a
// live one and never build a new one - the tray icon would go dead after the
// first minimise.
func (s *eventSink) attach(w *application.WebviewWindow) {
	s.setWindow(w)
	w.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		s.forget(w)
	})
}
