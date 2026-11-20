package supervisor

import (
	"strings"
	"time"

	"wow-hub/internal/repack"
)

// ServiceID names one of the three processes this app manages.
type ServiceID string

const (
	SvcMySQL ServiceID = "mysql"
	SvcAuth  ServiceID = "auth"
	SvcWorld ServiceID = "world"
)

// serviceOrder is fixed. The UI renders rows in this order and a snapshot
// whose services changed places between refreshes would make the table
// flicker for no reason.
var serviceOrder = []ServiceID{SvcMySQL, SvcAuth, SvcWorld}

// State is what the app believes about one service.
type State string

const (
	// StateStopped: not running.
	StateStopped State = "stopped"
	// StateStarting: launched, not yet ready.
	StateStarting State = "starting"
	// StateRunning: up and, for the world, ready on its port.
	StateRunning State = "running"
	// StateStopping: a stop was requested.
	StateStopping State = "stopping"
	// StateExternal: running, but started outside this app - by the old
	// Start Server.bat, or by a previous run of the app. The clean stop path
	// is unavailable for these.
	StateExternal State = "external"
	// StateFailed: it died on its own. A worldserver that crashes must not
	// look like one that was stopped.
	StateFailed State = "failed"
)

// ServiceStatus is one row of the Server Control table.
type ServiceStatus struct {
	ID       ServiceID
	State    State
	PID      int
	MemoryMB int
	// Owned is true only for a process this app started. An adopted process
	// cannot receive a graceful signal from us in the general case.
	Owned bool
	// Since is when the app first observed this PID.
	Since time.Time
	// Detail explains ambiguity and failure: several matching processes, or
	// why something stopped.
	Detail string
	// Count is how many processes matched by image name.
	Count int
}

// Snapshot is the whole table at one instant.
type Snapshot struct {
	Services []ServiceStatus
}

func (s Snapshot) find(id ServiceID) ServiceStatus {
	for _, sv := range s.Services {
		if sv.ID == id {
			return sv
		}
	}
	return ServiceStatus{}
}

// key is what "did anything change" is decided on.
//
// MemoryMB is in it, but only coarsely - rounded to memoryBucketMB. Memory
// moves every second, and comparing the exact value would turn a delta
// stream into a per-second flood that repaints the whole table forever. The
// bucket keeps the displayed number reasonably fresh while bounding how
// often a repaint can happen.
//
// It was previously excluded entirely, which had a second cost: the value
// was recomputed on every tick but never reached the screen, because a
// status event only fires when this key changes. A number nobody updates is
// worse than no number.
func (s Snapshot) key() string {
	var b strings.Builder
	for _, sv := range s.Services {
		b.WriteString(string(sv.ID))
		b.WriteByte('|')
		b.WriteString(string(sv.State))
		b.WriteByte('|')
		b.WriteString(itoa(sv.PID))
		b.WriteByte('|')
		if sv.Owned {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
		b.WriteByte('|')
		b.WriteString(sv.Detail)
		b.WriteByte('|')
		b.WriteString(itoa(sv.Count))
		b.WriteByte('|')
		b.WriteString(itoa(sv.MemoryMB / memoryBucketMB))
		b.WriteByte(';')
	}
	return b.String()
}

// memoryBucketMB is the granularity at which a memory change counts as a
// change. Small enough that the figure on screen tracks reality, large
// enough that ordinary allocator noise does not repaint the table.
const memoryBucketMB = 8

// StartTimeouts bounds each step of the start sequence.
type StartTimeouts struct {
	MySQLPort   time.Duration
	DBPing      time.Duration
	AuthPort    time.Duration
	WorldMarker time.Duration
}

// Conf holds every tunable the supervisor has.
type Conf struct {
	// StopTimeout is how long a process gets to exit after CTRL_BREAK before
	// the app asks about killing it. One value for world and auth: no
	// decision here varies it, and a field nobody varies stays untested.
	StopTimeout time.Duration
	// ConfirmTimeout is how long the force-kill dialog may stay unanswered.
	// After it, silence counts as a refusal, so the backend cannot sit
	// blocked on the UI forever.
	ConfirmTimeout time.Duration
	Start          StartTimeouts
}

// Confirmer is how the supervisor asks the UI whether to force-kill.
//
// Task 8 implements it over Wails events. It is declared here so New's
// signature is final and no caller has to change later.
type Confirmer interface {
	AskConfirm(service string, pid int) <-chan bool
}

// defaults for a Conf built without one.
const (
	defaultStopTimeout    = 30 * time.Second
	defaultConfirmTimeout = 60 * time.Second
	defaultMySQLPort      = 60 * time.Second
	defaultDBPing         = 10 * time.Second
	defaultAuthPort       = 30 * time.Second
	defaultWorldMarker    = 180 * time.Second
)

func (c Conf) withDefaults() Conf {
	if c.StopTimeout <= 0 {
		c.StopTimeout = defaultStopTimeout
	}
	if c.ConfirmTimeout <= 0 {
		c.ConfirmTimeout = defaultConfirmTimeout
	}
	if c.Start.MySQLPort <= 0 {
		c.Start.MySQLPort = defaultMySQLPort
	}
	if c.Start.DBPing <= 0 {
		c.Start.DBPing = defaultDBPing
	}
	if c.Start.AuthPort <= 0 {
		c.Start.AuthPort = defaultAuthPort
	}
	if c.Start.WorldMarker <= 0 {
		c.Start.WorldMarker = defaultWorldMarker
	}
	return c
}

// ports the app waits on. The world socket, not its initialization line:
// see the spike's RESULT.md.
//
// The values live in repack, which owns every other fact about this install;
// health and dbping read the same constants, so a repack on different ports
// is edited once.
const (
	portMySQL = repack.MySQLPort
	portAuth  = repack.AuthPort
	portWorld = repack.WorldPort
)

// dependents names, for each service, the services that must be gone before
// it may be stopped.
//
// This is the stop order expressed as data. worldserver writes to the
// database as it shuts down, so a MySQL that leaves while the world is still
// running leaves the world writing into a dead connection - which is how a
// character that had not been saved yet is lost. authserver holds the same
// relationship to MySQL, and the world needs auth for logins.
//
// It is enforced by stopOne, so it covers every route to a stop: the row
// buttons, StopAll, and Restart, which stops before it starts.
//
// Order within a list is deliberate. The guard reports the first dependent
// still running, and when both are up the one that has to be stopped first
// is the deepest - the world, not auth. Naming auth would send the user to
// stop the wrong process and leave the world running against the same dead
// database a moment later.
var dependents = map[ServiceID][]ServiceID{
	SvcMySQL: {SvcWorld, SvcAuth},
	SvcAuth:  {SvcWorld},
	SvcWorld: nil,
}

// imageExe is the executable each service is recognized by. MySQL's image
// name matches two processes on Windows; portOwner disambiguates.
var imageExe = map[ServiceID]string{
	SvcMySQL: "mysqld.exe",
	SvcAuth:  "authserver.exe",
	SvcWorld: "worldserver.exe",
}

func servicePort(id ServiceID) int {
	switch id {
	case SvcMySQL:
		return portMySQL
	case SvcAuth:
		return portAuth
	default:
		return portWorld
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
