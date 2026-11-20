package realmdb

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	// Registers the driver, as the package under test does.
	_ "github.com/go-sql-driver/mysql"

	"wow-hub/internal/i18n"
)

func TestMain(m *testing.M) {
	// The assertions pin exact Russian wording; the process-wide language
	// defaults to English now, so the tests pin theirs explicitly.
	i18n.Set(i18n.Ru)
	os.Exit(m.Run())
}

// A nil pool is the ordinary state of this app - it is nil whenever
// temp\mysql_ac.conf is missing - and the window still has to open and
// explain itself. A panic here would take down the panel that stops a
// running world.
func TestReadDegradesWhenThereIsNoConnection(t *testing.T) {
	r := Read(context.Background(), nil)

	if r.Err == "" {
		t.Fatal("want an explanation, got silence")
	}
	if len(r.Databases) != 0 {
		t.Errorf("want no rows, got %d", len(r.Databases))
	}
	if r.Characters != 0 || r.Accounts != 0 {
		t.Errorf("want zero counts, got %d/%d", r.Characters, r.Accounts)
	}
}

func TestListDegradesWhenThereIsNoConnection(t *testing.T) {
	got := List(context.Background(), nil, 10)

	if got.Err == "" {
		t.Fatal("want an explanation, got silence")
	}
	if len(got.List) != 0 {
		t.Errorf("want no rows, got %d", len(got.List))
	}
}

func TestListAccountsDegradesWhenThereIsNoConnection(t *testing.T) {
	got := ListAccounts(context.Background(), nil, 10)

	if got.Err == "" {
		t.Fatal("want an explanation, got silence")
	}
	if len(got.List) != 0 {
		t.Errorf("want no rows, got %d", len(got.List))
	}
}

// The account list is the one screen that reads a table holding
// credentials, so the statement is checked rather than trusted: `account`
// carries a salt, a verifier, a session key and a TOTP secret, and a query
// that selected them would put them in a screenshot attached to a bug
// report.
func TestTheAccountQueryNeverSelectsCredentials(t *testing.T) {
	query := accountQuery

	for _, column := range []string{"salt", "verifier", "session_key", "totp_secret"} {
		if strings.Contains(strings.ToLower(query), column) {
			t.Errorf("запрос читает %s: %s", column, query)
		}
	}
	for _, column := range []string{"id", "username", "last_login"} {
		if !strings.Contains(strings.ToLower(query), column) {
			t.Errorf("в запросе нет %s — список будет пустым: %s", column, query)
		}
	}
}

// A pool that exists but points at nothing is the other half of the same
// story: MySQL is installed and stopped, which is how this app is usually
// found. The error must come back as a value on the result, not as a panic
// and not as zeros that look like a measurement.
func TestReadReportsAnUnreachableDatabaseAsAValue(t *testing.T) {
	db, err := sql.Open("mysql", "acore:whatever@tcp(127.0.0.1:1)/")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := Read(ctx, db)
	if r.Err == "" {
		t.Fatal("want a reason for the missing numbers, got none")
	}
	if !strings.Contains(strings.ToLower(r.Err), "размеры баз") {
		t.Errorf("Err = %q, want it to name the step that failed", r.Err)
	}

	if got := List(ctx, db, 10); got.Err == "" {
		t.Error("List must report the same failure rather than an empty list")
	}
}
