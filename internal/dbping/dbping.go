// Package dbping answers one question: is the local MySQL actually
// answering, and how long did it take?
//
// It is used twice, and both times the distinction between "the process is
// alive" and "the database answers" is the whole point. The start sequence
// pings before launching authserver, because worldserver spends about a
// minute failing against a dead database and then dies in a screenful of
// errors. The Health screen pings every five seconds, because a hung mysqld
// is an ordinary failure that a process check will never notice.
package dbping

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	// Registers the "mysql" driver with database/sql. Imported for its side
	// effect, not for its API.
	_ "github.com/go-sql-driver/mysql"

	"wow-hub/internal/i18n"
	"wow-hub/internal/repack"
)

// Connection details. temp\mysql_ac.conf carries a password and nothing
// else, so user and host are fixed here and the port comes from repack,
// which owns every other fact about this install.
const (
	user   = "acore"
	host   = "127.0.0.1"
	port   = repack.MySQLPort
	driver = "mysql"
)

// Pinger checks the database.
type Pinger interface {
	// Ping reports whether the database answered, and how long it took.
	// The duration is returned even on failure: a 30 ms failure and a 30 s
	// failure are different problems.
	Ping(ctx context.Context) (time.Duration, error)
	// Close releases the connection pool. Safe to call more than once.
	Close() error
}

type mysqlPinger struct {
	db *sql.DB
}

var _ Pinger = (*mysqlPinger)(nil)

// New builds a Pinger for the standard local install, using password.
func New(password string) (Pinger, error) {
	return NewAt(password, host, port)
}

// NewFromFile reads the password from temp\mysql_ac.conf and builds a
// Pinger.
//
// The distinction from New matters to the user: a failure here names the
// config file, while a failure inside New would be the driver's "access
// denied", which points at the server instead of at a gitignored temp file
// that may simply not exist.
func NewFromFile(confPath string) (Pinger, error) {
	password, err := loadPassword(confPath)
	if err != nil {
		return nil, err
	}
	return New(password)
}

// NewAt builds a Pinger against an explicit host and port. Tests use it to
// aim at a port nothing listens on.
func NewAt(password, h string, p int) (Pinger, error) {
	db, err := openAt(password, h, p)
	if err != nil {
		return nil, err
	}
	return &mysqlPinger{db: db}, nil
}

// Open returns a pool for read-only queries against the same MySQL the
// pinger uses, with the same credentials and the same one-connection
// limit. The caller must Close it.
//
// A separate pool from the pinger's, and that is the point. The pinger
// runs on a five-second loop and is the thing that decides whether the
// database counts as alive; a slow aggregate query sharing its single
// connection would make a busy database look like a dead one. The realm
// queries are read-only, so two pools cannot disagree about any data.
func Open(confPath string) (*sql.DB, error) {
	password, err := loadPassword(confPath)
	if err != nil {
		return nil, err
	}
	return openAt(password, host, port)
}

func openAt(password, h string, p int) (*sql.DB, error) {
	if strings.TrimSpace(password) == "" {
		return nil, errors.New(i18n.T("dbping.nopass"))
	}

	// parseTime, so a TIMESTAMP column arrives as a time.Time rather than
	// as raw bytes. Without it every query that touches last_login or
	// joindate has to parse a string itself, and the account list does.
	//
	// loc=UTC is stated rather than left to the driver's default of UTC:
	// it does not change the server's time zone, it says which zone the
	// literal the server sent is labelled with. The wall clock is
	// therefore preserved, which is what a screen showing "last played"
	// wants - converting it would move the time by the offset between the
	// server and whoever is reading.
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/?parseTime=true&loc=UTC", user, password, h, p)
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.Tf("dbping.open", MaskDSN(dsn)), err)
	}

	// One connection, and give it back promptly. The Health screen pings
	// every five seconds; an unbounded pool would accumulate connections
	// against a personal MySQL that does not need them.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Minute)

	return db, nil
}

func (p *mysqlPinger) Ping(ctx context.Context) (time.Duration, error) {
	start := time.Now()
	err := p.db.PingContext(ctx)
	return time.Since(start), err
}

func (p *mysqlPinger) Close() error {
	if p == nil || p.db == nil {
		return nil
	}
	return p.db.Close()
}

// loadPassword delegates to repack rather than parsing the file again: one
// parser, one set of error messages, one thing to keep correct.
func loadPassword(confPath string) (string, error) {
	return repack.LoadMySQLPassword(confPath)
}

// MaskDSN replaces the password in a MySQL DSN with asterisks.
//
// Anything that gets logged goes through here. The password ends up in
// logs\wow-hub.log and in screenshots a user attaches to a bug report.
func MaskDSN(dsn string) string {
	at := strings.LastIndex(dsn, "@tcp(")
	if at < 0 {
		return dsn
	}
	colon := strings.Index(dsn[:at], ":")
	if colon < 0 {
		return dsn
	}
	return dsn[:colon+1] + "****" + dsn[at:]
}
