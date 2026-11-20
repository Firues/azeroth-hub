package health

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"wow-hub/internal/dbping"
	"wow-hub/internal/i18n"
	"wow-hub/internal/osops"
	"wow-hub/internal/repack"
)

func TestMain(m *testing.M) {
	// The assertions pin exact Russian wording; the process-wide language
	// defaults to English now, so the tests pin theirs explicitly.
	i18n.Set(i18n.Ru)
	os.Exit(m.Run())
}

// checkIDs is the exact order Run must return. The Health screen renders in
// this order and a list that reorders itself between refreshes is unreadable.
var checkIDs = []string{
	"repack",
	"mysql.process",
	"mysql.port",
	"mysql.ping",
	"auth.port",
	"world.process",
}

// stubPinger answers from a settable error and duration.
type stubPinger struct {
	err  error
	took time.Duration
}

func (p *stubPinger) Ping(context.Context) (time.Duration, error) {
	if p.took == 0 {
		p.took = time.Millisecond
	}
	return p.took, p.err
}

func (p *stubPinger) Close() error { return nil }

// makeLayout builds a repack-shaped tree so the "repack is present" check
// passes for reasons other than luck.
func makeLayout(t *testing.T) repack.Layout {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repack")
	for _, d := range []string{"configs", filepath.Join("data", "dbc"), "logs"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
	}
	for _, f := range []string{"worldserver.exe", "authserver.exe"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte{}, 0o644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	return repack.Layout{
		Root:           root,
		WorldserverExe: filepath.Join(root, "worldserver.exe"),
		AuthserverExe:  filepath.Join(root, "authserver.exe"),
		MySQLdExe:      filepath.Join(root, "mysql", "bin", "mysqld.exe"),
		MySQLadminExe:  filepath.Join(root, "mysql", "bin", "mysqladmin.exe"),
		WorldConf:      filepath.Join(root, "configs", "worldserver.conf"),
		AuthConf:       filepath.Join(root, "configs", "authserver.conf"),
		MySQLPassConf:  filepath.Join(root, "temp", "mysql_ac.conf"),
		DataDbcDir:     filepath.Join(root, "data", "dbc"),
		LogsDir:        filepath.Join(root, "logs"),
	}
}

// healthy wires a runner over a world where everything is up.
func healthy(t *testing.T) (*Runner, *osops.FakeOps, *stubPinger) {
	t.Helper()
	ops := osops.NewFake()
	ops.Procs["mysqld.exe"] = []int{74248}
	ops.Procs["authserver.exe"] = []int{69040}
	ops.Procs["worldserver.exe"] = []int{4242}
	ops.PortPIDs[3306] = 74248
	ops.PortPIDs[3724] = 69040
	ops.PortPIDs[8085] = 4242
	ops.Listening[3306] = true
	ops.Listening[3724] = true
	ops.Listening[8085] = true

	pinger := &stubPinger{}
	return NewRunner(ops, makeLayout(t), pinger), ops, pinger
}

func ids(checks []Check) []string {
	out := make([]string, len(checks))
	for i, c := range checks {
		out[i] = c.ID
	}
	return out
}

func byID(checks []Check, id string) Check {
	for _, c := range checks {
		if c.ID == id {
			return c
		}
	}
	return Check{}
}

func TestRunAllHealthy(t *testing.T) {
	r, _, _ := healthy(t)

	checks := r.Run(context.Background())

	if got := ids(checks); !equal(got, checkIDs) {
		t.Fatalf("check order = %v, want %v", got, checkIDs)
	}
	for _, c := range checks {
		if !c.OK {
			t.Errorf("%s = not OK (%s)", c.ID, c.Detail)
		}
		if c.Label == "" {
			t.Errorf("%s has no label; the UI renders labels, not IDs", c.ID)
		}
	}
}

func TestRunDetectsMissingRepack(t *testing.T) {
	// A repack that was moved or half-unpacked. The message has to name what
	// is missing: "not found" gives the user nothing to act on.
	ops := osops.NewFake()
	r := NewRunner(ops, repack.Layout{Root: filepath.Join(t.TempDir(), "gone")}, &stubPinger{})

	got := byID(r.Run(context.Background()), "repack")
	if got.OK {
		t.Fatal("repack check passed on a missing directory")
	}
	if got.Detail == "" {
		t.Fatal("repack check failed without saying what is missing")
	}
}

func TestRunDetectsEmptyLayoutWithoutPanicking(t *testing.T) {
	// A zero Layout must produce six failed checks, not a panic. The app has
	// to survive being pointed at nothing.
	r := NewRunner(osops.NewFake(), repack.Layout{}, nil)

	checks := r.Run(context.Background())
	if len(checks) != len(checkIDs) {
		t.Fatalf("got %d checks, want %d", len(checks), len(checkIDs))
	}
	for _, c := range checks {
		if c.OK {
			t.Errorf("%s passed on an empty layout", c.ID)
		}
	}
}

func TestRunDetectsMySQLProcessDown(t *testing.T) {
	r, ops, _ := healthy(t)
	ops.Procs["mysqld.exe"] = nil

	if got := byID(r.Run(context.Background()), "mysql.process"); got.OK {
		t.Fatalf("mysql.process passed with no mysqld running (%s)", got.Detail)
	}
}

func TestRunDetectsAuthPortClosed(t *testing.T) {
	r, ops, _ := healthy(t)
	ops.Listening[3724] = false

	got := byID(r.Run(context.Background()), "auth.port")
	if got.OK {
		t.Fatal("auth.port passed with nothing listening")
	}
	if !strings.Contains(got.Detail, "3724") {
		t.Errorf("detail = %q, want it to name the port", got.Detail)
	}
}

func TestRunDetectsWorldProcessDown(t *testing.T) {
	r, ops, _ := healthy(t)
	ops.Procs["worldserver.exe"] = nil

	if got := byID(r.Run(context.Background()), "world.process"); got.OK {
		t.Fatalf("world.process passed with no worldserver running (%s)", got.Detail)
	}
}

func TestRunDetectsPortOpenButPingFails(t *testing.T) {
	// The Review Focus case. Port 3306 accepting connections while the
	// database refuses them means something else owns the port. The port
	// check passes, so the ping is the only thing standing between the user
	// and a confident wrong diagnosis.
	r, ops, pinger := healthy(t)
	ops.Listening[3306] = true
	ops.PortPIDs[3306] = 9999 // something else owns it
	pinger.err = errStub("Access denied for user 'acore'@'localhost'")

	checks := r.Run(context.Background())

	if got := byID(checks, "mysql.port"); !got.OK {
		t.Errorf("mysql.port = not OK (%s); the port really is open, and that is what makes the ping essential", got.Detail)
	}

	ping := byID(checks, "mysql.ping")
	if ping.OK {
		t.Fatal("mysql.ping passed although the database refused the connection")
	}
	if !strings.Contains(ping.Detail, "Access denied") {
		t.Errorf("detail = %q, want the underlying error surfaced", ping.Detail)
	}
}

func TestRunPingFailureNamesConfigFile(t *testing.T) {
	// temp\ is gitignored, so the password file can simply be gone. The
	// detail has to name it, or the user is hunting the wrong problem.
	r, _, pinger := healthy(t)
	pinger.err = errStub("чтение C:\\repack\\temp\\mysql_ac.conf: файл не найден")

	got := byID(r.Run(context.Background()), "mysql.ping")
	if got.OK {
		t.Fatal("mysql.ping passed on a config error")
	}
	if !strings.Contains(got.Detail, "mysql_ac.conf") {
		t.Errorf("detail = %q, want it to name mysql_ac.conf", got.Detail)
	}
}

func TestRunPingDetailCarriesDuration(t *testing.T) {
	// A slow database is a different problem from a dead one, and the
	// Health screen is where the user looks to tell them apart.
	r, _, pinger := healthy(t)
	pinger.took = 1234 * time.Millisecond

	got := byID(r.Run(context.Background()), "mysql.ping")
	if !got.OK {
		t.Fatalf("mysql.ping = not OK on a slow but successful ping: %s", got.Detail)
	}
	if !strings.Contains(got.Detail, "1234") {
		t.Errorf("detail = %q, want it to carry the duration", got.Detail)
	}
}

func TestRunNeverOmitsACheck(t *testing.T) {
	// A list that silently shrinks reads as "all healthy" to anyone
	// glancing at it, which is the opposite of what a missing entry means.
	r, ops, pinger := healthy(t)
	ops.Procs["worldserver.exe"] = nil
	ops.Listening[3724] = false
	ops.Listening[3306] = false
	pinger.err = errStub("down")

	checks := r.Run(context.Background())
	if got := ids(checks); !equal(got, checkIDs) {
		t.Fatalf("check order = %v with everything broken, want %v", got, checkIDs)
	}
}

func TestRunWithNilPingerReportsUnconfigured(t *testing.T) {
	// Not "skipped" and not a pass: a check that could not be evaluated is
	// failed with a reason.
	r, ops, _ := healthy(t)
	r2 := NewRunner(ops, r.lay, nil)

	got := byID(r2.Run(context.Background()), "mysql.ping")
	if got.OK {
		t.Fatal("mysql.ping passed with no pinger configured")
	}
	if !strings.Contains(got.Detail, "не настро") {
		t.Errorf("detail = %q, want it to say the check is unconfigured", got.Detail)
	}
}

func TestRunHonoursContextCancellation(t *testing.T) {
	// The status loop cancels on shutdown. A ping that ignored it would keep
	// a connection open past exit.
	r, _, pinger := healthy(t)
	pinger.err = context.Canceled

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	checks := r.Run(ctx)
	if got := byID(checks, "mysql.ping"); got.OK {
		t.Fatal("mysql.ping passed on a cancelled context")
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

type errStub string

func (e errStub) Error() string { return string(e) }

var _ dbping.Pinger = (*stubPinger)(nil)
