package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"wow-hub/internal/backups"
	"wow-hub/internal/dumps"
	"wow-hub/internal/health"
	"wow-hub/internal/i18n"
	"wow-hub/internal/notify"
	"wow-hub/internal/osops"
	"wow-hub/internal/repack"
	"wow-hub/internal/restore"
	"wow-hub/internal/settings"
	"wow-hub/internal/supervisor"
)

type appFixture struct {
	app    *App
	sup    *supervisor.Supervisor
	ops    *osops.FakeOps
	clk    *osops.FakeClock
	sink   *notify.Recorder
	lay    repack.Layout
	pinger *okPinger
	// started records the launch order, which is how Restart is checked.
	started []string
}

// newAppFixture builds the app over the standard fakes.
//
// mutate runs before the App is built, which is where a test swaps in a real
// backups folder or a database pool. Building it afterwards would leave the
// supervisor's confirmer pointing at the previous App.
func newAppFixture(t *testing.T, mutate ...func(*appFixture)) *appFixture {
	t.Helper()
	ops := osops.NewFake()
	clk := osops.NewFakeClock()
	sink := notify.NewRecorder()
	lay := testLayout()

	f := &appFixture{ops: ops, clk: clk, sink: sink, lay: lay}

	// A real worldserver prints its initialization line into the pipe the app
	// hands it. The fake has to do the same, or "the world never becomes
	// ready" is the only scenario under test.
	ops.OnStart = func(exe string, args []string, dir string, out io.Writer) (osops.Proc, error) {
		lower := strings.ToLower(exe)
		if strings.Contains(lower, "worldserver") {
			out.Write([]byte("WORLD: World Initialized In 0 Minutes 46 Seconds\r\n"))
			ops.Listening[8085] = true
		}
		if strings.Contains(lower, "authserver") {
			ops.Listening[3724] = true
		}
		if strings.Contains(lower, "mysqld") {
			ops.Listening[3306] = true
		}
		return osops.NewFakeProc(900), nil
	}

	conf := &scriptedConfirmer{}
	pinger := &okPinger{}
	f.pinger = pinger
	f.sup = supervisor.New(ops, clk, lay, sink, pinger, conf, supervisor.Conf{})

	for _, m := range mutate {
		m(f)
	}
	f.app = NewApp(f.deps())

	return f
}

// deps is what the App is built from, so a test that changes one input can
// rebuild it without repeating the rest.
func (f *appFixture) deps() Deps {
	return Deps{
		Supervisor: f.sup,
		Health:     health.NewRunner(f.ops, f.lay, f.pinger),
		Sink:       f.sink,
		Ops:        f.ops,
		Layout:     f.lay,
		Clock:      f.clk,
	}
}

func testLayout() repack.Layout {
	const root = `C:\repack`
	return repack.Layout{
		Root:           root,
		WorldserverExe: root + `\worldserver.exe`,
		AuthserverExe:  root + `\authserver.exe`,
		MySQLdExe:      root + `\mysql\bin\mysqld.exe`,
		MySQLadminExe:  root + `\mysql\bin\mysqladmin.exe`,
		MySQLdumpExe:   root + `\mysql\bin\mysqldump.exe`,
		WorldConf:      root + `\configs\worldserver.conf`,
		AuthConf:       root + `\configs\authserver.conf`,
		MySQLPassConf:  root + `\temp\mysql_ac.conf`,
		MySQLRootConf:  root + `\temp\mysql_root.conf`,
		WorldConfRel:   "configs/worldserver.conf",
		AuthConfRel:    "configs/authserver.conf",
		DataDbcDir:     root + `\data\dbc`,
		LogsDir:        root + `\logs`,
		BackupsDir:     root + `\backups`,
		SettingsFile:   root + `\wow-hub.settings.json`,
	}
}

// testRoot is one directory for the whole package, removed once.
//
// t.TempDir() is deliberately not used. Measured on this machine, removing
// one test directory costs about 1.3 seconds however few files are in it -
// the antivirus holds a handle on what was just written and Windows makes
// RemoveAll wait for it - against milliseconds to create the tree. Across
// this package that is half a minute of waiting for a directory nobody
// looks at, and none of it is time the code under test spends.
var testRoot string

func TestMain(m *testing.M) {
	// Same reason as in the other packages: the assertions here were written
	// against the Russian wording, so the tests pin it explicitly rather than
	// inheriting the process default.
	i18n.Set(i18n.Ru)

	dir, err := os.MkdirTemp("", "wow-hub-app")
	if err != nil {
		fmt.Fprintln(os.Stderr, "временный каталог:", err)
		os.Exit(1)
	}
	testRoot = dir

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// tempDir returns a fresh directory for one test.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(testRoot, strings.ReplaceAll(t.Name(), "/", "_"))
	if err != nil {
		t.Fatalf("временный каталог: %v", err)
	}
	return dir
}

