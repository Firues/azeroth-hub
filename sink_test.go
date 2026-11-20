package main

import (
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"wow-hub/internal/notify"
)

// A minimised panel has no window. The status loop keeps running, and every
// event it produces has nowhere to go - which must be a dropped event, not a
// panic and not a blocked goroutine. A supervisor waiting on a force-kill
// answer would hang forever if Emit blocked here.
func TestEventSinkDropsEventsWhileNoWindowExists(t *testing.T) {
	s := newEventSink(nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.Emit(notify.Event{Type: notify.EventStatus, Data: 1})
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Emit blocked with no window")
	}
}

// Swapping the window must be safe from both ends: the status loop emits on
// its own goroutine while the tray builds a window on the UI thread. A race
// here would be a crash in the middle of restoring the panel.
func TestEventSinkSetWindowDoesNotRaceWithEmit(t *testing.T) {
	s := newEventSink(nil)

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				s.Emit(notify.Event{Type: notify.EventStatus, Data: 1})
			}
		}
	}()

	for i := 0; i < 200; i++ {
		s.setWindow(nil)
	}
	close(stop)
	<-done
}

// Eight clicks in quick succession, no window open: one panel, not eight.
//
// The tray icon is the way back to the panel, and a person reaching for it
// clicks twice as often as not. Two windows is not a cosmetic problem: the sink
// holds one window, so the older one keeps sitting on screen while every event
// goes past it - a panel that looks alive and updates nothing, with no way to
// close it that the user can reach.
func TestShowPanelBuildsOneWindowForSeveralClicks(t *testing.T) {
	s := newEventSink(nil)

	present := func(*application.WebviewWindow) {}

	var built []*application.WebviewWindow
	var buildMu sync.Mutex
	build := func() *application.WebviewWindow {
		buildMu.Lock()
		defer buildMu.Unlock()
		w := new(application.WebviewWindow)
		built = append(built, w)
		return w
	}

	const clicks = 8
	var wg sync.WaitGroup
	for i := 0; i < clicks; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.showPanel(build, present)
		}()
	}
	wg.Wait()

	if len(built) != 1 {
		t.Fatalf("clicks built %d windows, want 1", len(built))
	}
	if s.window() != built[0] {
		t.Fatal("sink did not keep the window that was built")
	}
}

// The re-entrant click: a second tray click handled while the first one is
// still building the window.
//
// This is what an impatient double click actually does, and it is not a second
// goroutine - Wails drains the message queue while it brings a webview up
// (pumpUntilInited), so the second click is dispatched from inside the first
// click's own build, on the same thread.
//
// A lock across the build deadlocks here: the second call waits for a lock the
// first one holds, and the first one is waiting for the message pump that the
// second call is sitting in. That deadlock freezes the panel for good - the
// window is created and never shown, and nothing else moves.
//
// So the second click must return at once and build nothing. Its user gets the
// panel the first click is already building.
func TestShowPanelSurvivesAReentrantClickDuringBuild(t *testing.T) {
	s := newEventSink(nil)

	present := func(*application.WebviewWindow) {}

	builds := 0
	var reentered *application.WebviewWindow
	done := make(chan struct{})
	build := func() *application.WebviewWindow {
		builds++
		// The window is coming up and the message pump dispatches the second
		// click. It has to come back out of this without waiting.
		go func() {
			defer close(done)
			reentered = s.showPanel(func() *application.WebviewWindow {
				t.Error("the re-entrant click built a second window")
				return nil
			}, present)
		}()
		<-done
		return new(application.WebviewWindow)
	}

	s.showPanel(build, present)

	if builds != 1 {
		t.Fatalf("built %d windows, want 1", builds)
	}
	if reentered != nil {
		t.Fatalf("re-entrant click got window %p, want nil while one is being built", reentered)
	}
	if s.window() == nil {
		t.Fatal("the window that was built was not kept")
	}
}

// A window closing must forget that window and only that window.
//
// Compare by identity: the panel is rebuilt often, and a close that arrives
// late from an older window must not clear the newer one that replaced it.
func TestForgetClearsOnlyTheWindowThatClosed(t *testing.T) {
	s := newEventSink(nil)

	older := new(application.WebviewWindow)
	s.setWindow(older)
	s.forget(older)
	if s.window() != nil {
		t.Fatal("forget did not clear the window that closed")
	}

	newer := new(application.WebviewWindow)
	s.setWindow(newer)
	s.forget(older)
	if s.window() != newer {
		t.Fatal("a late close from an old window cleared the current one")
	}
}
