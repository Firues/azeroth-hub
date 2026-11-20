package dumps

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"wow-hub/internal/i18n"
	"wow-hub/internal/osops"
	"wow-hub/internal/repack"
)

// dumpBody is what a successful mysqldump leaves behind, trailer and all.
const dumpBody = "DROP TABLE IF EXISTS `account`;\nCREATE TABLE `account` (id int);\n" +
	"-- Dump completed on 2026-09-30 12:00:00\n"

// testRoot is one directory for the whole package, removed once.
//
// t.TempDir() is deliberately not used. Measured on this machine, removing
// one test directory costs about 1.3 seconds however few files are in it -
// the antivirus holds a handle on the files just written and Windows makes
// RemoveAll wait for it - against 12 milliseconds to create the directory
// and write four files into it. Across this package that is half a minute
// of waiting for a directory nobody looks at, and none of it is time the
// code under test spends.
//
// The probe: MkdirTemp 2ms, MkdirAll + four WriteFile 12ms, RemoveAll
// 1276ms.
var testRoot string

func TestMain(m *testing.M) {
	// The assertions below pin exact Russian wording, which was the panel's
	// only language when they were written. The process-wide language defaults
	// to English now, so the tests choose theirs explicitly instead of
	// inheriting whatever the OS speaks.
	i18n.Set(i18n.Ru)

	dir, err := os.MkdirTemp("", "wow-hub-dumps")
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

// fixture is a repack on disk, a fake operating system, and a stopped clock.
//
// The layout comes from repack.LocateFrom against a real directory tree
// rather than from a hand-built struct, so the paths under test are the
// ones the app will actually use. A hand-built Layout would keep passing
// after somebody changed newLayout.
type fixture struct {
	t       *testing.T
	root    string
	backups string
	lay     repack.Layout
	ops     *osops.FakeOps
	clock   *osops.FakeClock
	runner  *Runner
	seen    []Progress
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := tempDir(t)

	// The three markers repack.LocateFrom insists on, plus everything the
	// dump path touches.
	for _, dir := range []string{
		filepath.Join(root, "configs"),
		filepath.Join(root, "data", "dbc"),
		filepath.Join(root, "mysql", "bin"),
		filepath.Join(root, "temp"),
		filepath.Join(root, "backups"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("создание %s: %v", dir, err)
		}
	}
	write(t, filepath.Join(root, "worldserver.exe"), "")
	write(t, filepath.Join(root, "mysql", "bin", "mysqldump.exe"), "")
	write(t, filepath.Join(root, "temp", "mysql_ac.conf"), "[client]\npassword = \"acore\"\n")
	write(t, filepath.Join(root, "backups", snapshotScript), "@echo off\n")

	lay, err := repack.LocateFrom(root)
	if err != nil {
		t.Fatalf("repack.LocateFrom(%s): %v", root, err)
	}

	f := &fixture{
		t:       t,
		root:    root,
		backups: lay.BackupsDir,
		lay:     lay,
		ops:     osops.NewFake(),
		clock:   osops.NewFakeClock(),
	}
	f.ops.Listening[repack.MySQLPort] = true
	f.ops.RunToBody = dumpBody
	f.runner = New(f.ops, lay, f.clock, func(p Progress) { f.seen = append(f.seen, p) })
	return f
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("запись %s: %v", path, err)
	}
}

