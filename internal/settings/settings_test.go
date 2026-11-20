package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testRoot is one directory for the whole package, removed once. Same reason
// as the other packages: removing a test directory costs about 1.3 seconds on
// this machine whatever is in it, because the antivirus holds a handle on the
// files just written, and that is time the code under test never spends.
var testRoot string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "wow-hub-settings")
	if err != nil {
		fmt.Fprintln(os.Stderr, "временный каталог:", err)
		os.Exit(1)
	}
	testRoot = dir

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(testRoot, strings.ReplaceAll(t.Name(), "/", "_"))
	if err != nil {
		t.Fatalf("временный каталог: %v", err)
	}
	return dir
}

// path is the settings file inside a fresh directory.
func path(t *testing.T) string {
	t.Helper()
	return filepath.Join(tempDir(t), "wow-hub.settings.json")
}

var (
	t0 = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	t1 = t0.AddDate(0, 0, 1)
	t7 = t0.AddDate(0, 0, 7)
)

/* --------------------------------- Load --------------------------------- */

func TestLoadWithoutAFileGivesTheDefaultsAndNoError(t *testing.T) {
	s, err := Load(path(t))
	if err != nil {
		t.Fatalf("отсутствие файла — это норма, а не ошибка: %v", err)
	}
	if s.Backup.Enabled {
		t.Error("напоминание включено по умолчанию")
	}
	if s.Backup.AutoRun {
		t.Error("автосоздание включено по умолчанию")
	}
	if s.Backup.EveryDays != DefaultDays {
		t.Errorf("интервал по умолчанию %d, ожидался %d", s.Backup.EveryDays, DefaultDays)
	}
}

func TestLoadWithoutAPathGivesTheDefaults(t *testing.T) {
	if _, err := Load(""); err != nil {
		t.Fatalf("пустой путь: %v", err)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	p := path(t)
	want := Default()
	want.Backup = Backup{
		Enabled: true, EveryDays: 3, AutoRun: true,
		LastRun: t7, LastAttempt: t7, SnoozedUntil: t1,
	}

	if err := Save(p, want); err != nil {
		t.Fatalf("сохранение: %v", err)
	}

	got, err := Load(p)
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if got.Backup != want.Backup {
		t.Errorf("получено %+v, ожидалось %+v", got.Backup, want.Backup)
	}
}

func TestSaveWritesReadableJSON(t *testing.T) {
	p := path(t)
	if err := Save(p, Default()); err != nil {
		t.Fatalf("сохранение: %v", err)
	}

	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("чтение файла: %v", err)
	}
	if !json.Valid(raw) {
		t.Fatalf("файл не является корректным JSON:\n%s", raw)
	}
	if !strings.HasSuffix(string(raw), "\n") {
		t.Error("файл не заканчивается переводом строки")
	}

	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if _, ok := probe["Backup"]; !ok {
		t.Errorf("в файле нет секции Backup: %s", raw)
	}
}

// A file that cannot be parsed is reported but still yields something usable.
// The alternative - zeroes - would read as "every 0 days" and make the
// schedule permanently due.
func TestLoadOfABrokenFileReportsItAndFallsBackToDefaults(t *testing.T) {
	p := path(t)
	if err := os.WriteFile(p, []byte("{ this is not json"), 0o644); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	s, err := Load(p)
	if err == nil {
		t.Fatal("битый файл не вызвал ошибку")
	}
	if !strings.Contains(err.Error(), filepath.Base(p)) {
		t.Errorf("в ошибке нет имени файла: %v", err)
	}
	if s.Backup.EveryDays != DefaultDays || s.Backup.Enabled {
		t.Errorf("после ошибки получено %+v, ожидались значения по умолчанию", s.Backup)
	}
}

// A file written by an older version, or trimmed by hand, must still fill in
// the fields it does not mention.
func TestLoadFillsInMissingFieldsFromTheDefaults(t *testing.T) {
	p := path(t)
	if err := os.WriteFile(p, []byte(`{"Backup":{"Enabled":true}}`), 0o644); err != nil {
		t.Fatalf("подготовка: %v", err)
	}

	s, err := Load(p)
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if !s.Backup.Enabled {
		t.Error("включённость потеряна")
	}
	if s.Backup.EveryDays != DefaultDays {
		t.Errorf("интервал %d, ожидался %d из значений по умолчанию",
			s.Backup.EveryDays, DefaultDays)
	}
}

