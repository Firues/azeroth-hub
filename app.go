package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"wow-hub/internal/backups"
	"wow-hub/internal/dumps"
	"wow-hub/internal/health"
	"wow-hub/internal/i18n"
	"wow-hub/internal/logx"
	"wow-hub/internal/notify"
	"wow-hub/internal/osops"
	"wow-hub/internal/prune"
	"wow-hub/internal/realmdb"
	"wow-hub/internal/repack"
	"wow-hub/internal/restore"
	"wow-hub/internal/settings"
	"wow-hub/internal/supervisor"
)

// healthEvery is the tick on which health runs. The status loop ticks once a
// second; the database is pinged once every five.
//
// Pinging is not free, and once per five seconds is well inside what a person
// can notice. Every second would be ten pings per visible change.
const healthEvery = 5

// dataRefreshEvery is the tick on which the read-only snapshot is retaken
// even when nothing appears to have changed.
//
// Slow on purpose. A read costs a walk of backups\ and a query against
// information_schema, and the numbers change only when someone plays or
// someone backs up - both of which the MySQL state change already catches.
// What this timer is for is everything a state change cannot announce: a
// snapshot folder copied in by hand, a file deleted in Explorer. A minute
// is soon enough to notice, and the query stays warm in between.
const dataRefreshEvery = 60

// supervisor.Confirmer is implemented by confirmer, not by App.
//
// An exported method on a bound struct is a binding, and Wails marshals the
// result to JSON when JavaScript calls it. A channel cannot be marshalled, so
// an exported AskConfirm on App is a binding that fails if it is ever invoked.
// The frontend has no reason to invoke it - it answers through
// RespondConfirm - which is exactly what makes the trap quiet.
//
// confirmer keeps the Go-side interface satisfied without putting the channel
// within reach of the page.
type confirmer struct{ a *App }

func (c confirmer) AskConfirm(service string, pid int) <-chan bool {
	return c.a.askConfirm(service, pid)
}

var _ supervisor.Confirmer = confirmer{}

// Deps is what the window needs from outside.
//
// A struct rather than six positional arguments. The supervisor and the App
// already have to be built in a cycle - the supervisor asks the App before
// force-killing, and the App needs the supervisor to do anything - so one
// field is filled in after construction regardless, and a named field says
// which one.
type Deps struct {
	Supervisor *supervisor.Supervisor
	Health     *health.Runner
	Sink       notify.Sink
	// Ops and Layout serve the read-only data screens: the rollback points
	// under backups\, and the disk-space figure that decides whether the
	// next one fits.
	Ops    osops.Ops
	Layout repack.Layout
	// DB is the read-only pool for the realm queries. Nil is normal and
	// expected - it is nil whenever temp\mysql_ac.conf is missing or
	// unreadable - and the data screens then say why instead of showing
	// zeros that look like a measurement.
	DB *sql.DB
	// Clock is time, made injectable for the same reason the supervisor
	// takes one: it names the folder a rollback point goes into, and a
	// test that cannot fix the clock cannot assert the name.
	Clock osops.Clock
	// Logger is the app's own log. Nil is normal and every use is guarded:
	// a missing log must not stop a backup.
	Logger *logx.Logger
}

// DataSnapshot is everything the data screens show without being asked: the
// rollback points on disk, and how big the databases are.
//
// Pushed rather than fetched, because none of it changes often. The
// character list is deliberately not in here: it is an order of magnitude
// larger and only one screen wants it, so that screen asks.
type DataSnapshot struct {
	Backups backups.Inventory
	Realm   realmdb.Realm
	// Loaded says whether a read has ever finished.
	//
	// Without it the page cannot tell "no rollback points" from "not read
	// yet", and those mean opposite things - the same reason Inventory.Err
	// exists instead of an empty list. The page reads the cached snapshot
	// on load to paint immediately, so it must know which one it is holding.
	Loaded bool
	// ReadAt is when the snapshot was taken, for the page to show its age.
	ReadAt time.Time
}

// App is what the frontend talks to.
//
// Every method here is reachable from the UI at any moment, including while
// nothing is running and while a dialog is open. Nothing may panic: a panic
// in the backend takes the whole window down with the server still up, which
// is the worst possible moment to lose the control panel.
type App struct {
	sup    *supervisor.Supervisor
	health *health.Runner
	sink   notify.Sink
	ops    osops.Ops
	lay    repack.Layout
	db     *sql.DB
	clock  osops.Clock
	log    *logx.Logger
	// dumps makes rollback points. Nil when there is no Ops, which only
	// happens in a test fixture that never presses the button.
	dumps *dumps.Runner
	// restore puts a physical point back. It writes over mysql\data, so it
	// is a separate object from dumps even though both act on backups\:
	// creation is safe to repeat, restoration is not, and one struct for
	// both would make that difference invisible at the call site.
	restore *restore.Runner
	// safety makes the snapshot a restore takes of the state it is about to
	// replace. A second runner rather than a second mode of dumps, because
	// its progress must not claim the screen: a physical snapshot reports
	// itself as a physical backup, and during a restore that would rename the
	// operation on screen and hide the restore's own result behind it.
	safety *dumps.Runner
	// ticks counts the status loop, so health can run on its own cadence
	// without a second goroutine.
	ticks int

	// pending is the single outstanding confirmation dialog. Stopping is
	// sequential, so at most one can be open; a nil value simply means the
	// frontend called RespondConfirm with nothing to answer.
	mu      sync.Mutex
	pending chan bool

	// data is the last read-only snapshot, and the guard around the one
	// refresh that may be running. Three callers want it - the slow loop, a
	// MySQL state change and the page - and none should have to wait for
	// the others.
	dataMu   sync.Mutex
	dataBusy bool
	// dataAgain remembers a refresh asked for while one was already
	// running, so the last request is honoured instead of dropped.
	dataAgain bool
	data      DataSnapshot
	// lastMySQL is the state seen on the previous tick, so a change can
	// trigger a refresh without waiting for the slow timer.
	lastMySQL supervisor.State

	// backup is the state of the rollback-point buttons: the one running
	// now, or the last one that ran. Guarded by backupMu because the
	// runner reports progress from its own goroutine while the page reads
	// it from Wails'.
	backupMu sync.Mutex
	backup   BackupStatus
	// backupCancel stops the running backup. Nil when none is running.
	backupCancel context.CancelFunc
	// backupStarted is when the running backup began.
	//
	// Held here rather than read from the runner's report, because a
	// failed or cancelled run returns no duration - and "0.0s" next to a
	// failure that took half a minute reads as a bug in the panel.
	backupStarted time.Time

	// settings is the persisted state: the rollback-point schedule, and
	// when a point was last made.
	//
	// Guarded because the status loop reads it once a second to decide
	// whether anything is due, while the page writes it.
	settingsMu sync.Mutex
	settings   settings.Settings
	// settingsErr is why the settings file could not be read, if it could
	// not. Reported on the screen rather than swallowed: a schedule that
	// silently forgot itself is worse than one that says it did.
	settingsErr string
}