// realRepack builds a repack tree on disk and returns the layout for it.
//
// The layout comes from repack.LocateFrom rather than from a struct literal,
// so the paths the backup tests exercise are the ones the app will use: a
// hand-written Layout keeps passing after somebody changes newLayout, and
// the failure then shows up only on a real machine.
func realRepack(t *testing.T) repack.Layout {
	t.Helper()
	root := tempDir(t)

	for _, dir := range []string{
		filepath.Join(root, "configs"),
		filepath.Join(root, "data", "dbc"),
		filepath.Join(root, "mysql", "bin"),
		filepath.Join(root, "temp"),
		filepath.Join(root, "backups"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	// The three markers LocateFrom insists on, plus everything the dump path
	// stats before it starts.
	files := []string{
		filepath.Join(root, "worldserver.exe"),
		filepath.Join(root, "mysql", "bin", "mysqldump.exe"),
		filepath.Join(root, "backups", "snapshot-mysql-data.cmd"),
	}
	for _, path := range files {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	conf := "[client]\npassword = \"acore\"\n"
	if err := os.WriteFile(filepath.Join(root, "temp", "mysql_ac.conf"), []byte(conf), 0o644); err != nil {
		t.Fatalf("write mysql_ac.conf: %v", err)
	}

	lay, err := repack.LocateFrom(root)
	if err != nil {
		t.Fatalf("repack.LocateFrom(%s): %v", root, err)
	}
	return lay
}

// logicalDumpBody is what a finished mysqldump leaves behind. The trailer is
// what the app checks for, so a fake that omits it models a truncated dump.
const logicalDumpBody = "DROP TABLE IF EXISTS `account`;\n" +
	"-- Dump completed on 2026-09-30 12:00:00\n"

// backupFixture is an app over a repack on disk with MySQL answering.
func backupFixture(t *testing.T) *appFixture {
	t.Helper()
	lay := realRepack(t)
	f := newAppFixture(t, func(f *appFixture) { f.lay = lay })
	f.ops.Listening[repack.MySQLPort] = true
	f.ops.RunToBody = logicalDumpBody
	return f
}

// scriptedConfirmer answers from a script. With no script it agrees, because
// most paths under test are the ones where the user says yes; tests that
// care about a refusal set answers = []bool{false}.
type scriptedConfirmer struct {
	answers []bool
	asked   []string
	calls   int
}

func (c *scriptedConfirmer) AskConfirm(service string, pid int) <-chan bool {
	c.asked = append(c.asked, service)
	ch := make(chan bool, 1)
	if c.calls < len(c.answers) {
		ch <- c.answers[c.calls]
	} else if c.answers == nil {
		ch <- true
	}
	c.calls++
	return ch
}

type okPinger struct{}

func (okPinger) Ping(context.Context) (time.Duration, error) { return time.Millisecond, nil }
func (okPinger) Close() error                                { return nil }

func TestTickForwardsHealthEveryFiveTicks(t *testing.T) {
	// Pinging the database is not free, and once per five seconds is well
	// inside what a person can notice.
	f := newAppFixture(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		f.app.tick(ctx)
	}
	if got := f.sink.CountOf(notify.EventHealth); got != 1 {
		t.Fatalf("after 5 ticks, %d health events, want 1", got)
	}

	for i := 0; i < 5; i++ {
		f.app.tick(ctx)
	}
	if got := f.sink.CountOf(notify.EventHealth); got != 2 {
		t.Fatalf("after 10 ticks, %d health events, want 2", got)
	}
}

func TestTickForwardsStatusEveryTick(t *testing.T) {
	f := newAppFixture(t)
	f.ops.Procs["worldserver.exe"] = []int{4242}
	f.sup.Refresh()
	f.sink.Reset()

	for i := 0; i < 3; i++ {
		f.app.tick(context.Background())
	}

	// Refresh emits only on a real change, so one event here is correct -
	// three ticks with an unchanged world must not repaint the table three
	// times.
	if got := f.sink.CountOf(notify.EventStatus); got > 1 {
		t.Fatalf("emitted %d status events for an unchanged world across 3 ticks", got)
	}
}

func TestRespondConfirmWithoutPendingDoesNotPanic(t *testing.T) {
	// The frontend can call any bound method at any time, including while no
	// dialog is open. A panic in the backend takes the whole app down with
	// the server still running - the worst possible moment to lose the
	// window.
	f := newAppFixture(t)
	f.app.RespondConfirm(true)
	f.app.RespondConfirm(false)
}

func TestAskConfirmEmitsBeforeItBlocks(t *testing.T) {
	// The supervisor blocks on the channel AskConfirm returns. If the event
	// went out after that, the dialog would appear only once the operation
	// had already given up on it.
	f := newAppFixture(t)

	ch := f.app.askConfirm("world", 4242)

	if got := f.sink.CountOf(notify.EventConfirmKill); got != 1 {
		t.Fatalf("emitted %d confirm-kill events, want 1 before blocking", got)
	}
	if ch == nil {
		t.Fatal("AskConfirm returned a nil channel; the supervisor would block forever")
	}
}

func TestRespondConfirmUnblocksAskConfirm(t *testing.T) {
	f := newAppFixture(t)

	ch := f.app.askConfirm("world", 4242)

	got := make(chan bool, 1)
	go func() { got <- <-ch }()

	// Give the goroutine a moment to actually block on the channel, so this
	// test cannot pass without the round trip doing anything.
	time.Sleep(20 * time.Millisecond)
	f.app.RespondConfirm(true)

	select {
	case v := <-got:
		if !v {
			t.Fatal("AskConfirm yielded false, want true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RespondConfirm did not unblock AskConfirm")
	}
}

func TestRespondConfirmFalseUnblocksWithFalse(t *testing.T) {
	// The refusal path matters more than the agreement one: a "true" sent by
	// mistake would kill a process the user meant to keep.
	f := newAppFixture(t)

	ch := f.app.askConfirm("world", 4242)
	got := make(chan bool, 1)
	go func() { got <- <-ch }()

	time.Sleep(20 * time.Millisecond)
	f.app.RespondConfirm(false)

	select {
	case v := <-got:
		if v {
			t.Fatal("AskConfirm yielded true, want false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RespondConfirm did not unblock AskConfirm")
	}
}

func TestRespondConfirmOnlyAnswersOnce(t *testing.T) {
	// A double click on the dialog's button must not leave a second answer
	// sitting in the channel, waiting to be picked up by some future dialog.
	f := newAppFixture(t)

	ch := f.app.askConfirm("world", 4242)
	f.app.RespondConfirm(true)
	<-ch
	f.app.RespondConfirm(true)

	select {
	case v := <-ch:
		t.Fatalf("channel yielded a second answer %v; the pending dialog was not cleared", v)
	default:
		// Nothing more to read, which is the guarantee.
	}
}

func TestUnknownServiceReturnsError(t *testing.T) {
	// The frontend can send anything. A map index would panic on a bad
	// string and take the app down mid-server.
	f := newAppFixture(t)

	for _, name := range []string{"nope", "", "WORLD", "mysql.exe"} {
		if err := f.app.Start(name); err == nil {
			t.Errorf("Start(%q) returned nil, want an error", name)
		}
		if err := f.app.Stop(name); err == nil {
			t.Errorf("Stop(%q) returned nil, want an error", name)
		}
		if err := f.app.Restart(name); err == nil {
			t.Errorf("Restart(%q) returned nil, want an error", name)
		}
	}
}

func TestRestartStopsBeforeStarting(t *testing.T) {
	// Restart that starts first would have two worldserver processes writing
	// the same characters for as long as it takes to notice.
	f := newAppFixture(t)
	f.ops.Procs["worldserver.exe"] = []int{4242}
	f.ops.PortPIDs[8085] = 4242
	f.ops.Listening[8085] = true
	// Restart ends in a start, and a world may not be started without its
	// database - StartService refuses it. MySQL has to be up for this test to
	// be about the stop-before-start ordering at all.
	f.ops.Procs["mysqld.exe"] = []int{1111}
	f.sup.Refresh()

	// Stop and start both wait on the clock - stopping an adopted process
	// asks before killing, and starting waits for the world socket - so a
	// background pump advances time while the operation runs.
	done := make(chan error, 1)
	go func() { done <- f.app.Restart("world") }()
	go f.pumpClock()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Restart: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Restart never returned")
	}

	if len(f.ops.KillCalls) != 1 {
		t.Fatalf("KillCalls = %v, want exactly one stop before the start", f.ops.KillCalls)
	}
}

// pumpClock advances the fake clock in the background whenever something is
// blocked on it, for the given budget.
//
// The supervisor blocks on the clock between phases of an operation, and a
// test that never advances it waits forever - which is exactly what happened
// the first time this ran: an adopted process asks before killing, the
// dialog was never answered, and the whole package timed out.
func (f *appFixture) pumpClock() {
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if f.clk.Pending() > 0 {
			f.clk.Fire(time.Second)
			continue
		}
		time.Sleep(time.Millisecond)
	}
}

func TestStatusAndHealthAreCallableWithNothingRunning(t *testing.T) {
	// The frontend calls these on load, before anything has started.
	f := newAppFixture(t)

	if got := len(f.app.Status().Services); got != 3 {
		t.Errorf("Status returned %d services, want 3", got)
	}
	if got := len(f.app.Health()); got != 6 {
		t.Errorf("Health returned %d checks, want 6", got)
	}
}

// backupsWithOnePoint makes a real backups\ folder holding one logical dump,
// so the read path is exercised against a filesystem rather than a fake.
func backupsWithOnePoint(t *testing.T) string {
	t.Helper()
	dir := tempDir(t)
	point := filepath.Join(dir, "2026-09-29_current")
	if err := os.MkdirAll(point, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(point, repack.DBWorld+".sql"), []byte("-- dump"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

// waitFor polls until cond holds, and fails with what it was waiting for.
//
// The refresh is asynchronous by design - that is the whole point of it - so
// a test of it has to wait rather than assume.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestDataReportsNothingReadYetBeforeTheFirstRefresh(t *testing.T) {
	// The page reads the cached snapshot on load to paint at once. Until a
	// read has finished it must be able to tell "nothing here" from "not
	// read yet" - an empty list and an unread one mean opposite things.
	f := newAppFixture(t)

	got := f.app.Data()
	if got.Loaded {
		t.Error("Loaded = true before any read; the page would show an empty list as the truth")
	}
	if !got.ReadAt.IsZero() {
		t.Errorf("ReadAt = %v before any read, want the zero time", got.ReadAt)
	}
}

func TestRefreshDataReadsTheFolderAndPushesTheSnapshot(t *testing.T) {
	dir := backupsWithOnePoint(t)
	f := newAppFixture(t, func(f *appFixture) { f.lay.BackupsDir = dir })

	f.app.refreshData(context.Background())

	got := f.app.Data()
	if !got.Loaded {
		t.Fatal("Loaded = false after a completed read")
	}
	if got.ReadAt.IsZero() {
		t.Error("ReadAt is zero after a read")
	}
	if len(got.Backups.Points) != 1 {
		t.Fatalf("found %d rollback points, want 1", len(got.Backups.Points))
	}
	if p := got.Backups.Points[0]; p.Kind != backups.KindLogical {
		t.Errorf("kind = %q, want %q for a folder of per-database dumps", p.Kind, backups.KindLogical)
	}
	// One database of the four is present, and the note has to say so: a
	// logical dump missing a schema restores a realm with a hole in it.
	if p := got.Backups.Points[0]; !strings.Contains(p.Note, repack.DBCharacters) {
		t.Errorf("Note = %q, want it to name %s as missing", p.Note, repack.DBCharacters)
	}
	if got := f.sink.CountOf(notify.EventData); got != 1 {
		t.Errorf("emitted %d data events, want 1", got)
	}
}

func TestRefreshDataCollapsesTriggersIntoOneExtraRead(t *testing.T) {
	// Three things want a refresh and two of them can fire at once. A second
	// concurrent read would load a busy database and produce nothing the
	// first will not also produce - but dropping the request entirely would
	// leave the page showing numbers from before the change that asked.
	//
	// The seam is DiskFree: Scan calls it exactly once per read, and the
	// fake can hold the first call open, which makes the window real instead
	// of a race the test usually loses.
	var calls int32
	release := make(chan struct{})
	dir := backupsWithOnePoint(t)

	f := newAppFixture(t, func(f *appFixture) {
		f.lay.BackupsDir = dir
		f.ops.OnDiskFree = func(string) (int64, int64, error) {
			if atomic.AddInt32(&calls, 1) == 1 {
				<-release
			}
			return 1 << 30, 1 << 31, nil
		}
	})

	done := make(chan struct{})
	go func() {
		f.app.refreshData(context.Background())
		close(done)
	}()

	// The extra triggers are called directly rather than through
	// RefreshData, because that one spawns a goroutine - and a goroutine the
	// scheduler happens to run after the read has finished is a new read
	// rather than a collapsed one. That is correct behaviour and not what
	// this test is about; the exported wrapper has its own test below.
	waitFor(t, "the first read to reach the filesystem", func() bool {
		return atomic.LoadInt32(&calls) >= 1
	})
	f.app.refreshData(context.Background())
	f.app.refreshData(context.Background())

	// Neither may have started a read of its own. That is the guarantee, and
	// it is checkable right here because the first read cannot have finished
	// while it is holding the channel.
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("%d reads started while one was already in flight, want 1", got)
	}

	close(release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("refreshData never returned after the read was released")
	}

	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Fatalf("%d reads for three triggers, want 2: one running, one to catch up", got)
	}
	if got := f.sink.CountOf(notify.EventData); got != 2 {
		t.Errorf("emitted %d data events, want 2", got)
	}
}

func TestRefreshDataTheExportedOneRunsInTheBackground(t *testing.T) {
	// The page calls this from a button, and it must not block: the size
	// query is a tenth of a second warm and ten seconds cold, and a binding
	// that blocks for ten seconds reads as a frozen window. So it returns
	// first and the answer arrives as an event.
	var calls int32
	dir := backupsWithOnePoint(t)

	f := newAppFixture(t, func(f *appFixture) {
		f.lay.BackupsDir = dir
		f.ops.OnDiskFree = func(string) (int64, int64, error) {
			atomic.AddInt32(&calls, 1)
			return 1 << 30, 1 << 31, nil
		}
	})

	f.app.RefreshData()
	waitFor(t, "the read RefreshData asked for", func() bool {
		return atomic.LoadInt32(&calls) >= 1
	})
	waitFor(t, "the snapshot to land", func() bool { return f.app.Data().Loaded })

	if got := f.sink.CountOf(notify.EventData); got != 1 {
		t.Errorf("emitted %d data events, want 1", got)
	}
}

func TestRefreshDataWithoutADatabaseReportsItInsteadOfPanicking(t *testing.T) {
	// A nil pool is the ordinary state of this application: it is nil
	// whenever temp\mysql_ac.conf is missing. Every screen has to say why
	// its numbers are missing rather than show zeros that look measured.
	dir := backupsWithOnePoint(t)
	f := newAppFixture(t, func(f *appFixture) { f.lay.BackupsDir = dir })

	f.app.refreshData(context.Background())

	got := f.app.Data()
	if got.Realm.Err == "" {
		t.Error("Realm.Err is empty with no pool; the screen would show zeros as a measurement")
	}
	// The rollback points do not depend on the database and must still read.
	if len(got.Backups.Points) != 1 {
		t.Errorf("found %d rollback points, want 1: the folder does not need MySQL", len(got.Backups.Points))
	}
	if chars := f.app.Characters(0); chars.Err == "" {
		t.Error("Characters.Err is empty with no pool, want an explanation")
	}
}

func TestRefreshDataWithoutARepackSaysSo(t *testing.T) {
	// The executable moved out of the repack: Layout is the zero value. The
	// message must name the missing folder rather than complain about an
	// empty path, which reads as a bug in the panel.
	f := newAppFixture(t, func(f *appFixture) { f.lay = repack.Layout{} })

	f.app.refreshData(context.Background())

	got := f.app.Data()
	if !strings.Contains(got.Backups.Err, "backups") {
		t.Errorf("Backups.Err = %q, want it to name the folder it could not read", got.Backups.Err)
	}
	if got.Loaded != true {
		t.Error("Loaded = false after a read that reported an error; the page would keep waiting")
	}
}

func TestTickRefreshesTheDataWhenMySQLChangesState(t *testing.T) {
	// The numbers on the data screens are wrong for exactly one reason:
	// MySQL came up or went down. Waiting for the slow timer would leave
	// "нет подключения к базе" on screen for up to a minute after the
	// database was back, which reads as a bug in the panel.
	var calls int32
	dir := backupsWithOnePoint(t)

	f := newAppFixture(t, func(f *appFixture) {
		f.lay.BackupsDir = dir
		f.ops.OnDiskFree = func(string) (int64, int64, error) {
			atomic.AddInt32(&calls, 1)
			return 1 << 30, 1 << 31, nil
		}
	})

	// The first tick always reads: lastMySQL starts empty, which no real
	// state equals, so the window fills in a second after it opens.
	f.app.tick(context.Background())
	waitFor(t, "the first read", func() bool { return f.app.Data().Loaded })
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("%d reads after the first tick, want 1", got)
	}

	// A second tick with nothing changed must not read again. The tick is
	// the loop's unit and it runs once a second forever; a read per tick
	// would query the database sixty times a minute for the same answer.
	f.app.tick(context.Background())
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("%d reads after an unchanged tick, want 1", got)
	}

	// MySQL appears. That is the change worth a read.
	f.ops.Procs["mysqld.exe"] = []int{1000}
	f.ops.PortPIDs[3306] = 1000
	f.ops.Listening[3306] = true
	f.sup.Refresh()

	f.app.tick(context.Background())
	waitFor(t, "the read MySQL coming up asked for", func() bool {
		return atomic.LoadInt32(&calls) >= 2
	})
}

// --- making a rollback point ----------------------------------------------

func TestBackupStatusStartsIdle(t *testing.T) {
	// The page calls this on load, before anything has been pressed.
	f := newAppFixture(t)

	got := f.app.BackupStatus()
	if got.Running {
		t.Error("Running = true before anything was asked for")
	}
	if got.Err != "" || got.Dir != "" {
		t.Errorf("состояние не пустое: %+v", got)
	}
}

func TestCreateBackupRejectsAnUnknownKind(t *testing.T) {
	// The page can send anything. Taking a default here would make a
	// physical snapshot, in a folder that looks like a logical one, for a
	// request that asked for neither.
	f := backupFixture(t)

	err := f.app.CreateBackup("logical-please")
	if err == nil {
		t.Fatal("CreateBackup принял неизвестный вид")
	}
	if !strings.Contains(err.Error(), "logical-please") {
		t.Errorf("ошибка не повторяет присланное значение: %v", err)
	}
	if len(f.ops.RunToCalls) != 0 {
		t.Error("что-то запустилось, хотя вид копии не распознан")
	}
}

func TestCreateBackupWithoutARepackSaysSo(t *testing.T) {
	// No Ops at all: the state a fixture that only drives the status loop
	// is in, and the state main.go is in when the repack was not found.
	app := NewApp(Deps{Sink: notify.NewRecorder()})

	err := app.CreateBackup("logical")
	if err == nil {
		t.Fatal("CreateBackup прошёл без репака")
	}
	if !strings.Contains(err.Error(), "репак") {
		t.Errorf("ошибка не говорит про репак: %v", err)
	}
}

func TestCreateBackupReturnsAtOnceAndReportsAsItGoes(t *testing.T) {
	f := backupFixture(t)

	if err := f.app.CreateBackup("logical"); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}

	// The whole point of the background run. A binding that returned with
	// the work done would hold the window for the 105 seconds a real point
	// takes, with nothing on screen to say why.
	if st := f.app.BackupStatus(); !st.Running {
		t.Error("CreateBackup вернулся уже с готовым результатом")
	}

	waitFor(t, "окончания копии", func() bool { return !f.app.BackupStatus().Running })

	st := f.app.BackupStatus()
	if st.Err != "" {
		t.Fatalf("Err = %q", st.Err)
	}
	if st.Kind != "logical" {
		t.Errorf("Kind = %q, want logical", st.Kind)
	}
	if st.Step != "" {
		t.Errorf("Step = %q после завершения, want пусто", st.Step)
	}
	if !slices.Equal(st.Databases, repack.Databases) {
		t.Errorf("Databases = %v, want %v", st.Databases, repack.Databases)
	}
	if st.SavedBytes != int64(len(logicalDumpBody))*4 {
		t.Errorf("SavedBytes = %d, want %d", st.SavedBytes, int64(len(logicalDumpBody))*4)
	}
	if _, err := os.Stat(filepath.Join(st.Dir, repack.DBWorld+".sql")); err != nil {
		t.Errorf("дампа нет там, где его называет состояние: %v", err)
	}
	if st.At.IsZero() {
		t.Error("At пуст — панель не сможет сказать, когда это было")
	}

	// Progress has to reach the page, not only the final state: the bar is
	// drawn from these.
	var progress int
	for _, e := range f.sink.Events() {
		if e.Type != notify.EventBackup {
			continue
		}
		progress++
	}
	if progress < 4 {
		t.Errorf("событий о копии: %d, ожидалось минимум 4 — по одному на базу", progress)
	}
}

