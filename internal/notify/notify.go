package notify

import "sync"

// Event is something the backend tells the frontend about.
type Event struct {
	// Type is one of "status", "health", "data", "operation", "backup",
	// "confirm-kill" or "confirm-cancel".
	Type string
	Data any
}

// Event types. The frontend switches on these, so they are constants rather
// than strings sprinkled through two packages.
const (
	EventStatus      = "status"
	EventHealth      = "health"
	EventOperation   = "operation"
	EventConfirmKill = "confirm-kill"
	// EventConfirmCancel tells the page to close the force-kill dialog
	// without an answer. The backend gives up on a dialog nobody answers
	// after Conf.ConfirmTimeout; without this the dialog would stay on
	// screen and a later click on it would do nothing at all.
	EventConfirmCancel = "confirm-cancel"
	// EventData carries the read-only snapshot: the rollback points under
	// backups\ and the size of each database. Pushed rather than fetched so
	// three screens can share one read.
	EventData = "data"
	// EventBackup carries the state of a rollback point being made: which
	// step is running, how far it has got, and how the last attempt ended.
	//
	// Pushed because the work takes minutes. The four database dumps are
	// 105 seconds and 385 MB, so a binding that returned when they were
	// done would hold the window for two minutes with nothing on screen -
	// and the same state is what the screen paints on load, so one type
	// covers both the progress and the result.
	EventBackup = "backup"
	// EventSchedule carries the rollback-point schedule: whether the
	// reminder is on, the interval, and when the next point falls due.
	//
	// Pushed rather than only fetched because a point made by the panel
	// moves the due date, and an automatic point moves it while the user is
	// looking at another screen. Without this the schedule line would keep
	// saying "пора" after the point it was asking for already existed.
	EventSchedule = "schedule"
)

// Operation levels, for EventOperation.Data. The page maps them onto the
// same three styles it uses for its own messages.
const (
	LevelOK    = "ok"
	LevelWarn  = "warn"
	LevelError = "error"
)

// Sink receives events destined for the UI.
//
// The supervisor does not import Wails. Task 8 supplies an implementation
// that turns Emit into a browser event; tests use Recorder.
type Sink interface {
	Emit(Event)
}

// Recorder collects events for assertions.
type Recorder struct {
	mu     sync.Mutex
	events []Event
}

var _ Sink = (*Recorder)(nil)

// NewRecorder returns an empty Recorder.
func NewRecorder() *Recorder { return &Recorder{} }

func (r *Recorder) Emit(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

// Events returns a copy of everything emitted so far.
func (r *Recorder) Events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, len(r.events))
	copy(out, r.events)
	return out
}

// CountOf returns how many events of the given type were emitted.
func (r *Recorder) CountOf(eventType string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e.Type == eventType {
			n++
		}
	}
	return n
}

// Reset drops everything, for a test that cares only about what follows.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = nil
}