// NewApp wires the backend behind the window.
func NewApp(d Deps) *App {
	a := &App{
		sup:    d.Supervisor,
		health: d.Health,
		sink:   d.Sink,
		ops:    d.Ops,
		lay:    d.Layout,
		db:     d.DB,
		clock:  d.Clock,
		log:    d.Logger,
	}
	if a.clock == nil {
		a.clock = osops.RealClock{}
	}
	// Built here rather than handed in, because the runner's progress sink
	// is a method on this App and the App is what owns the state the page
	// reads. Without an Ops there is nothing to run a dump with, and a
	// fixture that only drives the status loop should get a clear refusal
	// rather than a panic on the first click.
	if a.ops != nil {
		a.dumps = dumps.New(a.ops, a.lay, a.clock, a.backupProgress)
		// No progress sink: the restore already has its own step counter, and
		// a second set of reports would overwrite its step with a database
		// name or an archive name - the panel would say "acore_world.sql"
		// in the middle of a rollback.
		a.safety = dumps.New(a.ops, a.lay, a.clock, nil)
		a.restore = restore.New(a.ops, a.lay, a.clock, a.restoreProgress)
	}

	// Read once, at construction, so the first tick already knows the
	// schedule. A failure is kept rather than logged away: the page has a
	// place to say "настройки не прочитались", and a schedule that quietly
	// reverted to its defaults is the kind of thing a person discovers only
	// by missing a week of reminders.
	a.settings = settings.Default()
	if s, err := settings.Load(a.lay.SettingsFile); err != nil {
		a.settingsErr = err.Error()
		if a.log != nil {
			a.log.Error("настройки не прочитались", "err", err)
		}
	} else {
		a.settings = s
	}

	// The language is the first thing to settle: every string the backend
	// builds from here on - checklist, health details, errors - reads it. A
	// saved choice wins; a fresh install detects one from the OS.
	i18n.Init(a.settings.Language)

	return a
}

// setSupervisor attaches the supervisor after construction.
//
// Unexported on purpose. Wails binds every EXPORTED method to the frontend,
// and SetSupervisor must not be callable from JavaScript: the frontend would
// have to hand it a supervisor, and the only thing it could manage is a
// zero-valued one that breaks the app. main.go is in this same package, so
// unexported costs nothing.
//
// The cycle this breaks is real: the supervisor needs an App to ask before
// force-killing, and the App needs a supervisor to do anything.
func (a *App) setSupervisor(sup *supervisor.Supervisor) {
	a.sup = sup
}

// Status returns the Server Control table.
func (a *App) Status() supervisor.Snapshot {
	return a.sup.Status()
}

// Health evaluates the six checks on demand.
//
// No context parameter, on purpose. Wails generates a positional argument
// for a bound context.Context - the TypeScript binding literally reads
// "Health(arg1: context.Context)" and asks JavaScript for a value that
// cannot come from there. Whatever the page passes gets marshalled to JSON
// and unmarshalled into the context, and the call dies with "json: cannot
// unmarshal object into Go value of type context.Context". A JS AbortSignal
// was the only thing the frontend had to hand it.
//
// runHealth carries the context for the tick loop, which has a real one to
// cancel on shutdown.
func (a *App) Health() []health.Check {
	return a.runHealth(context.Background())
}

func (a *App) runHealth(ctx context.Context) []health.Check {
	if a.health == nil {
		return nil
	}
	return a.health.Run(ctx)
}

// tick is the status loop: once a second, forever.
//
// Unexported for the same reason as setSupervisor: the frontend must not be
// able to drive the loop's rate. Status refreshes itself and emits only on a
// real change, and health folds in every fifth tick.
//
// Status refreshes itself and emits only on a real change, so this is cheap
// to call often. Health is folded into it rather than run on its own
// goroutine, because one loop is one thing to reason about.
func (a *App) tick(ctx context.Context) {
	a.sup.Refresh()

	// The scheduler rides the status loop rather than owning a timer of its
	// own. It has nothing to do that the loop does not already know: the
	// loop has just refreshed the service states, which is the other half
	// of the decision, and a second goroutine would be a second thing to
	// stop on shutdown.
	a.checkSchedule()

	a.ticks++
	if a.ticks%healthEvery == 0 {
		checks := a.runHealth(ctx)
		if a.sink != nil {
			a.sink.Emit(notify.Event{Type: notify.EventHealth, Data: checks})
		}
	}

	// The data screens refresh on a MySQL state change as well as on the
	// slow timer, because that change is what makes the numbers wrong:
	// while MySQL is down there are none to read, and the moment it is up
	// the snapshot on screen is stale. Waiting for the timer would leave
	// "нет подключения к базе" up for up to a minute after the database
	// came back, which reads as a bug in the panel.
	//
	// The first tick always fires: lastMySQL starts empty, which no real
	// state equals, so the window fills in a second after it opens instead
	// of a minute.
	if st := stateOf(a.sup.Status(), supervisor.SvcMySQL); st != a.lastMySQL {
		a.lastMySQL = st
		go a.refreshData(ctx)
		return
	}
	if a.ticks%dataRefreshEvery == 0 {
		go a.refreshData(ctx)
	}
}

// Data returns the last snapshot without touching the disk or the database.
//
// The page calls this on load so a warm cache paints at once, and reads
// Loaded to tell "nothing here" from "not read yet".
func (a *App) Data() DataSnapshot {
	a.dataMu.Lock()
	defer a.dataMu.Unlock()
	return a.data
}