// entries lists the names in the backups folder.
func (f *fixture) entries() []string {
	f.t.Helper()
	items, err := os.ReadDir(f.backups)
	if err != nil {
		f.t.Fatalf("чтение %s: %v", f.backups, err)
	}
	var names []string
	for _, e := range items {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// only returns the single folder in backups, failing when there is not
// exactly one.
func (f *fixture) only() string {
	f.t.Helper()
	names := f.entries()
	var folders []string
	for _, n := range names {
		if n != snapshotScript {
			folders = append(folders, n)
		}
	}
	if len(folders) != 1 {
		f.t.Fatalf("в backups %d папок, ожидалась одна: %v", len(folders), names)
	}
	return filepath.Join(f.backups, folders[0])
}

func (f *fixture) read(name string) string {
	f.t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		f.t.Fatalf("чтение %s: %v", name, err)
	}
	return string(b)
}

// --- the logical point -----------------------------------------------------

func TestLogicalWritesOneVerifiedDumpPerDatabase(t *testing.T) {
	f := newFixture(t)

	rep, err := f.runner.Logical(context.Background())
	if err != nil {
		t.Fatalf("Logical: %v", err)
	}

	if rep.Kind != KindLogical {
		t.Errorf("Kind = %q, want %q", rep.Kind, KindLogical)
	}
	if !slices.Equal(rep.Databases, repack.Databases) {
		t.Errorf("Databases = %v, want %v", rep.Databases, repack.Databases)
	}
	if len(rep.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", rep.Warnings)
	}

	// The folder must hold exactly the four files restore-all-db.cmd looks
	// for, and nothing else: that script imports <db>.sql by name and skips
	// whatever is missing.
	dir := f.only()
	items, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("чтение %s: %v", dir, err)
	}
	var got []string
	for _, e := range items {
		got = append(got, e.Name())
	}
	slices.Sort(got)
	want := []string{"acore_auth.sql", "acore_characters.sql", "acore_playerbots.sql", "acore_world.sql"}
	if !slices.Equal(got, want) {
		t.Errorf("содержимое папки = %v, want %v", got, want)
	}
	if !strings.HasSuffix(f.read(filepath.Join(dir, "acore_world.sql")), dumpBody) {
		t.Error("дамп не содержит тела, которое записал mysqldump")
	}
	if rep.Bytes != int64(len(dumpBody))*4 {
		t.Errorf("Bytes = %d, want %d", rep.Bytes, int64(len(dumpBody))*4)
	}
}

func TestLogicalNamesTheFolderSoTheInventoryCanDateIt(t *testing.T) {
	f := newFixture(t)

	rep, err := f.runner.Logical(context.Background())
	if err != nil {
		t.Fatalf("Logical: %v", err)
	}

	// The fake clock sits at 2026-09-29 12:00 UTC, and the name is built in
	// local time, so only the shape is asserted. What matters is that the
	// first fifteen characters parse as backups.describe's stampWithTime
	// layout - otherwise the point shows up in the list dated midnight.
	name := filepath.Base(rep.Dir)
	if !strings.HasSuffix(name, LogicalSuffix) {
		t.Errorf("имя %q не кончается на %q", name, LogicalSuffix)
	}
	if _, err := time.ParseInLocation(stampLayout, name[:len(stampLayout)], time.Local); err != nil {
		t.Errorf("имя %q не начинается с отметки времени %q: %v", name, stampLayout, err)
	}
}

func TestLogicalNeverWritesIntoAnExistingFolder(t *testing.T) {
	f := newFixture(t)

	first, err := f.runner.Logical(context.Background())
	if err != nil {
		t.Fatalf("первый Logical: %v", err)
	}
	// Poison the first point, so overwriting it would be visible.
	write(t, filepath.Join(first.Dir, "acore_auth.sql"), "ORIGINAL")

	second, err := f.runner.Logical(context.Background())
	if err != nil {
		t.Fatalf("второй Logical: %v", err)
	}
	if second.Dir == first.Dir {
		t.Fatalf("второй дамп записан в ту же папку %s", first.Dir)
	}
	if got := f.read(filepath.Join(first.Dir, "acore_auth.sql")); got != "ORIGINAL" {
		t.Errorf("первая точка отката перезаписана: %q", got)
	}
}

func TestLogicalCollisionNameStillEndsWithTheSuffix(t *testing.T) {
	f := newFixture(t)

	if _, err := f.runner.Logical(context.Background()); err != nil {
		t.Fatalf("первый Logical: %v", err)
	}
	second, err := f.runner.Logical(context.Background())
	if err != nil {
		t.Fatalf("второй Logical: %v", err)
	}

	// The counter has to sit before the suffix, not after it. A name that
	// stopped ending in _current would be one removable refuses, so a
	// collision that then failed would leave a partial point on disk -
	// the exact thing cleanup exists to prevent.
	if !removable(second.Dir, f.backups, KindLogical) {
		t.Errorf("имя %q не распознаётся cleanup — при сбое такую папку не удалят",
			filepath.Base(second.Dir))
	}
}