func TestLoadClampsAnIntervalThatMeansNothing(t *testing.T) {
	cases := []struct {
		name string
		in   int
		want int
	}{
		{"ноль", 0, DefaultDays},
		{"отрицательный", -5, DefaultDays},
		{"слишком большой", 100000, MaxDays},
		{"на нижней границе", MinDays, MinDays},
		{"на верхней границе", MaxDays, MaxDays},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := path(t)
			body := fmt.Sprintf(`{"Backup":{"Enabled":true,"EveryDays":%d}}`, c.in)
			if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
				t.Fatalf("подготовка: %v", err)
			}

			s, err := Load(p)
			if err != nil {
				t.Fatalf("чтение: %v", err)
			}
			if s.Backup.EveryDays != c.want {
				t.Errorf("интервал %d, ожидался %d", s.Backup.EveryDays, c.want)
			}
		})
	}
}

func TestSaveClampsToo(t *testing.T) {
	p := path(t)
	s := Default()
	s.Backup.EveryDays = 0
	if err := Save(p, s); err != nil {
		t.Fatalf("сохранение: %v", err)
	}

	got, err := Load(p)
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if got.Backup.EveryDays != DefaultDays {
		t.Errorf("в файле оказался интервал %d", got.Backup.EveryDays)
	}
}

func TestSaveLeavesNoTemporaryFileBehind(t *testing.T) {
	p := path(t)
	if err := Save(p, Default()); err != nil {
		t.Fatalf("сохранение: %v", err)
	}

	entries, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		t.Fatalf("чтение каталога: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("остался временный файл %s", e.Name())
		}
	}
}

func TestSaveOverAnExistingFileReplacesIt(t *testing.T) {
	p := path(t)

	first := Default()
	first.Backup.Enabled = true
	first.Backup.EveryDays = 30
	if err := Save(p, first); err != nil {
		t.Fatalf("первое сохранение: %v", err)
	}

	second := Default()
	second.Backup.Enabled = false
	if err := Save(p, second); err != nil {
		t.Fatalf("второе сохранение: %v", err)
	}

	got, err := Load(p)
	if err != nil {
		t.Fatalf("чтение: %v", err)
	}
	if got.Backup.Enabled {
		t.Error("старое значение пережило перезапись")
	}
	if got.Backup.EveryDays != DefaultDays {
		t.Errorf("интервал %d, ожидался %d", got.Backup.EveryDays, DefaultDays)
	}
}

func TestSaveWithoutAPathIsAnError(t *testing.T) {
	if err := Save("", Default()); err == nil {
		t.Fatal("сохранение без пути не вызвало ошибку")
	}
}

/* ---------------------------------- Due --------------------------------- */

func TestDueIsOffWhenTheScheduleIsOff(t *testing.T) {
	b := Backup{Enabled: false, EveryDays: 7, LastRun: t0}

	due, next := b.Due(t7)
	if due {
		t.Error("выключенное расписание считает себя должным")
	}
	if !next.IsZero() {
		t.Errorf("выключенное расписание вернуло дату %v", next)
	}
}

// Nothing to measure an interval from, so it is due now.
func TestDueIsImmediateWhenNoPointHasEverBeenMade(t *testing.T) {
	b := Backup{Enabled: true, EveryDays: 7}

	due, next := b.Due(t0)
	if !due {
		t.Fatal("без единой точки расписание не считает себя должным")
	}
	if !next.Equal(t0) {
		t.Errorf("следующий срок %v, ожидался %v", next, t0)
	}
}

func TestDueIsNotYetDueInsideTheInterval(t *testing.T) {
	b := Backup{Enabled: true, EveryDays: 7, LastRun: t0}

	due, next := b.Due(t1)
	if due {
		t.Error("точка запрошена на следующий день при интервале в неделю")
	}
	if !next.Equal(t7) {
		t.Errorf("следующий срок %v, ожидался %v", next, t7)
	}
}

// The boundary: exactly on the due date is due, not "not yet".
func TestDueOnTheBoundary(t *testing.T) {
	b := Backup{Enabled: true, EveryDays: 7, LastRun: t0}

	due, _ := b.Due(t7)
	if !due {
		t.Error("на границе интервала точка не запрошена")
	}
}

func TestDueAfterTheInterval(t *testing.T) {
	b := Backup{Enabled: true, EveryDays: 7, LastRun: t0}

	due, _ := b.Due(t0.AddDate(0, 0, 40))
	if !due {
		t.Error("через сорок дней при интервале в неделю точка не запрошена")
	}
}

