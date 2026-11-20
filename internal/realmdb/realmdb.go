// Package realmdb reads the parts of the realm that the server itself does
// not show anywhere: how big each database is, and who the characters are.
//
// Read-only by construction. Every statement in this file is a SELECT, the
// package is handed a pool it does not own, and it never writes. That is
// the whole reason these screens can exist before the ones that modify
// data: looking cannot lose a character.
package realmdb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"wow-hub/internal/i18n"
	"wow-hub/internal/repack"
)

// Database is one schema and its footprint on disk.
type Database struct {
	Name string
	// Tables is 0 for a schema that does not exist, which is worth showing:
	// a missing database is a broken install, not an empty one.
	Tables    int
	SizeBytes int64
}

// Realm is what the overview tiles and the maintenance screen show.
//
// A single struct with an Err field rather than a returned error, because
// "MySQL is not running" is the ordinary state of this application, not a
// failure of the read. The screen then says why the numbers are missing
// instead of showing zeros that look like a real measurement.
type Realm struct {
	Databases  []Database
	TotalBytes int64
	Characters int64
	Accounts   int64
	ReadAt     time.Time
	Err        string
}

// Character is one row of the character list.
type Character struct {
	GUID    int
	Name    string
	Level   int
	Race    int
	Class   int
	Account string
	Online  bool
}

// Characters is the list plus the total, so the screen can say "first N of
// M" rather than silently truncating.
type Characters struct {
	List  []Character
	Total int64
	Err   string
}

// Read measures the four databases and counts what is in two of them.
//
// A nil pool is not a programming error to panic on: the pool is nil
// whenever temp\mysql_ac.conf is missing, and the window is expected to
// open and explain that.
func Read(ctx context.Context, db *sql.DB) Realm {
	r := Realm{ReadAt: time.Now()}
	if db == nil {
		r.Err = i18n.T("rdb.nodb")
		return r
	}

	sizes, err := schemaSizes(ctx, db)
	if err != nil {
		r.Err = err.Error()
		return r
	}

	// Every schema is reported, in a fixed order, including one that does
	// not exist. A list that quietly loses a row when something is wrong is
	// the failure mode this whole screen exists to avoid.
	r.Databases = make([]Database, 0, len(repack.Databases))
	for _, name := range repack.Databases {
		d := Database{Name: name}
		if got, ok := sizes[name]; ok {
			d.Tables, d.SizeBytes = got.Tables, got.SizeBytes
		}
		r.Databases = append(r.Databases, d)
		r.TotalBytes += d.SizeBytes
	}

	if n, cerr := count(ctx, db, "SELECT COUNT(*) FROM `"+repack.DBCharacters+"`.`characters`"); cerr == nil {
		r.Characters = n
	}
	if n, aerr := count(ctx, db, "SELECT COUNT(*) FROM `"+repack.DBAuth+"`.`account`"); aerr == nil {
		r.Accounts = n
	}

	return r
}

func schemaSizes(ctx context.Context, db *sql.DB) (map[string]Database, error) {
	// Placeholders are generated, the values are bound. The schema names
	// come from a constant list in repack, so nothing here can carry a
	// caller's text into the statement.
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(repack.Databases)), ",")
	args := make([]any, 0, len(repack.Databases))
	for _, name := range repack.Databases {
		args = append(args, name)
	}

	query := `SELECT table_schema,
	                 COUNT(*),
	                 COALESCE(SUM(data_length + index_length), 0)
	          FROM information_schema.tables
	          WHERE table_schema IN (` + placeholders + `)
	          GROUP BY table_schema`

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("rdb.sizes"), err)
	}
	defer rows.Close()

	out := make(map[string]Database, len(repack.Databases))
	for rows.Next() {
		var d Database
		if err := rows.Scan(&d.Name, &d.Tables, &d.SizeBytes); err != nil {
			return nil, fmt.Errorf("%s: %w", i18n.T("rdb.sizes"), err)
		}
		out[d.Name] = d
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T("rdb.sizes"), err)
	}
	return out, nil
}