func TestLogicalUsesTheCommandLineThatWasVerifiedAgainstTheRepack(t *testing.T) {
	f := newFixture(t)

	if _, err := f.runner.Logical(context.Background()); err != nil {
		t.Fatalf("Logical: %v", err)
	}

	if len(f.ops.RunToCalls) != 4 {
		t.Fatalf("запусков mysqldump: %d, ожидалось 4", len(f.ops.RunToCalls))
	}
	first := f.ops.RunToCalls[0]
	if first.Exe != f.lay.MySQLdumpExe {
		t.Errorf("Exe = %q, want %q", first.Exe, f.lay.MySQLdumpExe)
	}
	// --defaults-file has to be first; mysqldump rejects it anywhere else.
	if want := "--defaults-file=" + f.lay.MySQLPassConf; first.Args[0] != want {
		t.Errorf("Args[0] = %q, want %q", first.Args[0], want)
	}
	for _, flag := range []string{
		"--single-transaction", "--skip-lock-tables",
		"--no-tablespaces", "--routines", "--default-character-set=utf8mb4",
	} {
		if !slices.Contains(first.Args, flag) {
			t.Errorf("в командной строке нет %s: %v", flag, first.Args)
		}
	}
	if slices.Contains(first.Args, "--databases") || slices.Contains(first.Args, "--all-databases") {
		t.Error("--databases добавит CREATE DATABASE и USE, и файл перестанет импортироваться restore-all-db.cmd")
	}
	if last := first.Args[len(first.Args)-1]; last != repack.DBWorld {
		t.Errorf("последний аргумент = %q, want %q — схема передаётся позиционно", last, repack.DBWorld)
	}
	if !slices.Contains(first.Args, repack.DefaultMySQLUser) {
		t.Errorf("в командной строке нет пользователя %q: %v", repack.DefaultMySQLUser, first.Args)
	}
}

func TestLogicalReportsProgressForEveryDatabase(t *testing.T) {
	f := newFixture(t)

	if _, err := f.runner.Logical(context.Background()); err != nil {
		t.Fatalf("Logical: %v", err)
	}

	if len(f.seen) != 4 {
		t.Fatalf("отчётов о ходе: %d, ожидалось 4: %v", len(f.seen), f.seen)
	}
	for i, p := range f.seen {
		if p.Kind != KindLogical {
			t.Errorf("отчёт %d: Kind = %q", i, p.Kind)
		}
		if p.Index != i+1 || p.Total != 4 {
			t.Errorf("отчёт %d: Index/Total = %d/%d, want %d/4", i, p.Index, p.Total, i+1)
		}
		if p.Step != repack.Databases[i] {
			t.Errorf("отчёт %d: Step = %q, want %q", i, p.Step, repack.Databases[i])
		}
		if p.Bytes <= 0 {
			t.Errorf("отчёт %d: Bytes = %d, ожидалось больше нуля", i, p.Bytes)
		}
	}
}

func TestLogicalRefusesWhileMySQLIsNotListening(t *testing.T) {
	f := newFixture(t)
	f.ops.Listening[repack.MySQLPort] = false

	_, err := f.runner.Logical(context.Background())
	if err == nil {
		t.Fatal("Logical прошёл при выключенной базе")
	}
	if !strings.Contains(err.Error(), "3306") {
		t.Errorf("в ошибке нет порта: %v", err)
	}
	if len(f.ops.RunToCalls) != 0 {
		t.Error("mysqldump был запущен, хотя база не отвечает")
	}
	if names := f.entries(); len(names) != 1 {
		t.Errorf("в backups осталось %v, ожидался только скрипт", names)
	}
}

func TestLogicalRefusesWithoutMysqldump(t *testing.T) {
	f := newFixture(t)
	if err := os.Remove(f.lay.MySQLdumpExe); err != nil {
		t.Fatalf("удаление mysqldump: %v", err)
	}

	_, err := f.runner.Logical(context.Background())
	if err == nil {
		t.Fatal("Logical прошёл без mysqldump")
	}
	if !strings.Contains(err.Error(), "mysqldump") {
		t.Errorf("в ошибке не назван mysqldump: %v", err)
	}
}