func TestCreateBackupRefusesASecondOneWhileTheFirstRuns(t *testing.T) {
	// Two dumps at once would write 770 MB and load a database that is
	// already busy, to produce a second point that is no better than the
	// first.
	f := backupFixture(t)
	release := make(chan struct{})
	f.ops.OnRunTo = func(ctx context.Context, _ string, _ []string, _ string, out io.Writer) (string, error) {
		<-release
		_, err := io.WriteString(out, logicalDumpBody)
		return "", err
	}

	if err := f.app.CreateBackup("logical"); err != nil {
		t.Fatalf("первый CreateBackup: %v", err)
	}
	err := f.app.CreateBackup("logical")
	if err == nil {
		t.Fatal("второй CreateBackup прошёл, хотя первый ещё идёт")
	}
	if !strings.Contains(err.Error(), "уже создаётся") {
		t.Errorf("ошибка не объясняет отказ: %v", err)
	}

	close(release)
	waitFor(t, "окончания копии", func() bool { return !f.app.BackupStatus().Running })

	// And the refusal did not leave the state stuck.
	if err := f.app.CreateBackup("logical"); err != nil {
		t.Errorf("после завершения новая копия не запускается: %v", err)
	}
	waitFor(t, "окончания второй копии", func() bool { return !f.app.BackupStatus().Running })
}