func count(ctx context.Context, db *sql.DB, query string) (int64, error) {
	var n int64
	if err := db.QueryRowContext(ctx, query).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// List returns the characters, highest level first, capped at limit.
//
// The account name comes from acore_auth through a join, because the
// character table only stores an account id and "which account is this on"
// is the first question anyone asks of a character list.
func List(ctx context.Context, db *sql.DB, limit int) Characters {
	var out Characters
	if db == nil {
		out.Err = i18n.T("rdb.nodb")
		return out
	}
	if limit <= 0 {
		limit = defaultLimit
	}

	out.Total, _ = count(ctx, db, "SELECT COUNT(*) FROM `"+repack.DBCharacters+"`.`characters`")

	// `characters` is a reserved word in MySQL 8 and must be quoted. The
	// schema names come from repack, so the only thing interpolated here is
	// a constant.
	query := "SELECT c.guid, c.name, c.level, c.race, c.class, COALESCE(a.username, ''), c.online" +
		" FROM `" + repack.DBCharacters + "`.`characters` c" +
		" LEFT JOIN `" + repack.DBAuth + "`.`account` a ON a.id = c.account" +
		" ORDER BY c.level DESC, c.name" +
		" LIMIT ?"

	rows, err := db.QueryContext(ctx, query, limit)
	if err != nil {
		out.Err = i18n.T("rdb.chars") + ": " + err.Error()
		return out
	}
	defer rows.Close()

	for rows.Next() {
		var (
			c      Character
			online int
		)
		if err := rows.Scan(&c.GUID, &c.Name, &c.Level, &c.Race, &c.Class, &c.Account, &online); err != nil {
			out.Err = i18n.T("rdb.chars") + ": " + err.Error()
			return out
		}
		c.Online = online != 0
		out.List = append(out.List, c)
	}
	if err := rows.Err(); err != nil {
		out.Err = i18n.T("rdb.chars") + ": " + err.Error()
	}

	return out
}

// defaultLimit is generous on purpose. This repack holds a few hundred
// characters, so the whole list fits in one answer and the search box can
// filter it without a round trip per keystroke. A realm large enough to
// exceed it is told that the list was cut.
const defaultLimit = 2000

// Account is one row of the account list.
//
// Credentials are absent on purpose, and the query never asks for them:
// `account` holds a salt, a verifier, a session key and a TOTP secret, and
// none of those belongs on a screen that ends up in a screenshot attached
// to a bug report. A list that never selects them cannot leak them.
type Account struct {
	ID       int
	Username string
	// GMLevel is the highest security level the account holds on any
	// realm. 0 is an ordinary player.
	GMLevel int
	// Online says the account has a character logged in.
	Online bool
	// Locked says the account has been locked out.
	Locked bool
	// Joined is when the account was created.
	Joined time.Time
	// LastLogin is when it last signed in. The zero time means never,
	// which is what the page has to print anyway.
	LastLogin time.Time
	LastIP    string
	// TotalTimeSec is how long the account has played, in seconds. The
	// column is seconds since the account was made, so it is only ever
	// shown as hours.
	TotalTimeSec int64
}

// Accounts is the account list plus the total, so the screen can say "first
// N of M" rather than silently truncating.
type Accounts struct {
	List  []Account
	Total int64
	Err   string
}

// accountQuery is the account list's statement.
//
// At package level so a test can read it: this is the one statement in the
// package that touches a table holding credentials, and "the query does not
// select them" is a claim worth checking rather than trusting.
//
// The security level comes from account_access through a correlated
// subquery rather than a join and a GROUP BY: an account can hold a level
// on more than one realm, and a join would then return it once per realm.
// Schema names come from repack, so the only text interpolated here is a
// constant.
var accountQuery = "SELECT a.id, a.username, a.joindate, a.last_login, a.last_ip," +
	" a.online, a.locked, a.totaltime," +
	" COALESCE((SELECT MAX(x.gmlevel) FROM `" + repack.DBAuth + "`.`account_access` x" +
	" WHERE x.id = a.id), 0)" +
	" FROM `" + repack.DBAuth + "`.`account` a" +
	" ORDER BY a.last_login DESC, a.username" +
	" LIMIT ?"

// ListAccounts returns the accounts, most recently played first.
func ListAccounts(ctx context.Context, db *sql.DB, limit int) Accounts {
	var out Accounts
	if db == nil {
		out.Err = i18n.T("rdb.nodb")
		return out
	}
	if limit <= 0 {
		limit = defaultLimit
	}

	out.Total, _ = count(ctx, db, "SELECT COUNT(*) FROM `"+repack.DBAuth+"`.`account`")

	rows, err := db.QueryContext(ctx, accountQuery, limit)
	if err != nil {
		out.Err = i18n.T("rdb.accounts") + ": " + err.Error()
		return out
	}
	defer rows.Close()

	for rows.Next() {
		var (
			a      Account
			last   sql.NullTime
			online int
			locked int
		)
		if err := rows.Scan(&a.ID, &a.Username, &a.Joined, &last, &a.LastIP,
			&online, &locked, &a.TotalTimeSec, &a.GMLevel); err != nil {
			out.Err = i18n.T("rdb.accounts") + ": " + err.Error()
			return out
		}
		a.Online = online != 0
		a.Locked = locked != 0
		if last.Valid {
			a.LastLogin = last.Time
		}
		out.List = append(out.List, a)
	}
	if err := rows.Err(); err != nil {
		out.Err = i18n.T("rdb.accounts") + ": " + err.Error()
	}

	return out
}