func TestLogicalSaysSoWhenThereIsNoRepack(t *testing.T) {
	f := newFixture(t)
	f.lay = repack.Layout{}
	f.runner = New(f.ops, f.lay, f.clock, nil)

	_, err := f.runner.Logical(context.Background())
	if err == nil {
		t.Fatal("Logical прошёл без известного корня репака")
	}
	if !strings.Contains(err.Error(), "репак") {
		t.Errorf("в ошибке нет упоминания репака: %v", err)
	}
}

func TestLogicalRemovesTheWholeFolderWhenADumpFails(t *testing.T) {
	f := newFixture(t)
	f.ops.OnRunTo = func(_ context.Context, _ string, args []string, _ string, out io.Writer) (string, error) {
		if slices.Contains(args, repack.DBCharacters) {
			return "mysqldump: Got error: 2003: Can't connect", errors.New("exit status 2")
		}
		// The databases before the failure produce real dumps, so the
		// folder is non-empty and holds valid points when the failure
		// happens - which is exactly the state that must not survive.
		_, err := io.WriteString(out, dumpBody)
		return "", err
	}

	_, err := f.runner.Logical(context.Background())
	if err == nil {
		t.Fatal("Logical прошёл, хотя второй дамп не удался")
	}
	if !strings.Contains(err.Error(), repack.DBCharacters) {
		t.Errorf("в ошибке не названа упавшая база: %v", err)
	}
	if !strings.Contains(err.Error(), "Can't connect") {
		t.Errorf("в ошибке нет того, что сказал mysqldump: %v", err)
	}

	// The point of the test: three of four dumps must not be left behind.
	// restore-all-db.cmd would import them, skip the missing one and
	// report success, leaving world and characters from different days.
	if names := f.entries(); len(names) != 1 {
		t.Errorf("неполная точка отката осталась на диске: %v", names)
	}
}

func TestLogicalRemovesTheFolderWhenADumpIsTruncated(t *testing.T) {
	f := newFixture(t)
	// A dump that stops without the trailer, which is what a full disk or
	// a killed process leaves behind.
	f.ops.RunToBody = "DROP TABLE IF EXISTS `account`;\nCREATE TABLE `account` (id int);\n"

	_, err := f.runner.Logical(context.Background())
	if err == nil {
		t.Fatal("Logical принял оборванный дамп")
	}
	if !strings.Contains(err.Error(), "оборван") {
		t.Errorf("ошибка не говорит об обрыве: %v", err)
	}
	if names := f.entries(); len(names) != 1 {
		t.Errorf("оборванная точка отката осталась на диске: %v", names)
	}
}

func TestLogicalTreatsAWarningOnStderrAsAWarning(t *testing.T) {
	f := newFixture(t)
	// mysqldump 8.4 writes warnings to stderr and still exits 0 with a
	// complete dump. Reporting that as a failure would throw away a good
	// backup; hiding it entirely would hide a real one.
	f.ops.OnRunTo = func(_ context.Context, _ string, _ []string, _ string, out io.Writer) (string, error) {
		if _, err := io.WriteString(out, dumpBody); err != nil {
			return "", err
		}
		return "mysqldump: [Warning] Using a password on the command line", nil
	}

	rep, err := f.runner.Logical(context.Background())
	if err != nil {
		t.Fatalf("Logical: %v", err)
	}
	if len(rep.Warnings) != 4 {
		t.Fatalf("Warnings = %v, ожидалось четыре", rep.Warnings)
	}
	if !strings.Contains(rep.Warnings[0], "Using a password") {
		t.Errorf("предупреждение потеряло текст mysqldump: %q", rep.Warnings[0])
	}
}

func TestLogicalStopsAndCleansUpWhenCancelled(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())

	f.ops.OnRunTo = func(_ context.Context, _ string, args []string, _ string, out io.Writer) (string, error) {
		if slices.Contains(args, repack.DBCharacters) {
			// Cancel while the second database is being dumped, which is
			// where a user pressing the button would land.
			cancel()
			return "", context.Canceled
		}
		_, err := io.WriteString(out, dumpBody)
		return "", err
	}

	_, err := f.runner.Logical(ctx)
	if err == nil {
		t.Fatal("Logical прошёл после отмены")
	}
	if !strings.Contains(err.Error(), "отменено") {
		t.Errorf("ошибка не говорит об отмене: %v", err)
	}
	if names := f.entries(); len(names) != 1 {
		t.Errorf("после отмены осталась папка: %v", names)
	}
}