func TestCreateBackupReportsAFailureInsteadOfPanicking(t *testing.T) {
	// A binding that panics takes the window down with the server still
	// running, which is the worst moment to lose the control panel.
	f := backupFixture(t)
	f.ops.OnRunTo = func(_ context.Context, _ string, _ []string, _ string, _ io.Writer) (string, error) {
		return "mysqldump: Got error: 2003: Can't connect to MySQL server", errors.New("exit status 2")
	}

	if err := f.app.CreateBackup("logical"); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	waitFor(t, "окончания копии", func() bool { return !f.app.BackupStatus().Running })

	st := f.app.BackupStatus()
	if st.Err == "" {
		t.Fatal("сбой не попал в состояние — панель покажет успех")
	}
	if !strings.Contains(st.Err, "Can't connect") {
		t.Errorf("в ошибке нет того, что сказал mysqldump: %q", st.Err)
	}
	if st.Dir != "" || st.SavedBytes != 0 {
		t.Errorf("состояние показывает результат, которого нет: %+v", st)
	}
	// And nothing was left behind for the next restore to trip over.
	items, err := os.ReadDir(f.lay.BackupsDir)
	if err != nil {
		t.Fatalf("чтение backups: %v", err)
	}
	for _, e := range items {
		if e.IsDir() {
			t.Errorf("после сбоя осталась папка %s", e.Name())
		}
	}
}

func TestCancelBackupStopsTheRun(t *testing.T) {
	f := backupFixture(t)
	atSecond := make(chan struct{})
	f.ops.OnRunTo = func(ctx context.Context, _ string, args []string, _ string, out io.Writer) (string, error) {
		if slices.Contains(args, repack.DBCharacters) {
			close(atSecond)
			// The real mysqldump dies when its context is cancelled.
			<-ctx.Done()
			return "", ctx.Err()
		}
		_, err := io.WriteString(out, logicalDumpBody)
		return "", err
	}

	if err := f.app.CreateBackup("logical"); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	<-atSecond
	f.app.CancelBackup()

	waitFor(t, "остановки копии", func() bool { return !f.app.BackupStatus().Running })

	st := f.app.BackupStatus()
	if !strings.Contains(st.Err, "отменено") {
		t.Errorf("Err = %q, want упоминание отмены", st.Err)
	}
	if st.Running {
		t.Error("Running остался true после отмены")
	}
}

func TestCancelBackupWithNothingRunningIsHarmless(t *testing.T) {
	// The button stays reachable for a moment after the work finished, and
	// answering that click with an error would report a problem that does
	// not exist.
	f := newAppFixture(t)
	f.app.CancelBackup()
	f.app.CancelBackup()

	if st := f.app.BackupStatus(); st.Running {
		t.Error("CancelBackup без работы изменил состояние")
	}
}

func TestCreateBackupRefreshesThePointListOnSuccess(t *testing.T) {
	// A new folder appeared under backups\, so the list on screen is stale
	// until it is re-read.
	f := backupFixture(t)

	f.app.refreshData(context.Background())
	if got := len(f.app.Data().Backups.Points); got != 0 {
		t.Fatalf("точек до копии: %d, want 0", got)
	}

	if err := f.app.CreateBackup("logical"); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	waitFor(t, "новой точки в списке", func() bool {
		return len(f.app.Data().Backups.Points) == 1
	})

	got := f.app.Data().Backups.Points[0]
	if got.Kind != backups.KindLogical {
		t.Errorf("Kind = %q, want %q", got.Kind, backups.KindLogical)
	}
	if len(got.Databases) != 4 {
		t.Errorf("Databases = %v, want все четыре", got.Databases)
	}
	if got.Note != "" {
		t.Errorf("Note = %q — инвентарь считает точку неполной", got.Note)
	}
}

// --- restoring --------------------------------------------------------------

