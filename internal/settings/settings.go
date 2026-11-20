// Package settings stores the few choices the panel remembers between runs.
//
// One small JSON file, beside the exe. Deliberately not %APPDATA%: the repack
// is a folder people move, and a preferences file that stayed behind would
// describe a realm that is no longer the one next to it. It is also why the
// file is not in git - see .gitignore - since it records when *this* machine
// last made a rollback point.
//
// Everything here has a working default, and a missing file is not an error.
// An unreadable one is reported but does not stop anything: the panel's job
// is to run a server, and refusing to open because a preferences file is
// malformed would be a spectacularly bad trade.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"wow-hub/internal/i18n"
)

// The interval bounds. A day is the shortest thing a person means by "every
// few days", and a year is long enough to be "off" without being a number
// nobody chose.
const (
	MinDays     = 1
	MaxDays     = 365
	DefaultDays = 7
)

// autoRetryAfter is how long the panel waits before trying an automatic point
// again after a failed one.
//
// It exists because the scheduler runs on the one-second status loop: without
// it, a dump that fails would be retried every second, forever, and each
// attempt would write a partial file first. Six hours means a transient
// problem - MySQL busy, disk full - is retried the same day, and a real one
// does not turn into a loop.
const autoRetryAfter = 6 * time.Hour

// Backup is the rollback-point schedule.
type Backup struct {
	// Enabled turns the reminder on at all.
	Enabled bool
	// EveryDays is the interval. Clamped to [MinDays, MaxDays] on load and
	// on save, so a hand-edited file cannot make the reminder meaningless.
	EveryDays int
	// AutoRun makes the panel create the point itself once it is due, rather
	// than only saying so.
	//
	// Off by default, and that default is the point: a logical point is four
	// dumps, about 385 MB and about two minutes of disk writes. Starting
	// that unasked, while somebody is in the middle of playing, is not a
	// favour - and a panel that surprises you with disk load is a panel you
	// stop trusting.
	AutoRun bool
	// LastRun is when a point was last made through the panel. The zero time
	// means never.
	LastRun time.Time
	// LastAttempt is when the panel last *tried* on its own, successfully or
	// not. Kept apart from LastRun, which only moves on success: see
	// autoRetryAfter.
	LastAttempt time.Time
	// SnoozedUntil suppresses the reminder until then without changing the
	// interval. The zero time means it is not snoozed.
	SnoozedUntil time.Time
}

// Settings is the whole file.
type Settings struct {
	// Language is the UI language code ("en", "ru", ...). Empty means "no
	// decision yet": the backend detects one from the OS on first start, and
	// the picker on the More screen overwrites it from then on. A hand-edited
	// value nobody speaks is safe without validation here - i18n.Init treats
	// an unknown code the same as an empty one.
	Language string
	Backup   Backup
}

// Default is what a fresh install gets: no reminder, a week, no auto-run, and
// no language chosen yet.
//
// Off rather than on, because the first thing a new panel should do is
// nothing. Turning a reminder on is a decision, and a decision the user made
// is the only kind that survives being wrong.
func Default() Settings {
	return Settings{Language: "", Backup: Backup{Enabled: false, EveryDays: DefaultDays, AutoRun: false}}
}

// Load reads the file.
//
// A missing file gives the defaults and no error - that is the normal state
// of a fresh install, not a fault. A file that exists but cannot be parsed
// gives the defaults and an error, so the caller can say so rather than
// silently forgetting what the user chose.
func Load(path string) (Settings, error) {
	def := Default()
	if path == "" {
		return def, nil
	}

	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return def, nil
	}
	if err != nil {
		return def, fmt.Errorf("%s: %w", i18n.T("settings.prefix"), err)
	}

	// Start from the defaults so a file written by an older version, or one
	// with a field removed, still yields a usable struct rather than zeroes
	// that read as "every 0 days".
	out := def
	if err := json.Unmarshal(raw, &out); err != nil {
		return def, fmt.Errorf("%s: %w", i18n.Tf("settings.unreadable", filepath.Base(path)), err)
	}

	out.Backup = out.Backup.clamped()
	return out, nil
}