// --- the physical snapshot -------------------------------------------------

func TestPhysicalRunsTheRepackScriptFromTheBackupsFolder(t *testing.T) {
	f := newFixture(t)
	script := filepath.Join(f.backups, snapshotScript)
	f.ops.OnRunTo = func(_ context.Context, exe string, args []string, _ string, out io.Writer) (string, error) {
		// The script makes its own folder; model that.
		dir := filepath.Join(f.backups, "20260930_120000"+PhysicalSuffix)
		if err := os.Mkdir(dir, 0o755); err != nil {
			return "", err
		}
		write(t, filepath.Join(dir, ArchiveName), "7z archive bytes")
		if _, err := io.WriteString(out, "OK: 131000000 bytes\n"); err != nil {
			return "", err
		}
		return "", nil
	}

	rep, err := f.runner.Physical(context.Background())
	if err != nil {
		t.Fatalf("Physical: %v", err)
	}

	call := f.ops.RunToCalls[0]
	if call.Exe != osops.CmdExe() {
		t.Errorf("Exe = %q, want %q", call.Exe, osops.CmdExe())
	}
	// The script is named bare and started from backups\ on purpose: the
	// repack folder name contains parentheses, and cmd.exe mangles a
	// quoted path containing them. The script resolves the root from %~dp0.
	if !slices.Equal(call.Args, []string{"/c", snapshotScript}) {
		t.Errorf("Args = %v, want [/c %s]", call.Args, snapshotScript)
	}
	if call.Dir != f.backups {
		t.Errorf("Dir = %q, want %q", call.Dir, f.backups)
	}
	if _, err := os.Stat(script); err != nil {
		t.Errorf("скрипт не там, где его ищет RunTo: %v", err)
	}
	if rep.Dir != filepath.Dir(filepath.Join(f.backups, "20260930_120000"+PhysicalSuffix, ArchiveName)) {
		t.Errorf("Dir = %q, want папку, созданную скриптом", rep.Dir)
	}
	if rep.Bytes != int64(len("7z archive bytes")) {
		t.Errorf("Bytes = %d, want %d", rep.Bytes, len("7z archive bytes"))
	}
}

func TestPhysicalIgnoresOlderDatadirFolders(t *testing.T) {
	f := newFixture(t)
	// Three points already there, as on the real repack. The new one is
	// the lexicographically greatest only because it is the newest, so a
	// picker that took any of them would be caught.
	for _, name := range []string{"20260929_094649", "20260929_124551", "20260930_075446"} {
		dir := filepath.Join(f.backups, name+PhysicalSuffix)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("подготовка %s: %v", dir, err)
		}
		write(t, filepath.Join(dir, ArchiveName), "old")
	}
	f.ops.OnRunTo = func(_ context.Context, _ string, _ []string, _ string, _ io.Writer) (string, error) {
		dir := filepath.Join(f.backups, "20260930_120000"+PhysicalSuffix)
		if err := os.Mkdir(dir, 0o755); err != nil {
			return "", err
		}
		write(t, filepath.Join(dir, ArchiveName), "new")
		return "", nil
	}

	rep, err := f.runner.Physical(context.Background())
	if err != nil {
		t.Fatalf("Physical: %v", err)
	}
	if want := filepath.Join(f.backups, "20260930_120000"+PhysicalSuffix); rep.Dir != want {
		t.Errorf("Dir = %q, want %q", rep.Dir, want)
	}
	// And the older ones are untouched.
	for _, name := range []string{"20260929_094649", "20260929_124551", "20260930_075446"} {
		if got := f.read(filepath.Join(f.backups, name+PhysicalSuffix, ArchiveName)); got != "old" {
			t.Errorf("старая точка %s изменена: %q", name, got)
		}
	}
}