// restoreFixture is a repack with a physical point and an archiver that can
// be told to fail, so the whole three-step path can be driven through the
// App's own binding.
func restoreFixture(t *testing.T) *appFixture {
	t.Helper()

	lay := realRepack(t)
	// The archiver is the repack's own path, not a path a test may choose:
	// restore.Check stats it before it will touch anything.
	sevenZip := filepath.Join(lay.Root, "7z.exe")
	if err := os.WriteFile(sevenZip, []byte("MZ"), 0o644); err != nil {
		t.Fatalf("write 7z: %v", err)
	}
	lay.SevenZipExe = sevenZip

	dir := filepath.Join(lay.BackupsDir, "20261002_121135_datadir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir point: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mysql_data.7z"), []byte("archive"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	f := newAppFixture(t, func(f *appFixture) { f.lay = lay })

	// The snapshot script names its own folder and the runner learns which one
	// by looking before and after, so the fake has to make a folder when it is
	// that script being run. Anything else - the 7z call - just succeeds.
	f.ops.OnRunTo = func(ctx context.Context, exe string, args []string, _ string, _ io.Writer) (string, error) {
		if !slices.Contains(args, "snapshot-mysql-data.cmd") {
			return "", nil
		}
		dir := filepath.Join(lay.BackupsDir, "20261002_130000_safety_datadir")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		return "", os.WriteFile(filepath.Join(dir, "mysql_data.7z"), []byte("archive"), 0o644)
	}
	return f
}

func TestRestoreSnapshotRefusesANameThatIsNotAPoint(t *testing.T) {
	// The name arrives from the page and becomes part of a path. Every one of
	// these has to be refused before anything runs.
	f := restoreFixture(t)

	for _, name := range []string{"", `..\..\windows`, `..\..\windows\system32`, "20261009_999999_datadir"} {
		if err := f.app.RestoreSnapshot(name); err == nil {
			t.Errorf("RestoreSnapshot(%q) согласился", name)
		}
	}
	if st := f.app.BackupStatus(); st.Running {
		t.Error("отказ всё равно запустил операцию")
	}
}

func TestRestoreSnapshotNamesTheSafetyCopyAndLeavesTheStackDown(t *testing.T) {
	f := restoreFixture(t)

	if err := f.app.RestoreSnapshot("20261002_121135_datadir"); err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}
	waitFor(t, "окончания восстановления", func() bool { return !f.app.BackupStatus().Running })

	st := f.app.BackupStatus()
	if st.Err != "" {
		t.Fatalf("Err = %q", st.Err)
	}
	if st.Kind != "restore" {
		t.Errorf("Kind = %q, want restore", st.Kind)
	}
	if st.Point != "20261002_121135_datadir" {
		t.Errorf("Point = %q, want имя папки", st.Point)
	}
	// The folder the page names on success is the safety copy: the state that
	// was replaced, and the one thing a person needs to undo the restore.
	if st.Dir == "" {
		t.Error("Dir пуст — панель не скажет, куда делась база, которая была до отката")
	}
	if st.Step != "" {
		t.Errorf("Step = %q после завершения", st.Step)
	}
	if st.Total != restore.Steps {
		t.Errorf("Total = %d, want %d", st.Total, restore.Steps)
	}
}

// A restore is the one operation that must not be cancellable. By the time it
// is running, the archive is being unpacked over mysql\data, and half of that
// is a state neither the panel nor the user can reason about.
func TestARestoreIsNotCancellable(t *testing.T) {
	f := restoreFixture(t)

	// Only the unpack is held open. The snapshot script before it has to run
	// normally, and the fixture's own handler is what makes its folder.
	safety := f.ops.OnRunTo
	release := make(chan struct{})
	f.ops.OnRunTo = func(ctx context.Context, exe string, args []string, dir string, out io.Writer) (string, error) {
		if slices.Contains(args, "snapshot-mysql-data.cmd") {
			return safety(ctx, exe, args, dir, out)
		}
		<-release
		return "", nil
	}

	if err := f.app.RestoreSnapshot("20261002_121135_datadir"); err != nil {
		t.Fatalf("RestoreSnapshot: %v", err)
	}
	waitFor(t, "начала распаковки", func() bool {
		return len(f.ops.RunToCalls) > 0
	})

	f.app.CancelBackup()
	close(release)

	waitFor(t, "окончания восстановления", func() bool { return !f.app.BackupStatus().Running })
	if st := f.app.BackupStatus(); st.Err != "" {
		t.Errorf("отмена остановила восстановление: %q", st.Err)
	}
}

// One operation at a time. A restore stops the stack and unpacks over its
// data folder; a backup starting alongside it would write into the same
// directory.
func TestRestoreSnapshotRefusesWhileABackupRuns(t *testing.T) {
	f := restoreFixture(t)

	// The backup goroutine has to get as far as its first dump for the refusal
	// to be about the race this test names. Without a port listening on 3306
	// it fails at checkLogical instead, and whether RestoreSnapshot still sees
	// the backup as running becomes a race between the goroutine and the next
	// line - which a slow CI runner loses.
	f.ops.Listening[3306] = true

	release := make(chan struct{})
	f.ops.OnRunTo = func(ctx context.Context, _ string, _ []string, _ string, out io.Writer) (string, error) {
		<-release
		_, err := io.WriteString(out, logicalDumpBody)
		return "", err
	}

	if err := f.app.CreateBackup("logical"); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if err := f.app.RestoreSnapshot("20261002_121135_datadir"); err == nil {
		t.Error("RestoreSnapshot согласился поверх идущей копии")
	}
	close(release)
	waitFor(t, "окончания копии", func() bool { return !f.app.BackupStatus().Running })
}

// --- deleting ---------------------------------------------------------------

