package supervisor

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"wow-hub/internal/dbping"
	"wow-hub/internal/notify"
	"wow-hub/internal/osops"
)

// notifyRecorderStub exists only so startFixture's field reads clearly; the
// real recorder lives in notify.
type notifyRecorderStub = notify.Recorder

type startFixture struct {
	sup     *Supervisor
	ops     *osops.FakeOps
	clk     *osops.FakeClock
	sink    *notifyRecorderStub
	started []string
	pingErr error
	mu      sync.Mutex
}

func newStartFixture(t *testing.T) *startFixture {
	t.Helper()
	ops := osops.NewFake()
	clk := osops.NewFakeClock()
	sink := notify.NewRecorder()
	layout := testLayout()
	// MySQL's port must be reachable for the sequence to move past step 1;
	// worldserver's deliberately is not, so tests choose when to open 8085.
	ops.Listening[portMySQL] = true
	ops.Listening[portAuth] = true

	f := &startFixture{ops: ops, clk: clk, sink: sink}

	pinger := &fakePinger{err: &f.pingErr, onPing: func() {
		f.mu.Lock()
		f.started = append(f.started, "ping")
		f.mu.Unlock()
	}}
	sup := New(ops, clk, layout, sink, pinger, &fakeConfirmer{answers: []bool{true}}, Conf{})

	ops.OnStart = func(exe string, args []string, dir string, out io.Writer) (osops.Proc, error) {
		f.mu.Lock()
		f.started = append(f.started, exeName(exe))
		f.mu.Unlock()

		if strings.Contains(exe, "authserver") {
			ops.Listening[portAuth] = true
		}
		if strings.Contains(exe, "worldserver") {
			// worldserver's own stdout is what the readiness check reads.
			if w, ok := out.(*markerWatcher); ok {
				w.Write([]byte("WORLD: World Initialized In 0 Minutes 46 Seconds\r\n"))
			}
			ops.Listening[portWorld] = true
		}
		return osops.NewFakeProc(900), nil
	}

	f.sup = sup
	return f
}

func (f *startFixture) pumpStart(done <-chan error, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			return err
		default:
		}
		if f.clk.Pending() > 0 {
			f.clk.Fire(time.Second)
			continue
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-done:
		return err
	case <-time.After(100 * time.Millisecond):
		return errStillRunning
	}
}

func (f *startFixture) startedNames() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.started))
	copy(out, f.started)
	return out
}

// fakePinger answers from the fixture's pingErr pointer, so a test can set
// the failure after the supervisor has been built, and records each ping so
// the sequence can be asserted.
type fakePinger struct {
	err    *error
	onPing func()
}

func (p *fakePinger) Ping(context.Context) (time.Duration, error) {
	if p.onPing != nil {
		p.onPing()
	}
	if p.err != nil && *p.err != nil {
		return time.Millisecond, *p.err
	}
	return time.Millisecond, nil
}

func (p *fakePinger) Close() error { return nil }

var _ dbping.Pinger = (*fakePinger)(nil)

// exeName strips the extension so assertions read as "mysqld" rather than
// "mysqld.exe"; the image names on this machine all carry one.
func exeName(path string) string {
	return strings.TrimSuffix(strings.ToLower(filepath.Base(path)), ".exe")
}

func waitUntilCond(t *testing.T, budget time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

var filepathSeparator = string(os.PathSeparator)

type errTest string

func (e errTest) Error() string { return string(e) }

var _ = fmt.Sprint

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// commandFails makes the world's stdin handler stop killing the process.
//
// The shared start fixture models a server that obeys "server shutdown 1",
// which is what the binaries do. A test about the rollback must NOT have that
// behaviour, or the process vanishes on its own and the rollback has nothing
// left to do - which looks like a passing rollback while proving nothing.
func (f *startFixture) commandFails() {
	f.ops.OnStart = func(exe string, args []string, dir string, out io.Writer) (osops.Proc, error) {
		f.mu.Lock()
		f.started = append(f.started, exeName(exe))
		f.mu.Unlock()
		if out != nil {
			out.Write([]byte("WORLD: World Initialized In 0 Minutes 46 Seconds\r\n"))
		}
		if strings.Contains(strings.ToLower(exe), "authserver") {
			f.ops.Listening[3724] = true
		}
		if strings.Contains(strings.ToLower(exe), "mysqld") {
			f.ops.Listening[3306] = true
		}
		return osops.NewFakeProc(3000), nil
	}
}
