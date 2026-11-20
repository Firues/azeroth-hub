package supervisor

import (
	"context"
	"time"

	"wow-hub/internal/notify"
	"wow-hub/internal/osops"
)

// pollInterval is how often a wait loop re-checks its condition. Short
// enough that a stop feels immediate once the process actually exits, long
// enough that a 30-second wait costs a hundred cheap queries rather than
// thirty thousand.
const pollInterval = 200 * time.Millisecond

// waitGone polls until pid is no longer alive, the context is cancelled, or
// the timeout passes. It reports whether the process is gone.
//
// It polls Ops.Alive rather than waiting on Proc.Wait for two reasons.
// Adopted processes have no Proc of ours to wait on, and a Proc.Wait can
// block forever if a grandchild inherited the stdout pipe - which is a real
// hazard, not a theoretical one; see the test that found it. One code path
// works for every case, owned or not.
func waitGone(ctx context.Context, clk osops.Clock, ops osops.Ops, pid int, timeout time.Duration) bool {
	deadline := clk.Now().Add(timeout)

	for {
		if !ops.Alive(pid) {
			return true
		}
		if !clk.Now().Before(deadline) {
			return !ops.Alive(pid)
		}

		select {
		case <-ctx.Done():
			return !ops.Alive(pid)
		case <-clk.After(pollInterval):
		}
	}
}

// waitUntil polls cond until it holds, the context is cancelled, or the
// timeout passes.
func waitUntil(ctx context.Context, clk osops.Clock, timeout time.Duration, cond func() bool) bool {
	deadline := clk.Now().Add(timeout)

	for {
		if cond() {
			return true
		}
		if !clk.Now().Before(deadline) {
			return cond()
		}

		select {
		case <-ctx.Done():
			return cond()
		case <-clk.After(pollInterval):
		}
	}
}

// askConfirm asks the UI whether to force-kill, and waits for an answer.
//
// A dialog nobody answers must not wedge the backend, so the clock bounds
// the wait exactly as it bounds a process exit. Silence counts as a refusal.
//
// On a timeout the page is told to close the dialog. Leaving it up would be
// worse than the refusal itself: the user would still be looking at a
// question the backend has stopped listening to, and clicking "yes" on it
// would quietly do nothing.
func (s *Supervisor) askConfirm(ctx context.Context, id ServiceID, pid int) (bool, bool) {
	if s.confirmer == nil {
		// No UI attached: never kill on a guess.
		return false, false
	}

	reply := s.confirmer.AskConfirm(string(id), pid)

	deadline := s.clk.Now().Add(s.conf.ConfirmTimeout)
	for {
		select {
		case ok := <-reply:
			return ok, true
		default:
		}

		if !s.clk.Now().Before(deadline) {
			s.cancelConfirm()
			return false, false
		}

		select {
		case <-ctx.Done():
			s.cancelConfirm()
			return false, false
		case ok := <-reply:
			return ok, true
		case <-s.clk.After(pollInterval):
		}
	}
}

// cancelConfirm tells the page to close the dialog it opened.
func (s *Supervisor) cancelConfirm() {
	if s.sink != nil {
		s.sink.Emit(notify.Event{Type: notify.EventConfirmCancel})
	}
}