// deleteFixture is a repack on disk with two copies in backups\, one of each
// kind the screen shows.
func deleteFixture(t *testing.T) (*appFixture, repack.Layout) {
	t.Helper()
	lay := realRepack(t)

	logical := filepath.Join(lay.BackupsDir, "2026-09-29_bots_current")
	if err := os.MkdirAll(logical, 0o755); err != nil {
		t.Fatalf("mkdir logical: %v", err)
	}
	for _, db := range repack.Databases {
		if err := os.WriteFile(filepath.Join(logical, db+".sql"), []byte(logicalDumpBody), 0o644); err != nil {
			t.Fatalf("write %s: %v", db, err)
		}
	}
	physical := filepath.Join(lay.BackupsDir, "20261002_121135_datadir")
	if err := os.MkdirAll(physical, 0o755); err != nil {
		t.Fatalf("mkdir physical: %v", err)
	}
	if err := os.WriteFile(filepath.Join(physical, "mysql_data.7z"), []byte("archive"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	return newAppFixture(t, func(f *appFixture) { f.lay = lay }), lay
}

// The point is gone from the disk and from the list on screen. A delete that
// only removed the folder would leave the row there until the next full
// refresh, which is exactly where a person clicks "Удалить" a second time.
func TestDeleteBackupRemovesThePointAndTakesItOffTheList(t *testing.T) {
	f, lay := deleteFixture(t)

	if err := f.app.DeleteBackup("2026-09-29_bots_current"); err != nil {
		t.Fatalf("DeleteBackup: %v", err)
	}
	if _, err := os.Stat(filepath.Join(lay.BackupsDir, "2026-09-29_bots_current")); !os.IsNotExist(err) {
		t.Errorf("папка копии осталась на диске (err = %v)", err)
	}

	waitFor(t, "обновления списка копий", func() bool {
		snap := f.app.Data()
		return len(snap.Backups.Points) > 0 && snap.Loaded &&
			snap.Backups.Points[0].Name == "20261002_121135_datadir"
	})
}

// A copy is a folder of files and it is gone for good, so the name has to be
// checked before anything is removed - including the case where the name would
// resolve to a real folder above backups\.
func TestDeleteBackupRefusesANameThatIsNotAPoint(t *testing.T) {
	f, lay := deleteFixture(t)

	// A sibling of backups\ with a copy-shaped folder in it: what a name that
	// escapes would find.
	outside := filepath.Join(lay.Root, "2026-09-29_bots_current")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	victim := filepath.Join(outside, "acore_world.sql")
	if err := os.WriteFile(victim, []byte(logicalDumpBody), 0o644); err != nil {
		t.Fatalf("write outside: %v", err)
	}

	for _, name := range []string{
		"", ".", "..",
		`..\2026-09-29_bots_current`,
		"../2026-09-29_bots_current",
		"20261009_999999_datadir",
		"README.md",
	} {
		if err := f.app.DeleteBackup(name); err == nil {
			t.Errorf("DeleteBackup(%q) согласился", name)
		}
	}

	if _, err := os.Stat(victim); err != nil {
		t.Errorf("отказ удалил папку над backups\\: %v", err)
	}
	if _, err := os.Stat(filepath.Join(lay.BackupsDir, "20261002_121135_datadir")); err != nil {
		t.Errorf("отказ тронул настоящую копию: %v", err)
	}
}

// A copy being made is being written into right now, and its folder exists
// from the first byte of it.
func TestDeleteBackupRefusesWhileABackupRuns(t *testing.T) {
	f, lay := deleteFixture(t)

	release := make(chan struct{})
	f.ops.OnRunTo = func(ctx context.Context, _ string, _ []string, _ string, out io.Writer) (string, error) {
		<-release
		_, err := io.WriteString(out, logicalDumpBody)
		return "", err
	}

	if err := f.app.CreateBackup("logical"); err != nil {
		t.Fatalf("CreateBackup: %v", err)
	}
	if err := f.app.DeleteBackup("20261002_121135_datadir"); err == nil {
		t.Error("DeleteBackup согласился поверх идущей копии")
	}
	close(release)
	waitFor(t, "окончания копии", func() bool { return !f.app.BackupStatus().Running })

	if _, err := os.Stat(filepath.Join(lay.BackupsDir, "20261002_121135_datadir")); err != nil {
		t.Errorf("копия исчезла, хотя удаление отказали: %v", err)
	}
}

// A delete is not a backup operation: it must not occupy the progress bar, or
// the screen would show a running job for the time it takes to erase 385 МБ -
// which is to say, nothing.
func TestDeleteBackupLeavesTheBackupStatusIdle(t *testing.T) {
	f, _ := deleteFixture(t)

	if err := f.app.DeleteBackup("20261002_121135_datadir"); err != nil {
		t.Fatalf("DeleteBackup: %v", err)
	}
	if st := f.app.BackupStatus(); st.Running {
		t.Errorf("удаление заняло полосу прогресса копий: %+v", st)
	}
}

func TestStepsOfGivesTheBarADenominator(t *testing.T) {
	if got := stepsOf(dumps.KindLogical); got != len(repack.Databases) {
		t.Errorf("stepsOf(logical) = %d, want %d", got, len(repack.Databases))
	}
	if got := stepsOf(dumps.KindPhysical); got != 1 {
		t.Errorf("stepsOf(physical) = %d, want 1", got)
	}
}

// --- the realm checklist ---------------------------------------------------

// setupItem finds one line of the checklist, failing when it is absent.
func setupItem(t *testing.T, items []SetupItem, key string) SetupItem {
	t.Helper()
	for _, it := range items {
		if it.Key == key {
			return it
		}
	}
	t.Fatalf("в чеклисте нет пункта %q: %+v", key, items)
	return SetupItem{}
}

func TestSetupCountsAnAdoptedProcessAsRunning(t *testing.T) {
	// A world that outlived the panel it was started from is StateExternal,
	// and it is up. Counting only StateRunning put this line at "1 из 3" with
	// the advice "нажмите «Запустить сервер»" in front of a person whose world
	// was already running - and the button does not start a second one. The
	// frontend counts external in upCount, so this is where the two used to
	// disagree on the same screen.
	//
	// setupServer reads a snapshot and nothing else, so it is tested as the
	// function it is rather than through the fixture.
	snap := func(states ...supervisor.State) supervisor.Snapshot {
		ids := []supervisor.ServiceID{supervisor.SvcMySQL, supervisor.SvcAuth, supervisor.SvcWorld}
		s := supervisor.Snapshot{}
		for i, id := range ids {
			if i >= len(states) {
				break
			}
			s.Services = append(s.Services, supervisor.ServiceStatus{ID: id, State: states[i]})
		}
		return s
	}

	cases := []struct {
		name   string
		states []supervisor.State
		wantOK bool
	}{
		{"все три наши", []supervisor.State{
			supervisor.StateRunning, supervisor.StateRunning, supervisor.StateRunning}, true},
		{"все три чужие - мир пережил панель", []supervisor.State{
			supervisor.StateExternal, supervisor.StateExternal, supervisor.StateExternal}, true},
		{"смесь: мир чужой, остальные наши", []supervisor.State{
			supervisor.StateRunning, supervisor.StateRunning, supervisor.StateExternal}, true},
		{"мир выключен", []supervisor.State{
			supervisor.StateRunning, supervisor.StateRunning, supervisor.StateStopped}, false},
		{"всё выключено", []supervisor.State{
			supervisor.StateStopped, supervisor.StateStopped, supervisor.StateStopped}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := setupServer(snap(c.states...))
			if got.OK != c.wantOK {
				t.Fatalf("OK = %v (%s), want %v", got.OK, got.Detail, c.wantOK)
			}
			if !got.OK && got.Fix == "" {
				t.Error("непрошедший пункт не говорит, что делать")
			}
		})
	}

	// A snapshot with nothing in it is not a realm that is up. Without the
	// total check, "0 == 0" reads as a passing line on an empty table.
	if got := setupServer(supervisor.Snapshot{}); got.OK {
		t.Errorf("пустой снимок прошёл как поднятый реалм: %+v", got)
	}
}

func TestSetupNamesTheMissingPiecesOfAPartialRepack(t *testing.T) {
	// A half-unpacked repack is the common failure. "Репак не найден"
	// would send the user looking in the wrong place.
	f := newAppFixture(t)

	got := setupItem(t, f.app.Setup(), "repack")
	if got.OK {
		t.Fatal("пункт прошёл, хотя маркеров репака нет на диске")
	}
	for _, want := range []string{"worldserver.exe", "configs", "dbc"} {
		if !strings.Contains(got.Detail, want) {
			t.Errorf("в подробностях нет %q: %q", want, got.Detail)
		}
	}
	if got.Fix == "" {
		t.Error("пункт не говорит, что делать")
	}
}

func TestSetupPassesTheRepackItemForACompleteRepack(t *testing.T) {
	f := newAppFixture(t, func(f *appFixture) { f.lay = realRepack(t) })

	got := setupItem(t, f.app.Setup(), "repack")
	if !got.OK {
		t.Errorf("пункт не прошёл на полном репаке: %+v", got)
	}
	if got.Fix != "" {
		t.Errorf("у прошедшего пункта есть совет: %q", got.Fix)
	}
}

func TestSetupDoesNotClaimTheDatabaseAnswersBeforeAnythingWasRead(t *testing.T) {
	// The trap this exists for: a zero-valued snapshot has an empty Err, so
	// a checklist that only looked at Err would show a green tick for a
	// database nobody has asked about yet.
	f := newAppFixture(t)

	got := setupItem(t, f.app.Setup(), "database")
	if got.OK {
		t.Errorf("пункт прошёл до первого чтения: %+v", got)
	}
	if !strings.Contains(got.Detail, "не прочитаны") {
		t.Errorf("подробности не объясняют причину: %q", got.Detail)
	}
}

func TestSetupReportsTheDatabaseOnceItHasBeenRead(t *testing.T) {
	f := newAppFixture(t)
	f.app.refreshData(context.Background())

	got := setupItem(t, f.app.Setup(), "database")
	// No pool in this fixture, so the read failed - and the checklist has
	// to say that rather than pass.
	if got.OK {
		t.Fatalf("пункт прошёл, хотя базы нет: %+v", got)
	}
	if got.Detail == "" || got.Fix == "" {
		t.Errorf("пункт ничего не объясняет: %+v", got)
	}
}

func TestSetupCallsAWeekOldPointStale(t *testing.T) {
	dir := tempDir(t)
	f := newAppFixture(t, func(f *appFixture) { f.lay.BackupsDir = dir })

	// Written by hand rather than by the runner, so the age is exact. The
	// name carries the date, and backups.Scan reads it from there.
	old := time.Now().Add(-9 * 24 * time.Hour).Format("20060102_150405")
	point := filepath.Join(dir, old+"_current")
	if err := os.Mkdir(point, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(point, repack.DBWorld+".sql"), []byte("-- dump"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	f.app.refreshData(context.Background())

	got := setupItem(t, f.app.Setup(), "rollback")
	if got.OK {
		t.Errorf("точка девятидневной давности сочтена свежей: %+v", got)
	}
	if !strings.Contains(got.Detail, "9 дней") {
		t.Errorf("подробности не называют возраст: %q", got.Detail)
	}
}

func TestSetupCallsARecentPointFresh(t *testing.T) {
	f := newAppFixture(t, func(f *appFixture) { f.lay.BackupsDir = backupsWithOnePoint(t) })
	f.app.refreshData(context.Background())

	got := setupItem(t, f.app.Setup(), "rollback")
	if !got.OK {
		t.Errorf("сегодняшняя точка сочтена старой: %+v", got)
	}
}

func TestSetupSaysWhenThereAreNoPointsAtAll(t *testing.T) {
	dir := tempDir(t)
	f := newAppFixture(t, func(f *appFixture) { f.lay.BackupsDir = dir })
	f.app.refreshData(context.Background())

	got := setupItem(t, f.app.Setup(), "rollback")
	if got.OK {
		t.Fatal("пункт прошёл без единой точки отката")
	}
	if !strings.Contains(got.Detail, "нет") {
		t.Errorf("подробности не говорят, что точек нет: %q", got.Detail)
	}
}

func TestSetupWarnsWhenTheNextPointWouldNotFit(t *testing.T) {
	dir := tempDir(t)
	f := newAppFixture(t, func(f *appFixture) {
		f.lay.BackupsDir = dir
		// Below minFreeForBackup, and set on the fake so the scan reads it.
		f.ops.DiskFreeBytes = 200 << 20
		f.ops.DiskTotalBytes = 500 << 30
	})
	f.app.refreshData(context.Background())

	got := setupItem(t, f.app.Setup(), "space")
	if got.OK {
		t.Errorf("пункт прошёл при 200 МБ свободного места: %+v", got)
	}
	if !strings.Contains(got.Detail, "200") {
		t.Errorf("подробности не называют свободное место: %q", got.Detail)
	}
}

func TestSetupCountsTheRunningServices(t *testing.T) {
	f := newAppFixture(t)

	got := setupItem(t, f.app.Setup(), "server")
	if got.OK {
		t.Errorf("пункт прошёл, хотя ничего не запущено: %+v", got)
	}
	if !strings.Contains(got.Detail, "0 из 3") {
		t.Errorf("подробности не называют счёт: %q", got.Detail)
	}
}

func TestOpenFolderOnlyOpensKnownFolders(t *testing.T) {
	// The page can send any string. A binding that opened whatever it was
	// handed would let the app open anything on the disk.
	f := newAppFixture(t, func(f *appFixture) { f.lay = realRepack(t) })

	if err := f.app.OpenFolder("C:\\Windows\\System32"); err == nil {
		t.Fatal("OpenFolder принял произвольный путь")
	}
	if len(f.ops.OpenCalls) != 0 {
		t.Errorf("что-то открылось: %v", f.ops.OpenCalls)
	}

	for _, which := range []string{"backups", "logs", "root", "configs"} {
		if err := f.app.OpenFolder(which); err != nil {
			t.Errorf("OpenFolder(%q): %v", which, err)
		}
	}
	if len(f.ops.OpenCalls) != 4 {
		t.Errorf("открытий: %d, want 4", len(f.ops.OpenCalls))
	}
	if got := f.ops.OpenCalls[0]; got != f.lay.BackupsDir {
		t.Errorf("первым открылся %q, want %q", got, f.lay.BackupsDir)
	}
}

func TestOpenFolderSaysSoWhenThereIsNoRepack(t *testing.T) {
	app := NewApp(Deps{})

	if err := app.OpenFolder("backups"); err == nil {
		t.Fatal("OpenFolder прошёл без репака")
	}
}

func TestHumanBytesUsesTheLargestUnitThatFits(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{512, "512 Б"},
		{2048, "2,0 КБ"},
		{5 << 20, "5,0 МБ"},
		{3 << 30, "3,0 ГБ"},
	}
	for _, tc := range cases {
		if got := humanBytes(tc.in); got != tc.want {
			t.Errorf("humanBytes(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHumanDurationPicksTheRussianForm(t *testing.T) {
	// "5 дней" for everything is what a program written in English first
	// does, and it reads as one.
	cases := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "меньше минуты"},
		{time.Minute, "1 минуту"},
		{3 * time.Minute, "3 минуты"},
		{11 * time.Minute, "11 минут"},
		{22 * time.Minute, "22 минуты"},
		{time.Hour, "1 час"},
		{4 * time.Hour, "4 часа"},
		{25 * time.Hour, "1 день"},
		{50 * time.Hour, "2 дня"},
		{9 * 24 * time.Hour, "9 дней"},
		{21 * 24 * time.Hour, "21 день"},
	}
	for _, tc := range cases {
		if got := humanDuration(tc.in); got != tc.want {
			t.Errorf("humanDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Every exported method on App is a Wails binding, and Wails marshals
// arguments in from JSON and results out to JSON. Two whole classes of
// signature pass the compiler and fail at the only moment that matters,
// which is the click:
//
//   - a context.Context parameter, which becomes a positional argument the
//     page is asked to supply. Health took one, the page passed an
//     AbortSignal, and every call died with "json: cannot unmarshal object
//     into Go value of type context.Context".
//   - a channel or func result, which JSON cannot represent. AskConfirm
//     returned one, and because the frontend never calls it the fault stayed
//     asleep until something did.
//
// Checked by reflection over the bound type rather than by convention,
// because neither mistake announces itself in a build.
// The bound type is AppService, not App: Wails v3 binds registered services,
// and only AppService is registered. App keeps its exported methods because
// the supervisor and the tests call them, and none of them is a binding any
// more - so a signature here is not automatically a page-facing one.
func TestNoBoundMethodHasAnUnserialisableSignature(t *testing.T) {
	ctxType := reflect.TypeOf((*context.Context)(nil)).Elem()

	typ := reflect.TypeOf(&AppService{})
	for i := 0; i < typ.NumMethod(); i++ {
		m := typ.Method(i)
		if !m.IsExported() {
			continue
		}
		ft := m.Type

		// In(0) is the receiver, so a real first parameter is In(1).
		if ft.NumIn() > 1 && ft.In(1) == ctxType {
			t.Errorf("AppService.%s takes a context.Context; the generated binding will "+
				"demand one from JavaScript and every call will fail to unmarshal", m.Name)
		}
		for j := 0; j < ft.NumIn(); j++ {
			if k := ft.In(j).Kind(); k == reflect.Chan || k == reflect.Func {
				t.Errorf("AppService.%s takes an unserialisable argument at position %d; "+
					"JSON cannot carry it to the page", m.Name, j)
			}
		}
		// RespondConfirm returns nothing, which Wails is happy with.
		if ft.NumOut() == 0 {
			continue
		}
		if k := ft.Out(0).Kind(); k == reflect.Chan || k == reflect.Func {
			t.Errorf("AppService.%s returns a %s, which JSON cannot marshal; "+
				"the binding fails if the page ever calls it", m.Name, ft.Out(0))
		}
	}
}

/* ------------------------------ расписание ------------------------------ */

// waitForBackup lets a point that was just started finish.
//
// A test that ends while the runner is still writing into its fixture is a
// test that fails somewhere else, later, for a reason that has nothing to do
// with what it was checking.
func waitForBackup(t *testing.T, f *appFixture) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !f.app.BackupStatus().Running {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("точка отката не завершилась за 10 секунд")
}

// A fresh install must do nothing until asked. Turning a reminder on is a
// decision, and only a decision the user made survives being wrong.
func TestScheduleStartsOff(t *testing.T) {
	f := backupFixture(t)

	s := f.app.Schedule()
	if s.Enabled || s.AutoRun {
		t.Error("расписание включено на свежей установке")
	}
	if s.EveryDays != settings.DefaultDays {
		t.Errorf("интервал %d, ожидался %d", s.EveryDays, settings.DefaultDays)
	}
	if s.Due {
		t.Error("выключенное расписание считает себя должным")
	}
	if !s.LastRun.IsZero() {
		t.Errorf("LastRun = %v, ожидался нулевой", s.LastRun)
	}
	// A missing settings file is the normal state, not a fault to report.
	if s.Err != "" {
		t.Errorf("отсутствие файла настроек сообщено как ошибка: %s", s.Err)
	}
}

func TestSetScheduleStoresAndReportsIt(t *testing.T) {
	f := backupFixture(t)

	if err := f.app.SetSchedule(true, 3, true); err != nil {
		t.Fatalf("сохранение расписания: %v", err)
	}

	s := f.app.Schedule()
	if !s.Enabled || !s.AutoRun || s.EveryDays != 3 {
		t.Errorf("получено %+v, ожидалось включённое на 3 дня с автосозданием", s)
	}
	if s.Err != "" {
		t.Errorf("ошибка настроек после успешного сохранения: %s", s.Err)
	}
}

func TestSetScheduleReachesTheFile(t *testing.T) {
	f := backupFixture(t)

	if err := f.app.SetSchedule(true, 2, false); err != nil {
		t.Fatalf("сохранение: %v", err)
	}

	// Reading the file back is what the next start does.
	s, err := settings.Load(f.lay.SettingsFile)
	if err != nil {
		t.Fatalf("чтение файла настроек: %v", err)
	}
	if !s.Backup.Enabled || s.Backup.EveryDays != 2 {
		t.Errorf("в файле %+v", s.Backup)
	}
}

func TestAFreshAppPicksUpTheStoredSchedule(t *testing.T) {
	f := backupFixture(t)
	if err := f.app.SetSchedule(true, 5, true); err != nil {
		t.Fatalf("сохранение: %v", err)
	}

	again := newAppFixture(t, func(g *appFixture) { g.lay = f.lay })

	s := again.app.Schedule()
	if !s.Enabled || !s.AutoRun || s.EveryDays != 5 {
		t.Errorf("после перезапуска получено %+v", s)
	}
}

// 0 would make the schedule permanently due and 99999 is a number nobody
// chose; both are reachable by typing into the box.
func TestSetScheduleClampsAnIntervalThatMeansNothing(t *testing.T) {
	f := backupFixture(t)

	cases := []struct {
		name string
		in   int
		want int
	}{
		{"ноль", 0, settings.DefaultDays},
		{"отрицательный", -4, settings.DefaultDays},
		{"слишком большой", 99999, settings.MaxDays},
		{"обычный", 3, 3},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := f.app.SetSchedule(true, c.in, false); err != nil {
				t.Fatalf("сохранение: %v", err)
			}
			if got := f.app.Schedule().EveryDays; got != c.want {
				t.Errorf("интервал %d дал %d, ожидался %d", c.in, got, c.want)
			}
		})
	}
}

func TestSetScheduleTellsThePage(t *testing.T) {
	f := backupFixture(t)
	before := f.sink.CountOf(notify.EventSchedule)

	if err := f.app.SetSchedule(true, 7, false); err != nil {
		t.Fatalf("сохранение: %v", err)
	}

	if got := f.sink.CountOf(notify.EventSchedule); got != before+1 {
		t.Errorf("событий расписания %d, ожидалось %d", got, before+1)
	}
}

// An enabled schedule with no point ever made is due at once: there is
// nothing to measure an interval from.
func TestAnEnabledScheduleWithNoPointsIsDueAtOnce(t *testing.T) {
	f := backupFixture(t)

	if err := f.app.SetSchedule(true, 7, false); err != nil {
		t.Fatalf("сохранение: %v", err)
	}
	if !f.app.Schedule().Due {
		t.Fatal("включённое расписание без единой точки не считает себя должным")
	}
}

func TestSnoozeDefersTheReminder(t *testing.T) {
	f := backupFixture(t)

	if err := f.app.SetSchedule(true, 7, false); err != nil {
		t.Fatalf("сохранение: %v", err)
	}
	if !f.app.Schedule().Due {
		t.Fatal("подготовка: расписание не считает себя должным")
	}

	if err := f.app.SnoozeSchedule(1); err != nil {
		t.Fatalf("отложение: %v", err)
	}

	s := f.app.Schedule()
	if s.Due {
		t.Error("после отложения напоминание всё ещё срабатывает")
	}
	if s.DaysLeft != 1 {
		t.Errorf("DaysLeft = %d, ожидался 1", s.DaysLeft)
	}

	// And it comes back on its own, without the interval being touched.
	f.clk.Fire(25 * time.Hour)
	if !f.app.Schedule().Due {
		t.Error("после отложения напоминание не вернулось")
	}
}

func TestASuccessfulBackupMovesTheDueDate(t *testing.T) {
	f := backupFixture(t)

	if err := f.app.SetSchedule(true, 1, false); err != nil {
		t.Fatalf("сохранение: %v", err)
	}

	f.app.finishBackup(dumps.KindLogical, dumps.Report{
		Dir: `C:/repack/backups/20260930_120000_current`, Bytes: 1024,
	}, nil)

	s := f.app.Schedule()
	if s.Due {
		t.Error("успешная точка не сдвинула срок")
	}
	if s.LastRun.IsZero() {
		t.Error("время точки не запомнилось")
	}
	if s.DaysLeft != 1 {
		t.Errorf("DaysLeft = %d, ожидался 1", s.DaysLeft)
	}
}

// A failure must not move the due date. If it did, a dump that never
// happened would silence the reminder for a week.
func TestAFailedBackupLeavesTheReminderOn(t *testing.T) {
	f := backupFixture(t)

	if err := f.app.SetSchedule(true, 1, false); err != nil {
		t.Fatalf("сохранение: %v", err)
	}

	f.app.finishBackup(dumps.KindLogical, dumps.Report{}, errors.New("на диске нет места"))

	s := f.app.Schedule()
	if !s.Due {
		t.Error("неудачная точка сдвинула срок: напоминание замолчало")
	}
	if !s.LastRun.IsZero() {
		t.Errorf("неудачная точка записалась как состоявшаяся: %v", s.LastRun)
	}
}

// The checklist and the reminder must not disagree about the same folder.
func TestTheChecklistThresholdFollowsTheSchedule(t *testing.T) {
	f := backupFixture(t)

	if got := f.app.rollbackStaleAfter(); got != backupStaleAfter {
		t.Errorf("без расписания порог %v, ожидался %v", got, backupStaleAfter)
	}

	if err := f.app.SetSchedule(true, 2, false); err != nil {
		t.Fatalf("сохранение: %v", err)
	}
	if got := f.app.rollbackStaleAfter(); got != 48*time.Hour {
		t.Errorf("с расписанием на 2 дня порог %v, ожидался 48h", got)
	}

	// Switched off: back to the fallback, not to the last interval.
	if err := f.app.SetSchedule(false, 2, false); err != nil {
		t.Fatalf("сохранение: %v", err)
	}
	if got := f.app.rollbackStaleAfter(); got != backupStaleAfter {
		t.Errorf("с выключенным расписанием порог %v, ожидался %v", got, backupStaleAfter)
	}
}

func TestTheSchedulerDoesNothingWhileAutoRunIsOff(t *testing.T) {
	f := backupFixture(t)

	if err := f.app.SetSchedule(true, 1, false); err != nil {
		t.Fatalf("сохранение: %v", err)
	}
	f.app.checkSchedule()

	if f.app.BackupStatus().Running {
		t.Error("планировщик начал точку при выключенном автосоздании")
	}
}

func TestTheSchedulerDoesNothingWhileTheReminderIsOff(t *testing.T) {
	f := backupFixture(t)

	if err := f.app.SetSchedule(false, 1, true); err != nil {
		t.Fatalf("сохранение: %v", err)
	}
	f.app.checkSchedule()

	if f.app.BackupStatus().Running {
		t.Error("планировщик начал точку при выключенном напоминании")
	}
}

func TestTheSchedulerStartsAPointWhenItIsDue(t *testing.T) {
	f := backupFixture(t)

	if err := f.app.SetSchedule(true, 1, true); err != nil {
		t.Fatalf("сохранение: %v", err)
	}
	f.app.checkSchedule()

	// CreateBackup sets Running before it starts the goroutine, so this is
	// already true the moment checkSchedule returns.
	if !f.app.BackupStatus().Running {
		t.Fatal("планировщик не начал точку, хотя она назрела")
	}
	waitForBackup(t, f)
}

// The status loop runs once a second, so without the retry guard one failure
// would become a thousand attempts, each writing a partial file first.
func TestTheSchedulerDoesNotRetryRightAfterAFailure(t *testing.T) {
	f := backupFixture(t)

	if err := f.app.SetSchedule(true, 1, true); err != nil {
		t.Fatalf("сохранение: %v", err)
	}

	// The stamp a failed automatic attempt leaves behind.
	f.app.recordBackupAttempt()
	f.app.checkSchedule()

	if f.app.BackupStatus().Running {
		t.Error("планировщик повторил попытку сразу после неудачи")
	}

	// And it does try again once the retry window has passed.
	f.clk.Fire(7 * time.Hour)
	f.app.checkSchedule()
	if !f.app.BackupStatus().Running {
		t.Error("через семь часов планировщик не попробовал снова")
	}
	waitForBackup(t, f)
}

func TestTheSchedulerWaitsForMySQL(t *testing.T) {
	f := backupFixture(t)

	if err := f.app.SetSchedule(true, 1, true); err != nil {
		t.Fatalf("сохранение: %v", err)
	}

	// Nothing listening on 3306: there is nothing to dump into, and there is
	// no point queuing one.
	f.ops.Listening[repack.MySQLPort] = false
	f.app.tick(context.Background())

	if f.app.BackupStatus().Running {
		t.Error("планировщик начал точку без работающего MySQL")
	}
}