func TestPhysicalRefusesWhileMySQLIsRunning(t *testing.T) {
	f := newFixture(t)
	f.ops.Procs["mysqld.exe"] = []int{69088}

	_, err := f.runner.Physical(context.Background())
	if err == nil {
		t.Fatal("Physical прошёл при запущенной базе")
	}
	if !strings.Contains(err.Error(), "MySQL") {
		t.Errorf("ошибка не называет причину: %v", err)
	}
	if len(f.ops.RunToCalls) != 0 {
		t.Error("скрипт был запущен, хотя база работает")
	}
}

func TestPhysicalRefusesWhileWorldserverIsRunning(t *testing.T) {
	f := newFixture(t)
	f.ops.Procs["worldserver.exe"] = []int{1234}

	_, err := f.runner.Physical(context.Background())
	if err == nil {
		t.Fatal("Physical прошёл при запущенном worldserver")
	}
	if !strings.Contains(err.Error(), "worldserver") {
		t.Errorf("ошибка не называет причину: %v", err)
	}
}

func TestPhysicalRepeatsWhatTheScriptSaidWhenItRefuses(t *testing.T) {
	f := newFixture(t)
	f.ops.OnRunTo = func(_ context.Context, _ string, _ []string, _ string, out io.Writer) (string, error) {
		// What the real script prints when 7-Zip is missing.
		_, _ = io.WriteString(out, "ERROR: 7-Zip not found at \"C:\\Program Files\\7-Zip\\7z.exe\"\n")
		return "", errors.New("exit status 1")
	}

	_, err := f.runner.Physical(context.Background())
	if err == nil {
		t.Fatal("Physical прошёл, хотя скрипт упал")
	}
	// Without the script's own line the error is just an exit status, and
	// a user cannot act on that.
	if !strings.Contains(err.Error(), "7-Zip not found") {
		t.Errorf("ошибка не повторяет сообщение скрипта: %v", err)
	}
}

func TestPhysicalRemovesTheFolderTheScriptLeftBehindOnFailure(t *testing.T) {
	f := newFixture(t)
	f.ops.OnRunTo = func(_ context.Context, _ string, _ []string, _ string, out io.Writer) (string, error) {
		// The script makes the folder before it starts archiving, so a
		// failed 7-Zip leaves an empty one.
		dir := filepath.Join(f.backups, "20260930_120000"+PhysicalSuffix)
		if err := os.Mkdir(dir, 0o755); err != nil {
			return "", err
		}
		_, _ = io.WriteString(out, "ERROR: 7-Zip failed. The archive is incomplete - do not trust it.\n")
		return "", errors.New("exit status 1")
	}

	if _, err := f.runner.Physical(context.Background()); err == nil {
		t.Fatal("Physical прошёл, хотя скрипт упал")
	}
	if names := f.entries(); len(names) != 1 {
		t.Errorf("папка неудавшегося слепка осталась: %v", names)
	}
}

func TestPhysicalRefusesAnEmptyArchive(t *testing.T) {
	f := newFixture(t)
	f.ops.OnRunTo = func(_ context.Context, _ string, _ []string, _ string, _ io.Writer) (string, error) {
		dir := filepath.Join(f.backups, "20260930_120000"+PhysicalSuffix)
		if err := os.Mkdir(dir, 0o755); err != nil {
			return "", err
		}
		write(t, filepath.Join(dir, ArchiveName), "")
		return "", nil
	}

	_, err := f.runner.Physical(context.Background())
	if err == nil {
		t.Fatal("Physical принял пустой архив")
	}
	if !strings.Contains(err.Error(), "пуст") {
		t.Errorf("ошибка не говорит про пустой архив: %v", err)
	}
	if names := f.entries(); len(names) != 1 {
		t.Errorf("папка с пустым архивом осталась: %v", names)
	}
}

func TestPhysicalDoesNotStartWhenTheContextIsAlreadyCancelled(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := f.runner.Physical(ctx); err == nil {
		t.Fatal("Physical прошёл с отменённым контекстом")
	}
	if len(f.ops.RunToCalls) != 0 {
		t.Error("скрипт запущен, хотя работа была уже отменена")
	}
}

// --- the guards ------------------------------------------------------------