// RefreshData retakes the snapshot in the background and pushes it as a
// "data" event.
//
// Asynchronous, and it returns nothing, so there is nothing for the page to
// wait on - the answer arrives as the event. That is not a preference: the
// size query is a tenth of a second warm and ten seconds cold, and a
// binding that blocks for ten seconds reads as a frozen window.
func (a *App) RefreshData() {
	go a.refreshData(context.Background())
}

// Characters returns the character list, highest level first.
//
// Asked for rather than pushed. Two thousand rows dwarf everything else in
// the snapshot, and only one screen wants them, so putting them on the
// timer would re-send them every minute for nothing. A limit of zero takes
// the package default.
func (a *App) Characters(limit int) realmdb.Characters {
	return realmdb.List(context.Background(), a.db, limit)
}

// Accounts returns the account list, most recently played first.
//
// Read-only in the strongest sense: the query does not select the salt, the
// verifier, the session key or the TOTP secret, so there is nothing here to
// leak into a screenshot. Asked for rather than pushed, for the same reason
// as the character list.
func (a *App) Accounts(limit int) realmdb.Accounts {
	return realmdb.ListAccounts(context.Background(), a.db, limit)
}

// refreshData retakes the snapshot and pushes it.
//
// One read at a time. Three triggers want it - the slow timer, a MySQL state
// change and the page - and a second concurrent read would load a database
// that may already be busy while producing nothing the first read will not
// also produce. A trigger that arrives mid-read is remembered and honoured
// when that read finishes, so the last request is never silently dropped.
func (a *App) refreshData(ctx context.Context) {
	a.dataMu.Lock()
	if a.dataBusy {
		a.dataAgain = true
		a.dataMu.Unlock()
		return
	}
	a.dataBusy = true
	a.dataMu.Unlock()

	for {
		snap := a.readData(ctx)

		a.dataMu.Lock()
		a.data = snap
		again := a.dataAgain
		a.dataAgain = false
		if !again {
			a.dataBusy = false
		}
		a.dataMu.Unlock()

		if a.sink != nil {
			a.sink.Emit(notify.Event{Type: notify.EventData, Data: snap})
		}
		if !again {
			return
		}
	}
}

// readData is the read itself: the only place in this file that touches the
// filesystem or the database.
func (a *App) readData(ctx context.Context) DataSnapshot {
	inv := backups.Inventory{Dir: a.lay.BackupsDir}
	if a.lay.BackupsDir == "" {
		// No repack above the executable. Said plainly rather than handed
		// to os.ReadDir, whose complaint about an empty path reads as a
		// bug in the panel rather than as "the app was moved".
		inv.Err = i18n.T("app.backup.norepack")
	} else {
		inv = backups.Scan(a.ops, a.lay.BackupsDir)
	}

	return DataSnapshot{
		Backups: inv,
		Realm:   realmdb.Read(ctx, a.db),
		Loaded:  true,
		ReadAt:  time.Now(),
	}
}

// stateOf reads one service's state out of a snapshot.
//
// A service missing from the snapshot counts as stopped, not as running: a
// table that lost a row must not make the data screens claim the database is
// up.
func stateOf(snap supervisor.Snapshot, id supervisor.ServiceID) supervisor.State {
	for _, sv := range snap.Services {
		if sv.ID == id {
			return sv.State
		}
	}
	return supervisor.StateStopped
}

// BackupStatus is the state of the rollback-point buttons.
//
// One struct for what is happening and for what happened, because the
// screen paints the same panel either way: while a point is being made it
// shows progress, and afterwards it shows how the last attempt ended. Two
// types would make the page decide which of them it is holding, and the
// moment it guessed wrong it would show a stale result as a live one.
type BackupStatus struct {
	// Running says a point is being made right now.
	Running bool
	// Kind is "logical" or "physical". Empty before anything has run.
	Kind string
	// Step is the schema being dumped, or the archive name.
	Step string
	// Index and Total place Step in the job, 1-based. Total is set before
	// the first report so the bar has a denominator to draw against.
	Index int
	Total int
	// Bytes is how much the current step has written.
	Bytes int64
	// ElapsedSec is how long the attempt has been running. Seconds rather
	// than a time.Duration, which would reach the page as a number of
	// nanoseconds and have to be divided there.
	ElapsedSec float64
	// Dir is the folder the last successful point went to.
	Dir string
	// SavedBytes is how big that point is.
	SavedBytes int64
	// Databases are the dumps in it, for a logical point.
	Databases []string
	// Err is why the last attempt failed. Empty when it did not.
	Err string
	// Warnings is output mysqldump produced while still exiting 0.
	Warnings []string
	// Point is the rollback point a restore went back to. Empty after a
	// backup, and after a restore that failed.
	Point string
	// At is when the last attempt finished.
	At time.Time
}

// BackupStatus returns the state without touching the disk or the database.
//
// The page calls this on load, so a window opened while a point is being
// made - or reopened after one finished - shows it without waiting for the
// next event.
func (a *App) BackupStatus() BackupStatus {
	a.backupMu.Lock()
	defer a.backupMu.Unlock()
	return a.backup
}