// Save writes the file.
//
// Through a temporary file and a rename, because the alternative - writing
// in place - can leave a half-written file if the process dies mid-write, and
// a preferences file that fails to parse on the next start is worse than no
// file at all.
func Save(path string, s Settings) error {
	if path == "" {
		return errors.New(i18n.T("settings.nowhere"))
	}

	s.Backup = s.Backup.clamped()

	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("%s: %w", i18n.T("settings.prefix"), err)
	}
	body = append(body, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("%s: %w", i18n.T("settings.prefix"), err)
	}

	if err := os.Rename(tmp, path); err != nil {
		// Leaving the temporary behind would accumulate one file per failed
		// save, and the next save overwrites it anyway.
		_ = os.Remove(tmp)
		return fmt.Errorf("%s: %w", i18n.T("settings.prefix"), err)
	}
	return nil
}

// ClampDays keeps an interval inside the range that means something.
//
// Exported because the app needs the same rule when a value arrives from the
// page, and a second copy of "zero becomes a week" is exactly the kind of
// thing that drifts. Zero would make the schedule permanently due and a
// negative one is not a duration.
func ClampDays(n int) int {
	if n < MinDays {
		return DefaultDays
	}
	if n > MaxDays {
		return MaxDays
	}
	return n
}

// clamped applies ClampDays to a whole record.
//
// Runs on the way in from the file and on the way out to it, because both
// sides are reachable by hand: the file by editing it, the struct by a caller
// that forgot.
func (b Backup) clamped() Backup {
	b.EveryDays = ClampDays(b.EveryDays)
	return b
}

// Due reports whether a point is wanted now, and when the next one falls due.
//
// A schedule that is on with no point ever made is due immediately. There is
// nothing to measure an interval from, and "you have never made one" is the
// strongest possible reason to make one now.
//
// When it is not due, the second return value is the moment it becomes due,
// so the page can say "через 3 дня" rather than only "нет".
func (b Backup) Due(now time.Time) (due bool, next time.Time) {
	if !b.Enabled || b.EveryDays <= 0 {
		return false, time.Time{}
	}

	// A snooze defers the reminder without touching the interval, so the
	// next due date is the end of the snooze rather than a recomputed one.
	if now.Before(b.SnoozedUntil) {
		return false, b.SnoozedUntil
	}

	if b.LastRun.IsZero() {
		return true, now
	}

	next = b.LastRun.AddDate(0, 0, b.EveryDays)
	if now.Before(next) {
		return false, next
	}
	return true, now
}

// CanRetry reports whether an automatic attempt may start now.
//
// Separate from Due on purpose. Due answers "does the user want a point";
// this answers "may the panel start one by itself right now", and the answer
// is no for autoRetryAfter after a failed attempt - which is what keeps the
// one-second status loop from turning one failure into a thousand.
func (b Backup) CanRetry(now time.Time) bool {
	if b.LastAttempt.IsZero() {
		return true
	}
	return now.Sub(b.LastAttempt) >= autoRetryAfter
}

// Ran records a successful point.
//
// Both stamps move: the attempt was made and it worked, so there is nothing
// left to retry.
func (b Backup) Ran(at time.Time) Backup {
	b.LastRun = at
	b.LastAttempt = at
	b.SnoozedUntil = time.Time{}
	return b
}

// Attempted records a failed attempt at an automatic point.
//
// Only the attempt moves. LastRun has to stay where it is, or a failure would
// read as "a fresh point exists" and the reminder would go quiet for a week
// after a dump that never happened.
func (b Backup) Attempted(at time.Time) Backup {
	b.LastAttempt = at
	return b
}

// Snooze defers the reminder for the given number of days.
//
// Counted from now rather than from the due date: "remind me in a day" means
// a day from when the button was pressed, which is what a person expects and
// the only reading that cannot produce a snooze already in the past.
func (b Backup) Snooze(now time.Time, days int) Backup {
	if days < 1 {
		days = 1
	}
	b.SnoozedUntil = now.AddDate(0, 0, days)
	return b
}
