// Package health answers the question a user has when something feels
// wrong: which part of the stack is unhappy?
//
// Six checks, always all six. A list that silently shrinks to four entries
// reads as "all healthy" to anyone glancing at it, which is the opposite of
// what a missing entry means.
package health

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"wow-hub/internal/dbping"
	"wow-hub/internal/i18n"
	"wow-hub/internal/osops"
	"wow-hub/internal/repack"
)

// Check is one row of the Health screen.
type Check struct {
	// ID is stable and machine-facing. The frontend switches on it.
	ID string
	// Label is what a person reads.
	Label string
	OK    bool
	// Detail explains a failure, or adds information on a pass. It is never
	// empty for a failure: "not OK" with no reason is a dead end.
	Detail string
}

// Check IDs, in the order Run returns them.
const (
	IDRepack       = "repack"
	IDMySQLProcess = "mysql.process"
	IDMySQLPort    = "mysql.port"
	IDMySQLPing    = "mysql.ping"
	IDAuthPort     = "auth.port"
	IDWorldProcess = "world.process"
)

// Ports, read from repack so the health screen, the supervisor's readiness
// waits and dbping cannot drift apart.
const (
	portMySQL = repack.MySQLPort
	portAuth  = repack.AuthPort
	portWorld = repack.WorldPort
)

// Runner evaluates the six checks.
//
// Three of them are about MySQL, and that is not duplication. "the process
// is alive" and "the database answers" diverge in practice - a hung mysqld
// is an ordinary failure and the ping is the only thing that catches it. In
// the UI they are one row with three labels, not three cards.
type Runner struct {
	ops    osops.Ops
	lay    repack.Layout
	pinger dbping.Pinger
}

// NewRunner builds a Runner. pinger may be nil; the ping check then reports
// itself as unconfigured rather than silently passing.
func NewRunner(ops osops.Ops, lay repack.Layout, pinger dbping.Pinger) *Runner {
	return &Runner{ops: ops, lay: lay, pinger: pinger}
}

// Run evaluates every check and returns all six, in a fixed order.
//
// Health runs on the status loop, so it must not block indefinitely: the
// caller supplies a context and the ping is bounded by it.
func (r *Runner) Run(ctx context.Context) []Check {
	checks := make([]Check, 0, 6)
	checks = append(checks, r.checkRepack())
	checks = append(checks, r.checkProcess("mysqld.exe", IDMySQLProcess, i18n.T("health.mysql.process.label")))
	checks = append(checks, r.checkPort(portMySQL, IDMySQLPort, i18n.T("health.mysql.port.label")))
	checks = append(checks, r.checkPing(ctx))
	checks = append(checks, r.checkPort(portAuth, IDAuthPort, i18n.T("health.auth.port.label")))
	checks = append(checks, r.checkProcess("worldserver.exe", IDWorldProcess, i18n.T("health.world.process.label")))
	return checks
}

func (r *Runner) checkRepack() Check {
	c := Check{ID: IDRepack, Label: i18n.T("health.repack.label")}

	if r.lay.Root == "" {
		c.Detail = i18n.T("health.repack.noroot")
		return c
	}

	var missing []string
	for _, p := range []struct{ name, path string }{
		{"worldserver.exe", r.lay.WorldserverExe},
		{"configs", filepathDir(r.lay.WorldConf)},
		{"data\\dbc", r.lay.DataDbcDir},
	} {
		if p.path == "" {
			missing = append(missing, i18n.Tf("health.path.missing", p.name))
			continue
		}
		if _, err := os.Stat(p.path); err != nil {
			missing = append(missing, p.path)
		}
	}

	if len(missing) > 0 {
		c.Detail = i18n.Tf("health.repack.absent", strings.Join(missing, "; "))
		return c
	}

	c.OK = true
	c.Detail = r.lay.Root
	return c
}

func (r *Runner) checkProcess(image, id, label string) Check {
	c := Check{ID: id, Label: label}

	pids, err := r.ops.FindByName(image)
	if err != nil {
		c.Detail = i18n.Tf("health.port.probefail", err)
		return c
	}
	if len(pids) == 0 {
		c.Detail = i18n.T("health.port.notrunning")
		return c
	}

	c.OK = true
	if len(pids) == 1 {
		c.Detail = fmt.Sprintf("PID %d", pids[0])
		return c
	}

	// Several matches is normal on this machine, so say which one is serving
	// rather than leaving the user to work it out.
	detail := i18n.Tf("health.process.count", len(pids))
	parts := make([]string, len(pids))
	for i, p := range pids {
		parts[i] = strconv.Itoa(p)
	}
	detail += strings.Join(parts, ", ") + ")"

	if owner, ok := r.ops.PortOwner(portFor(image)); ok {
		detail += i18n.Tf("health.process.portowner", owner)
	}
	c.Detail = detail
	return c
}

func (r *Runner) checkPort(port int, id, label string) Check {
	c := Check{ID: id, Label: label}

	if r.ops.Listen(port) {
		c.OK = true
		c.Detail = i18n.Tf("health.port.listening", port)
		return c
	}

	c.Detail = i18n.Tf("health.port.noanswer", port)
	return c
}

func (r *Runner) checkPing(ctx context.Context) Check {
	c := Check{ID: IDMySQLPing, Label: i18n.T("health.ping.label")}

	if r.pinger == nil {
		c.Detail = i18n.T("health.check.unconfigured")
		return c
	}

	took, err := r.pinger.Ping(ctx)
	if err != nil {
		// A port that answers while the database refuses is a different
		// problem from a port that is simply dead, and the two need
		// different next steps. The port check reports the other half; here
		// the wording makes the combination legible.
		if r.ops.Listen(portMySQL) {
			c.Detail = i18n.Tf("health.ping.foreign", portMySQL, err)
			return c
		}
		c.Detail = err.Error()
		return c
	}

	c.OK = true
	c.Detail = i18n.Tf("health.ping.ms", took.Milliseconds())
	return c
}

func portFor(image string) int {
	switch image {
	case "mysqld.exe":
		return portMySQL
	case "authserver.exe":
		return portAuth
	default:
		return portWorld
	}
}

func filepathDir(p string) string {
	i := strings.LastIndexAny(p, `/\`)
	if i < 0 {
		return ""
	}
	return p[:i]
}