// CreateBackup starts a rollback point and returns at once.
//
// Asynchronous for the same reason RefreshData is, only more so: a full
// logical point is 105 seconds and 385 MB, and a binding that returned when
// it was done would hold the window for two minutes with nothing on screen.
// Progress arrives as "backup" events.
//
// The returned error is only ever about something knowable immediately: an
// unknown kind, no repack to write into, or a backup already running. What
// goes wrong during the work arrives as an event, because by then the page
// has long since stopped waiting for this call.
func (a *App) CreateBackup(kind string) error {
	k, err := parseKind(kind)
	if err != nil {
		return err
	}
	if a.dumps == nil {
		return errors.New(i18n.T("app.backup.unavailable"))
	}

	a.backupMu.Lock()
	if a.backup.Running {
		a.backupMu.Unlock()
		return errors.New(i18n.T("app.backup.busy"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.backupCancel = cancel
	a.backupStarted = time.Now()
	a.backup = BackupStatus{
		Running: true,
		Kind:    string(k),
		Total:   stepsOf(k),
		At:      time.Now(),
	}
	snap := a.backup
	a.backupMu.Unlock()

	if a.log != nil {
		a.log.Info("создание точки отката", "kind", k)
	}
	a.emitBackup(snap)

	go a.runBackup(ctx, k)
	return nil
}

// RestoreSnapshot puts a physical rollback point back and returns at once.
//
// The same split as CreateBackup: the returned error is only about what is
// knowable before any work happens - a name that is not a folder of backups\,
// an archive that is missing or empty, a restore already running - and
// everything else arrives as a "backup" event, because a full stop plus
// snapshot plus unpack is about a minute with the window at its best.
//
// The stack is left stopped. See the restore package for why that is the
// user's decision to make and not the panel's.
func (a *App) RestoreSnapshot(name string) error {
	if a.restore == nil {
		return errors.New(i18n.T("app.restore.norepack"))
	}
	if err := a.restore.Check(name); err != nil {
		return err
	}

	a.backupMu.Lock()
	if a.backup.Running {
		a.backupMu.Unlock()
		return errors.New(i18n.T("app.ops.busy"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.backupCancel = cancel
	a.backupStarted = time.Now()
	a.backup = BackupStatus{
		Running: true,
		Kind:    restore.Kind,
		Total:   restore.Steps,
		Step:    i18n.T("restore.step.stop"),
		At:      time.Now(),
	}
	snap := a.backup
	a.backupMu.Unlock()

	if a.log != nil {
		a.log.Info("восстановление из точки отката", "point", name)
	}
	a.emitBackup(snap)

	go a.runRestore(ctx, name)
	return nil
}

// runRestore is the work, on its own goroutine.
func (a *App) runRestore(ctx context.Context, name string) {
	rep, err := a.restore.Run(ctx, name, a.sup, safetySnapshot{a.safety})
	a.finishRestore(name, rep, err)
}

// safetySnapshot adapts the dumps runner to what restore needs.
//
// The runner's own Report is a backup-shaped struct - it has a Kind and a
// list of databases - and a restore wants two numbers. Rather than teach the
// runner about restores, this takes what it returns and drops the rest.
type safetySnapshot struct{ runner *dumps.Runner }

func (s safetySnapshot) Physical(ctx context.Context) (string, int64, error) {
	if s.runner == nil {
		return "", 0, errors.New(i18n.T("app.safety.nomysqldump"))
	}
	rep, err := s.runner.Physical(ctx)
	return rep.Dir, rep.Bytes, err
}

// restoreProgress is the restore's progress sink, which writes into the same
// state CreateBackup uses.
//
// One struct on the page, because the screen already knows how to draw a
// running operation and a finished one; a second type would mean the page
// holding two of them and deciding which it has.
func (a *App) restoreProgress(p restore.Progress) {
	a.backupMu.Lock()
	a.backup.Step = p.Step
	a.backup.Index = p.Index
	a.backup.Total = p.Total
	a.backup.Bytes = p.Bytes
	a.backup.ElapsedSec = p.Elapsed.Seconds()
	snap := a.backup
	a.backupMu.Unlock()

	a.emitBackup(snap)
}

// finishRestore records the outcome and tells the page.
func (a *App) finishRestore(name string, rep restore.Report, err error) {
	a.backupMu.Lock()
	started := a.backupStarted
	a.backup.Running = false
	a.backup.At = time.Now()
	a.backup.ElapsedSec = time.Since(started).Seconds()
	a.backup.Step = ""
	a.backupCancel = nil
	if err != nil {
		a.backup.Err = err.Error()
		a.backup.Dir = ""
		a.backup.SavedBytes = 0
		a.backup.Index = 0
	} else {
		a.backup.Err = ""
		// The safety copy is what the page names on success: it is the
		// folder that appeared, and the only one a person needs to know
		// about to undo the restore they just did.
		a.backup.Dir = rep.SafetyDir
		a.backup.SavedBytes = rep.SafetyBytes
		a.backup.Point = name
		a.backup.Index = a.backup.Total
	}
	snap := a.backup
	a.backupMu.Unlock()

	if a.log != nil {
		if err != nil {
			a.log.Error("восстановление не выполнено", "point", name, "err", err)
		} else {
			a.log.Info("восстановление выполнено", "point", name, "safety", rep.SafetyDir)
		}
	}
	a.emitBackup(snap)

	// The safety copy is a new folder under backups\, so the list is stale
	// either way - a restore that failed partway through may have made one
	// too.
	go a.refreshData(context.Background())
}

// CancelBackup stops a point that is being made.
//
// A no-op when none is. The button stays reachable for a moment after the
// work finished, and refusing there would answer a click with an error
// about a problem that does not exist.
func (a *App) CancelBackup() {
	a.backupMu.Lock()
	cancel := a.backupCancel
	// A restore is not cancellable: it is already past the point where the
	// stack is down and the archive is being unpacked over mysql\data, and a
	// half-unpacked database directory is worse than a restore that runs to
	// its end. The safety copy exists for exactly that case.
	if a.backup.Kind == restore.Kind {
		cancel = nil
	}
	a.backupMu.Unlock()

	if cancel == nil {
		return
	}
	if a.log != nil {
		a.log.Info("отмена создания точки отката")
	}
	cancel()
}

/* ------------------------------ удаление -------------------------------- */

// DeleteBackup removes one rollback point and answers at once.
//
// The shortest operation on backups\ and the only one with no undo, which is
// why the page asks before calling it: the dialog names the folder and says
// outright that there is no way back.
//
// Not one of the operations that take the progress bar. There is nothing in a
// deletion to watch, and a running operation greys out the create and restore
// buttons over an act that does not need the server stopped. What it does need
// is that no copy is being made right now - the folder exists from the first
// byte written into it, so a delete during one would take a copy being made.
func (a *App) DeleteBackup(name string) error {
	a.backupMu.Lock()
	running := a.backup.Running
	a.backupMu.Unlock()

	if running {
		return errors.New(i18n.T("app.ops.busy"))
	}

	if err := prune.Remove(a.lay, name); err != nil {
		if a.log != nil {
			a.log.Error("точка отката не удалена", "point", name, "err", err)
		}
		return err
	}
	if a.log != nil {
		a.log.Info("точка отката удалена", "point", name)
	}

	// The row that was just deleted is still on screen, and the checklist on
	// "Сервер" judges freshness by the newest folder that is there.
	go a.refreshData(context.Background())
	return nil
}

/* ------------------------------- расписание ------------------------------ */

// ScheduleState is the rollback-point schedule as the page needs it.
//
// The stored settings plus the two things that are computed from them and the
// clock. Computed on the backend rather than in the page because the page has
// no clock the backend agrees with, and because "is it due" is the same
// question the status loop asks - one implementation, one answer.
type ScheduleState struct {
	Enabled   bool
	EveryDays int
	AutoRun   bool

	// Due says a point is wanted now.
	Due bool
	// NextAt is when the next one falls due. Zero when the schedule is off.
	NextAt time.Time
	// DaysLeft is how long until then, rounded up. Negative is not sent:
	// "due" is what a past date means.
	DaysLeft int
	// LastRun is when a point was last made through the panel. Zero means
	// never.
	LastRun time.Time

	// Err is why the settings file could not be read. Empty when it could.
	Err string
}

// Schedule returns the schedule without touching the disk.
func (a *App) Schedule() ScheduleState {
	a.settingsMu.Lock()
	s := a.settings
	errMsg := a.settingsErr
	a.settingsMu.Unlock()

	now := a.clock.Now()
	due, next := s.Backup.Due(now)

	out := ScheduleState{
		Enabled:   s.Backup.Enabled,
		EveryDays: s.Backup.EveryDays,
		AutoRun:   s.Backup.AutoRun,
		Due:       due,
		LastRun:   s.Backup.LastRun,
		Err:       errMsg,
	}

	if !next.IsZero() && next.After(now) {
		out.NextAt = next
		// Rounded up, so "due in 6 hours" on a one-day interval does not
		// read as "0 days" - which would look like "due now" next to a
		// schedule that says it is not.
		out.DaysLeft = int((next.Sub(now) + 24*time.Hour - 1) / (24 * time.Hour))
	}
	return out
}

// SetSchedule stores the schedule.
//
// The interval is clamped by the settings package rather than here, so a
// value that arrives from a hand-edited file and a value that arrives from
// the page go through the same rule.
func (a *App) SetSchedule(enabled bool, everyDays int, autoRun bool) error {
	a.settingsMu.Lock()
	a.settings.Backup.Enabled = enabled
	// Clamped here and not only in Save. Save clamps what reaches the file,
	// but the page reads the in-memory copy, so an unclamped value would show
	// as stored while the file held something else - and the next start would
	// silently disagree with the screen.
	a.settings.Backup.EveryDays = settings.ClampDays(everyDays)
	a.settings.Backup.AutoRun = autoRun
	next := a.settings
	a.settingsMu.Unlock()

	if err := a.saveSettings(next); err != nil {
		return err
	}
	a.emitSchedule()
	return nil
}

// Language reports the language the backend currently speaks, so the page can
// paint its picker to match after a restart.
func (a *App) Language() string {
	return i18n.L()
}

// SetLanguage switches every backend-built string to lang and remembers the
// choice in the settings file.
//
// Applied at once rather than on the next start: the checklist and the health
// details are rebuilt from live state every few seconds, so the new language
// is on screen within one tick, and the page re-pulls what is pushed less
// often. Only the tray menu keeps its startup language until the next launch -
// it is built once, before any page exists to ask for a switch.
func (a *App) SetLanguage(lang string) error {
	if !i18n.Known(lang) {
		return fmt.Errorf("%s: %s", i18n.T("settings.prefix"),
			i18n.Tf("app.err.lang", lang, strings.Join(i18n.Languages, ", ")))
	}
	i18n.Set(lang)

	a.settingsMu.Lock()
	a.settings.Language = lang
	next := a.settings
	a.settingsMu.Unlock()

	if err := a.saveSettings(next); err != nil {
		return err
	}
	a.emitSchedule()
	return nil
}

// SnoozeSchedule defers the reminder by the given number of days.
func (a *App) SnoozeSchedule(days int) error {
	now := a.clock.Now()

	a.settingsMu.Lock()
	a.settings.Backup = a.settings.Backup.Snooze(now, days)
	next := a.settings
	a.settingsMu.Unlock()

	if err := a.saveSettings(next); err != nil {
		return err
	}
	a.emitSchedule()
	return nil
}

// saveSettings writes the file and reports the outcome to the page.
//
// The in-memory copy is kept even when the write fails: the user's choice
// applies for this session either way, and reverting the screen to the old
// value because a file could not be written would be a worse lie than a
// setting that works until the window closes.
func (a *App) saveSettings(s settings.Settings) error {
	err := settings.Save(a.lay.SettingsFile, s)

	a.settingsMu.Lock()
	if err != nil {
		a.settingsErr = err.Error()
	} else {
		a.settingsErr = ""
	}
	a.settingsMu.Unlock()

	if err != nil && a.log != nil {
		a.log.Error("настройки не сохранились", "err", err)
	}
	return err
}

// recordBackupRun stamps a successful point into the schedule.
func (a *App) recordBackupRun() {
	now := a.clock.Now()

	a.settingsMu.Lock()
	a.settings.Backup = a.settings.Backup.Ran(now)
	next := a.settings
	a.settingsMu.Unlock()

	if err := a.saveSettings(next); err != nil && a.log != nil {
		a.log.Error("не удалось запомнить время точки отката", "err", err)
	}
}

// recordBackupAttempt stamps a failed automatic attempt.
//
// Only the attempt moves, so the reminder stays on: see settings.Backup.
func (a *App) recordBackupAttempt() {
	now := a.clock.Now()

	a.settingsMu.Lock()
	a.settings.Backup = a.settings.Backup.Attempted(now)
	next := a.settings
	a.settingsMu.Unlock()

	_ = a.saveSettings(next)
}

// checkSchedule is the scheduler: one decision, once a second.
//
// It only ever starts a point when the user asked it to start one - AutoRun is
// off by default and is a separate checkbox from the reminder. What it must
// never do is retry: the loop runs once a second, so a failing dump without
// the LastAttempt guard would be retried a thousand times between one
// successful one, each attempt writing a partial file first.
func (a *App) checkSchedule() {
	a.settingsMu.Lock()
	s := a.settings
	a.settingsMu.Unlock()

	if !s.Backup.Enabled || !s.Backup.AutoRun {
		return
	}

	now := a.clock.Now()
	due, _ := s.Backup.Due(now)
	if !due || !s.Backup.CanRetry(now) {
		return
	}

	// Nothing to dump into: a logical point needs something listening on
	// 3306, and there is no point queuing one. The reminder stays on screen,
	// which is what the user needs to see in this state anyway.
	//
	// Asked the same way dumps.checkLogical asks it - the port directly,
	// rather than the supervisor's state - because that is the question that
	// actually decides whether a dump will work. The supervisor's answer is
	// narrower: a MySQL started outside the panel reports "external", and
	// dumping from it is perfectly fine.
	if a.dumps == nil || a.ops == nil || !a.ops.Listen(repack.MySQLPort) {
		return
	}

	// A point is already being made. The runner would refuse a second one,
	// and the refusal would be logged once a second.
	a.backupMu.Lock()
	running := a.backup.Running
	a.backupMu.Unlock()
	if running {
		return
	}

	// Stamped before the work, not after: the attempt has happened, and
	// stamping it only on the way out would leave the loop free to start a
	// second one during the first.
	a.recordBackupAttempt()

	if a.log != nil {
		a.log.Info("точка отката по расписанию")
	}
	if err := a.CreateBackup(string(dumps.KindLogical)); err != nil {
		if a.log != nil {
			a.log.Info("точка отката по расписанию не началась", "err", err)
		}
	}
}

// runBackup is the work, on its own goroutine.
func (a *App) runBackup(ctx context.Context, kind dumps.Kind) {
	var (
		rep dumps.Report
		err error
	)
	if kind == dumps.KindPhysical {
		rep, err = a.dumps.Physical(ctx)
	} else {
		rep, err = a.dumps.Logical(ctx)
	}
	a.finishBackup(kind, rep, err)
}

// backupProgress is the runner's progress sink.
//
// Called from the backup's own goroutine, about fifty times for a full
// point - once per database and every 8 MiB inside one. The lock is held
// only long enough to update the state, and the event goes out after it is
// released: emitting while holding it would let a slow Wails call block the
// goroutine that is writing the dump.
func (a *App) backupProgress(p dumps.Progress) {
	a.backupMu.Lock()
	a.backup.Kind = string(p.Kind)
	a.backup.Step = p.Step
	a.backup.Index = p.Index
	a.backup.Total = p.Total
	a.backup.Bytes = p.Bytes
	a.backup.ElapsedSec = p.Elapsed.Seconds()
	snap := a.backup
	a.backupMu.Unlock()

	a.emitBackup(snap)
}

// finishBackup records the outcome and tells the page.
func (a *App) finishBackup(kind dumps.Kind, rep dumps.Report, err error) {
	a.backupMu.Lock()
	started := a.backupStarted
	a.backup.Running = false
	a.backup.At = time.Now()
	a.backup.ElapsedSec = time.Since(started).Seconds()
	a.backupCancel = nil

	// The step is cleared either way. Leaving it would have the panel name
	// a database that is not being written any more, next to a finished
	// result.
	a.backup.Step = ""
	if err != nil {
		a.backup.Err = err.Error()
		a.backup.Dir = ""
		a.backup.SavedBytes = 0
		a.backup.Databases = nil
		a.backup.Warnings = nil
		a.backup.Index = 0
	} else {
		a.backup.Err = ""
		a.backup.Dir = rep.Dir
		a.backup.SavedBytes = rep.Bytes
		a.backup.Databases = rep.Databases
		a.backup.Warnings = rep.Warnings
		a.backup.Index = a.backup.Total
		a.backup.Bytes = rep.Bytes
	}
	snap := a.backup
	a.backupMu.Unlock()

	if a.log != nil {
		if err != nil {
			a.log.Error("точка отката не создана", "kind", kind, "err", err)
		} else {
			a.log.Info("точка отката создана",
				"kind", kind, "dir", rep.Dir, "bytes", rep.Bytes)
		}
	}
	a.emitBackup(snap)

	// The schedule remembers the point that was just made. Recorded here
	// rather than at the call sites, because this is the one place that
	// knows a point was made - a manual one and an automatic one both land
	// here, and the schedule cares only that a fresh point exists.
	//
	// Only on success. A failed dump leaves LastRun alone, which is what
	// keeps the reminder on instead of going quiet for a week.
	if err == nil {
		a.recordBackupRun()
		a.emitSchedule()
	}

	// A new folder appeared under backups\, so the list on screen is stale.
	// Only on success: a failed attempt deletes what it made, and re-reading
	// would just re-send the same list.
	if err == nil {
		go a.refreshData(context.Background())
	}
}

func (a *App) emitSchedule() {
	if a.sink != nil {
		a.sink.Emit(notify.Event{Type: notify.EventSchedule, Data: a.Schedule()})
	}
}

func (a *App) emitBackup(s BackupStatus) {
	if a.sink != nil {
		a.sink.Emit(notify.Event{Type: notify.EventBackup, Data: s})
	}
}

// parseKind maps a frontend string onto a backup kind.
//
// An unknown value is an error rather than a default. Quietly taking a
// physical snapshot when the page asked for something else would produce a
// backup that is not the portable kind, in a folder that looks like one.
func parseKind(name string) (dumps.Kind, error) {
	switch dumps.Kind(name) {
	case dumps.KindLogical:
		return dumps.KindLogical, nil
	case dumps.KindPhysical:
		return dumps.KindPhysical, nil
	default:
		return "", errors.New(i18n.Tf("app.err.kind", name))
	}
}

// stepsOf is how many steps a backup of this kind has, so the progress bar
// has a denominator before the first report arrives.
func stepsOf(k dumps.Kind) int {
	if k == dumps.KindPhysical {
		return 1
	}
	return len(repack.Databases)
}

// --- the realm checklist ---------------------------------------------------

// SetupItem is one line of the realm-setup checklist.
type SetupItem struct {
	// Key is a stable identifier for the page to hang styles and tests on.
	Key   string
	Label string
	OK    bool
	// Detail is what is actually true, in the user's terms, and is shown
	// whether or not the item passes. "точка отката есть, ей 3 дня" says
	// more than a tick does.
	Detail string
	// Fix is what to do when the item does not pass. Empty when it does.
	Fix string
}

// backupStaleAfter is the fallback age at which the checklist says the newest
// rollback point is old.
//
// A week: that is the point at which a person who plays a few evenings a
// week has done enough since the last copy that restoring to it would cost
// them real progress.
//
// Used only when no schedule is set - see App.rollbackStaleAfter. A schedule
// that is on supplies its own interval, so the reminder and the checklist
// never disagree about the same point.
const backupStaleAfter = 7 * 24 * time.Hour

// minFreeForBackup is the free space below which the checklist warns.
//
// Measured on this repack: a full logical point is 385 MB and a physical
// snapshot 131 MB, so a new pair costs about half a gigabyte and the points
// already there total roughly 1.6 GB. Two gigabytes leaves room for the
// next pair without having to delete anything first.
const minFreeForBackup = 2 << 30

// Setup evaluates the realm checklist.
//
// Derived rather than stored. Every line comes from what the app already
// holds - the layout, the data snapshot, the supervisor - so the checklist
// costs one walk of the repack and cannot disagree with the screens it
// summarizes. It answers one question: is this realm playable right now,
// and if not, what is missing.
func (a *App) Setup() []SetupItem {
	data := a.Data()

	return []SetupItem{
		a.setupRepack(),
		setupDatabase(data),
		setupRollback(data, a.rollbackStaleAfter()),
		setupSpace(data),
		setupServer(a.sup.Status()),
	}
}

func (a *App) setupRepack() SetupItem {
	item := SetupItem{Key: "repack", Label: i18n.T("setup.repack.label")}
	if a.lay.Root == "" {
		item.Detail = i18n.T("setup.repack.nofolder")
		item.Fix = i18n.T("setup.repack.fix")
		return item
	}

	var missing []string
	for _, path := range []string{a.lay.WorldserverExe, filepath.Join(a.lay.Root, "configs"), a.lay.DataDbcDir} {
		if _, err := os.Stat(path); err != nil {
			missing = append(missing, filepath.Base(path))
		}
	}
	if len(missing) > 0 {
		// A half-unpacked repack is the common failure, and naming the
		// pieces is what makes it fixable.
		item.Detail = i18n.Tf("setup.repack.missing", strings.Join(missing, ", "))
		item.Fix = i18n.T("setup.repack.fix2")
		return item
	}

	item.OK = true
	item.Detail = a.lay.Root
	return item
}

func setupDatabase(data DataSnapshot) SetupItem {
	item := SetupItem{Key: "database", Label: i18n.T("setup.db.label")}

	// Unread is not the same as absent. A zero-valued snapshot has an empty
	// Err, and reporting that as "the database answers" would be a green
	// tick nobody earned - the same trap the Loaded flag exists for.
	if !data.Loaded {
		item.Detail = i18n.T("setup.db.notread")
		return item
	}

	if data.Realm.Err != "" {
		item.Detail = data.Realm.Err
		item.Fix = i18n.T("setup.db.fix")
		return item
	}

	item.OK = true
	item.Detail = i18n.Tf("setup.db.detail",
		len(data.Realm.Databases), humanBytes(data.Realm.TotalBytes), data.Realm.Accounts)
	return item
}

// rollbackStaleAfter is how old the newest point may get before the checklist
// complains.
//
// The schedule's own interval when the reminder is on, so the two agree: a
// person who set "every 3 days" must not be told by the checklist that three
// days is fine while the reminder beside it says the point is overdue. The
// default of a week applies when there is no schedule to ask.
func (a *App) rollbackStaleAfter() time.Duration {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()

	if a.settings.Backup.Enabled && a.settings.Backup.EveryDays > 0 {
		return time.Duration(a.settings.Backup.EveryDays) * 24 * time.Hour
	}
	return backupStaleAfter
}

func setupRollback(data DataSnapshot, staleAfter time.Duration) SetupItem {
	item := SetupItem{Key: "rollback", Label: i18n.T("setup.rollback.label")}

	if !data.Loaded {
		item.Detail = i18n.T("setup.db.notread")
		return item
	}

	points := data.Backups.Points
	if len(points) == 0 {
		item.Detail = i18n.T("setup.rollback.none")
		if data.Backups.Err != "" {
			item.Detail = data.Backups.Err
		}
		item.Fix = i18n.T("setup.rollback.fix")
		return item
	}

	// Points come back newest first, so the first one is the one that
	// decides this.
	newest := points[0]
	age := time.Since(newest.Created)
	if age < 0 {
		// A folder named for a moment in the future - a clock that moved,
		// or a name copied from elsewhere. Reported as fresh rather than
		// as minus six hours.
		age = 0
	}

	item.OK = age <= staleAfter
	item.Detail = i18n.Tf("setup.rollback.detail", humanDuration(age), newest.Name)
	if !item.OK {
		item.Fix = i18n.Tf("setup.rollback.stale", humanDuration(staleAfter))
	}
	return item
}

func setupSpace(data DataSnapshot) SetupItem {
	item := SetupItem{Key: "space", Label: i18n.T("setup.space.label")}
	inv := data.Backups

	if !data.Loaded {
		item.Detail = i18n.T("setup.db.notread")
		return item
	}

	// A zero here means the figure could not be read, not that the disk is
	// full: WinOps only leaves it at zero when GetDiskFreeSpaceEx failed,
	// which happens when the folder does not exist. Worth saying rather
	// than passing quietly.
	if inv.FreeBytes <= 0 {
		item.Detail = i18n.T("setup.space.unknown")
		item.Fix = i18n.T("setup.space.fix")
		return item
	}

	item.OK = inv.FreeBytes >= minFreeForBackup
	item.Detail = i18n.Tf("setup.space.detail", humanBytes(inv.FreeBytes))
	if !item.OK {
		item.Fix = i18n.T("setup.space.lowfix")
	}
	return item
}

// setupServer answers "is the realm up", and a process this panel did not
// start is up as surely as one it did.
//
// The distinction that made this wrong is Owned, not liveness. A world that
// outlived the panel it was started from is StateExternal, and counting only
// StateRunning put the checklist at "1 из 3" with the advice "нажмите
// «Запустить сервер»" at a person whose world was already running - the
// button does not start a second one. The frontend counts external in
// upCount, so this is where the two used to disagree on screen.
func setupServer(snap supervisor.Snapshot) SetupItem {
	item := SetupItem{Key: "server", Label: i18n.T("setup.server.label")}

	up := 0
	for _, svc := range snap.Services {
		if svc.State == supervisor.StateRunning || svc.State == supervisor.StateExternal {
			up++
		}
	}
	total := len(snap.Services)

	item.OK = total > 0 && up == total
	item.Detail = i18n.Tf("setup.server.detail", up, total)
	if !item.OK {
		item.Fix = i18n.T("setup.server.fix")
	}
	return item
}

// OpenFolder reveals one of the repack's own folders in Explorer.
//
// A whitelist rather than a path from the page. The frontend can send any
// string, and a binding that opened whatever it was handed would make the
// app able to open anything on the disk - a strange power for a control
// panel, and impossible to explain to whoever reads this next.
func (a *App) OpenFolder(which string) error {
	var dir string
	switch which {
	case "backups":
		dir = a.lay.BackupsDir
	case "logs":
		dir = a.lay.LogsDir
	case "configs":
		dir = filepath.Join(a.lay.Root, "configs")
	case "root":
		dir = a.lay.Root
	default:
		return errors.New(i18n.Tf("app.folder.unknown", which))
	}

	if dir == "" {
		return errors.New(i18n.T("app.folder.norepack"))
	}
	if a.ops == nil {
		return errors.New(i18n.T("app.folder.noenv"))
	}
	return a.ops.OpenPath(dir)
}

// humanBytes renders a byte count the way a person reads it.
//
// The frontend has its own formatter, for the figures it renders itself.
// This one exists because the checklist assembles sentences, and a sentence
// has to be built on the side that knows what it means rather than on the
// side that only draws it.
func humanBytes(n int64) string {
	// The same figure the frontend renders in bytes() has to come out spelled
	// the same way, or one screen shows two numbers for one fact: the tile
	// said "1,2 ГБ" and the checklist beside it said "1.2 ГБ". So: the
	// language's decimal separator, and the unit picked so the number stays
	// under 1024 - "1255.4 МБ" and "1.2 ГБ" are the same size written twice,
	// and comparing them by eye is the only thing this figure is for.
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d %s", n, i18n.T("unit.b"))
	}

	units := []string{i18n.T("unit.kb"), i18n.T("unit.mb"), i18n.T("unit.gb"), i18n.T("unit.tb")}
	value := float64(n)
	i := -1
	for value >= unit && i < len(units)-1 {
		value /= unit
		i++
	}

	// One decimal only below ten, exactly as bytes() does: "977 МБ" reads
	// better than "977.4 МБ", but "1.2 ГБ" would be badly misread.
	//
	// The separator is a manual swap, not a locale: Go's %f always writes a
	// dot, whatever the process locale is. i18n.DecimalSep is the same swap
	// the frontend's bytes() makes.
	digits := 0
	if value < 10 {
		digits = 1
	}
	return strings.Replace(fmt.Sprintf("%.*f %s", digits, value, units[i]), ".", i18n.DecimalSep(), 1)
}

// humanDuration renders an age the way a person says it.
//
// "3 дня", not "72 ч 0 мин": the checklist is a sentence about whether a
// rollback point is recent, and the largest unit that fits is the whole of
// what it needs to say.
func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return i18n.T("duration.ltmin")
	case d < time.Hour:
		return i18n.P(int(d.Minutes()), "dur.minute")
	case d < 24*time.Hour:
		return i18n.P(int(d.Hours()), "dur.hour")
	default:
		return i18n.P(int(d.Hours()/24), "dur.day")
	}
}

// StartAll brings the whole stack up.
func (a *App) StartAll() error {
	return a.sup.StartAll(context.Background())
}

// StopAll takes the whole stack down, in order.
func (a *App) StopAll() error {
	return a.sup.StopAll(context.Background())
}

// Start brings one service up, doing nothing if it is already running.
func (a *App) Start(service string) error {
	id, err := parseService(service)
	if err != nil {
		return err
	}
	return a.sup.StartService(context.Background(), id)
}

// Stop takes one service down.
func (a *App) Stop(service string) error {
	id, err := parseService(service)
	if err != nil {
		return err
	}
	return a.sup.StopService(context.Background(), id)
}

// Restart stops a service and starts it again.
//
// Stop first, always. Starting first would have two worldserver processes
// writing the same characters for as long as it takes to notice.
func (a *App) Restart(service string) error {
	id, err := parseService(service)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if err := a.sup.StopService(ctx, id); err != nil {
		return fmt.Errorf("%s: %w", i18n.Tf("app.restart.stopfail", id), err)
	}
	return a.sup.StartService(ctx, id)
}

// parseService maps a frontend string onto a service.
//
// The frontend can send anything, so an unknown value is an error rather
// than a map index that panics.
func parseService(name string) (supervisor.ServiceID, error) {
	switch supervisor.ServiceID(name) {
	case supervisor.SvcMySQL:
		return supervisor.SvcMySQL, nil
	case supervisor.SvcAuth:
		return supervisor.SvcAuth, nil
	case supervisor.SvcWorld:
		return supervisor.SvcWorld, nil
	default:
		return "", errors.New(i18n.Tf("app.err.service", name))
	}
}

// askConfirm is how the supervisor asks the frontend whether to force-kill.
// confirmer is the only caller; see the note on that type for why this is not
// exported.
//
// The event goes out BEFORE the channel is returned. The supervisor blocks on
// that channel, so emitting afterwards would show the dialog only after the
// operation had already stopped waiting for it.
func (a *App) askConfirm(service string, pid int) <-chan bool {
	ch := make(chan bool, 1)

	a.mu.Lock()
	a.pending = ch
	a.mu.Unlock()

	if a.sink != nil {
		a.sink.Emit(notify.Event{
			Type: notify.EventConfirmKill,
			Data: map[string]any{"service": service, "pid": pid},
		})
	}
	return ch
}

// RespondConfirm delivers the user's answer and clears the pending dialog.
//
// A nil pending is normal, not an error: the frontend can call this at any
// time, and the dialog it was answering may already be gone. It must not
// panic.
func (a *App) RespondConfirm(ok bool) {
	a.mu.Lock()
	ch := a.pending
	a.pending = nil
	a.mu.Unlock()

	if ch == nil {
		return
	}
	// Buffered and never closed, so a second answer has nowhere to go.
	select {
	case ch <- ok:
	default:
	}
}