func TestASnoozeDefersTheReminder(t *testing.T) {
	b := Backup{Enabled: true, EveryDays: 7, LastRun: t0}

	// Snoozed for a day, and asked the day the interval expires.
	snoozed := b.Snooze(t7, 1)
	due, next := snoozed.Due(t7)
	if due {
		t.Error("напоминание сработало внутри отложенного срока")
	}
	if !next.Equal(t7.AddDate(0, 0, 1)) {
		t.Errorf("следующий срок %v, ожидался конец отложения", next)
	}

	// And it fires once the snooze is over, without the interval moving.
	due, _ = snoozed.Due(t7.AddDate(0, 0, 1))
	if !due {
		t.Error("после отложения напоминание не сработало")
	}
}

func TestSnoozeCountsFromNowNotFromTheDueDate(t *testing.T) {
	b := Backup{Enabled: true, EveryDays: 7, LastRun: t0}

	// Asked long after the due date: a day from now, not a day from then.
	late := t0.AddDate(0, 0, 60)
	snoozed := b.Snooze(late, 3)

	due, next := snoozed.Due(late)
	if due {
		t.Error("сразу после отложения напоминание сработало")
	}
	if !next.Equal(late.AddDate(0, 0, 3)) {
		t.Errorf("конец отложения %v, ожидался %v", next, late.AddDate(0, 0, 3))
	}
}

func TestSnoozeOfZeroDaysStillDefers(t *testing.T) {
	b := Backup{Enabled: true, EveryDays: 1, LastRun: t0}

	snoozed := b.Snooze(t1, 0)
	due, _ := snoozed.Due(t1)
	if due {
		t.Error("отложение на ноль дней ничего не отложило и сработало сразу")
	}
}

/* --------------------------- stamps and retries -------------------------- */

// A success moves both stamps and clears the snooze.
func TestRanRecordsThePointAndClearsTheSnooze(t *testing.T) {
	b := Backup{Enabled: true, EveryDays: 7, LastRun: t0, SnoozedUntil: t7}

	got := b.Ran(t7)
	if !got.LastRun.Equal(t7) {
		t.Errorf("LastRun = %v, ожидался %v", got.LastRun, t7)
	}
	if !got.LastAttempt.Equal(t7) {
		t.Errorf("LastAttempt = %v, ожидался %v", got.LastAttempt, t7)
	}
	if !got.SnoozedUntil.IsZero() {
		t.Error("отложение пережило успешную точку")
	}
}

// A failure must not touch LastRun: if it did, a dump that never happened
// would silence the reminder for a week.
func TestAttemptedLeavesLastRunWhereItWas(t *testing.T) {
	b := Backup{Enabled: true, EveryDays: 7, LastRun: t0}

	got := b.Attempted(t7)
	if !got.LastRun.Equal(t0) {
		t.Errorf("LastRun сдвинулся на %v после неудачной попытки", got.LastRun)
	}
	if !got.LastAttempt.Equal(t7) {
		t.Errorf("LastAttempt = %v, ожидался %v", got.LastAttempt, t7)
	}

	// And the reminder is still due, because no point was made.
	due, _ := got.Due(t7)
	if !due {
		t.Error("после неудачной попытки напоминание замолчало")
	}
}

func TestCanRetryIsTrueBeforeAnyAttempt(t *testing.T) {
	b := Backup{Enabled: true, EveryDays: 7}
	if !b.CanRetry(t0) {
		t.Error("до первой попытки повтор запрещён")
	}
}

// This is what keeps the one-second status loop from turning one failure into
// a thousand attempts.
func TestCanRetryIsFalseRightAfterAFailedAttempt(t *testing.T) {
	b := Backup{Enabled: true, EveryDays: 7, LastRun: t0}
	failed := b.Attempted(t7)

	if failed.CanRetry(t7.Add(time.Minute)) {
		t.Error("повтор разрешён через минуту после неудачи")
	}
	if failed.CanRetry(t7.Add(autoRetryAfter - time.Minute)) {
		t.Error("повтор разрешён чуть раньше срока")
	}
	if !failed.CanRetry(t7.Add(autoRetryAfter)) {
		t.Error("повтор не разрешён по истечении срока")
	}
}

/* -------------------------------- defaults ------------------------------ */

func TestDefaultIsOffWithAWeek(t *testing.T) {
	d := Default()
	if d.Backup.Enabled || d.Backup.AutoRun {
		t.Error("значения по умолчанию что-то включают")
	}
	if d.Backup.EveryDays != 7 {
		t.Errorf("интервал по умолчанию %d", d.Backup.EveryDays)
	}
	if !d.Backup.LastRun.IsZero() || !d.Backup.LastAttempt.IsZero() {
		t.Error("значения по умолчанию выглядят как состоявшаяся точка")
	}
}
