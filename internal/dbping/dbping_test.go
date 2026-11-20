package dbping

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewRejectsEmptyPassword(t *testing.T) {
	// An empty password would reach the driver as a real password and come
	// back as "wrong password", pointing the user at their server instead
	// of at their missing config file.
	_, err := New("")
	if err == nil {
		t.Fatal("New accepted an empty password")
	}
}

func TestNewErrorNamesTheSourceFile(t *testing.T) {
	// This is the Review Focus failure: temp\ is gitignored, so the file
	// can simply be gone, and "access denied" from the driver would send
	// the user hunting in the wrong place.
	path := filepath.Join(t.TempDir(), "temp", "mysql_ac.conf")

	_, err := NewFromFile(path)
	if err == nil {
		t.Fatal("NewFromFile accepted a missing file")
	}
	if !strings.Contains(err.Error(), "mysql_ac.conf") {
		t.Errorf("error = %q, want it to name the file", err)
	}
}

func TestPingAgainstClosedPortFailsFast(t *testing.T) {
	// Port 1 is reserved and nothing listens there. The ping must return an
	// error bounded by the context, not hang and not panic: this runs on the
	// app's status loop.
	p, err := NewAt("test-password", "127.0.0.1", 1)
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	_, err = p.Ping(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Ping against a closed port returned no error")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Ping took %s, want it bounded by the context", elapsed)
	}
}

func TestPingHonoursAnAlreadyCancelledContext(t *testing.T) {
	// The status loop cancels its context on shutdown. A ping that ignored
	// cancellation would keep a goroutine and a socket alive past exit.
	p, err := NewAt("test-password", "127.0.0.1", 1)
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer p.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	if _, err := p.Ping(ctx); err == nil {
		t.Fatal("Ping with a cancelled context returned no error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Ping took %s on a cancelled context", elapsed)
	}
}

func TestPingReportsDurationOnFailure(t *testing.T) {
	// The Health screen shows how long the database took. On a failure the
	// elapsed time is still the interesting number: a 30 ms failure and a
	// 30 s failure are different problems.
	//
	// The invariant is that time never runs backwards, not that it is
	// always positive. A connection refused on loopback is detected inside
	// a single tick of the system clock, and this assertion demanded a
	// positive value often enough to fail about one run in four.
	p, err := NewAt("test-password", "127.0.0.1", 1)
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	d, err := p.Ping(ctx)
	if err == nil {
		t.Fatal("expected an error")
	}
	if d < 0 {
		t.Fatalf("Ping returned duration %v, want a non-negative elapsed time", d)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	// Both supervisor shutdown and app shutdown will call Close. A second
	// call that returns an error would surface as a spurious failure at exit.
	p, err := NewAt("test-password", "127.0.0.1", 1)
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v, want it to be harmless", err)
	}
}

func TestPingAfterCloseDoesNotPanic(t *testing.T) {
	p, err := NewAt("test-password", "127.0.0.1", 1)
	if err != nil {
		t.Fatalf("NewAt: %v", err)
	}
	p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := p.Ping(ctx); err == nil {
		t.Fatal("Ping after Close returned no error")
	}
}

func TestMaskedDSNNeverShowsThePassword(t *testing.T) {
	// The DSN ends up in logs and in screenshots.
	if got := MaskDSN("acore:sup3rs3cret@tcp(127.0.0.1:3306)/"); strings.Contains(got, "sup3rs3cret") {
		t.Fatalf("MaskDSN leaked the password: %q", got)
	}
}