func TestVerifyDumpAcceptsOnlyAFinishedDump(t *testing.T) {
	dir := tempDir(t)

	cases := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"finished", dumpBody, false},
		{"no trailing newline", strings.TrimRight(dumpBody, "\n"), false},
		{"windows line endings", strings.ReplaceAll(dumpBody, "\n", "\r\n"), false},
		{"truncated", "DROP TABLE x;\nINSERT INTO x VALUES (1),\n", true},
		{"empty", "", true},
		{"trailer mentioned in data but not last", dumpBody + "INSERT INTO t VALUES ('-- Dump completed on');\n", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "_")+".sql")
			write(t, path, tc.body)

			err := verifyDump(path)
			if tc.wantErr && err == nil {
				t.Errorf("verifyDump принял %q", tc.name)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("verifyDump отверг %q: %v", tc.name, err)
			}
		})
	}
}

func TestVerifyDumpNamesTheFileItRejects(t *testing.T) {
	path := filepath.Join(tempDir(t), "acore_world.sql")
	write(t, path, "DROP TABLE x;\n")

	err := verifyDump(path)
	if err == nil {
		t.Fatal("verifyDump принял оборванный файл")
	}
	if !strings.Contains(err.Error(), "acore_world.sql") {
		t.Errorf("в ошибке нет имени файла: %v", err)
	}
}

func TestRemovableOnlyAcceptsFoldersThisPackageMakes(t *testing.T) {
	backups := filepath.Join("C:", "repack", "backups")

	cases := []struct {
		name string
		dir  string
		kind Kind
		want bool
	}{
		{"a logical point", filepath.Join(backups, "20260930_120000"+LogicalSuffix), KindLogical, true},
		{"a physical point", filepath.Join(backups, "20260930_120000"+PhysicalSuffix), KindPhysical, true},
		{"a point with a collision counter", filepath.Join(backups, "20260930_120000_2"+LogicalSuffix), KindLogical, true},
		{"case differs", filepath.Join(backups, "20260930_120000_CURRENT"), KindLogical, true},
		{"the wrong kind", filepath.Join(backups, "20260930_120000"+PhysicalSuffix), KindLogical, false},
		{"a folder of table dumps", filepath.Join(backups, "2026-09-29"), KindLogical, false},
		{"a hand-made folder", filepath.Join(backups, "before-the-festival"), KindLogical, false},
		{"the backups folder itself", backups, KindLogical, false},
		{"somewhere else entirely", filepath.Join("C:", "repack", "configs", "x"+LogicalSuffix), KindLogical, false},
		{"a sibling of backups", filepath.Join("C:", "repack", "backups-old", "x"+LogicalSuffix), KindLogical, false},
		{"nothing", "", KindLogical, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := removable(tc.dir, backups, tc.kind); got != tc.want {
				t.Errorf("removable(%q, %q, %q) = %v, want %v", tc.dir, backups, tc.kind, got, tc.want)
			}
		})
	}
}

func TestRemovableRefusesWhenTheBackupsFolderIsUnknown(t *testing.T) {
	if removable(filepath.Join("C:", "backups", "x"+LogicalSuffix), "", KindLogical) {
		t.Error("removable разрешил удаление при неизвестной папке backups")
	}
}

func TestCountingWriterReportsOnVolumeNotOnEveryWrite(t *testing.T) {
	var got []int64
	cw := &countingWriter{
		w:     io.Discard,
		every: 100,
		next:  100,
		on:    func(n int64) { got = append(got, n) },
	}

	// Ten writes of ten bytes: one report, at the hundredth byte.
	for i := 0; i < 10; i++ {
		if _, err := cw.Write(make([]byte, 10)); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if len(got) != 1 || got[0] != 100 {
		t.Errorf("отчёты = %v, want [100]", got)
	}

	// A single large write must not be lost.
	if _, err := cw.Write(make([]byte, 350)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if len(got) != 2 || got[1] != 450 {
		t.Errorf("отчёты = %v, want [100 450]", got)
	}
}

func TestNewToleratesNoClockAndNoReporter(t *testing.T) {
	f := newFixture(t)
	r := New(f.ops, f.lay, nil, nil)
	if r.clock == nil {
		t.Fatal("New оставил часы пустыми")
	}
	// Reporting to nobody must not panic.
	if _, err := r.Logical(context.Background()); err != nil {
		t.Fatalf("Logical: %v", err)
	}
}
