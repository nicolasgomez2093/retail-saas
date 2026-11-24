package httpapi

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The test trigger pauses the winner after it has acquired the production
// tenant lock. pg_blocking_pids proves that the second request is waiting
// for that very transaction before the trigger is allowed to continue.
func runCatalogRace(t *testing.T, db *pgxpool.Pool, gateTable string,
	first, second func() *httptest.ResponseRecorder) (*httptest.ResponseRecorder, *httptest.ResponseRecorder) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	table := pgx.Identifier{gateTable}.Sanitize()
	const key int64 = 413603
	execSQL(t, ctx, db, fmt.Sprintf(`CREATE FUNCTION test_catalog_race_gate() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN PERFORM pg_advisory_xact_lock(%d::bigint); RETURN NEW; END
		$$`, key))
	execSQL(t, ctx, db, "CREATE TRIGGER test_catalog_race_gate BEFORE INSERT ON "+table+
		" FOR EACH ROW EXECUTE FUNCTION test_catalog_race_gate()")
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		execSQL(t, cleanup, db, "DROP TRIGGER test_catalog_race_gate ON "+table)
		execSQL(t, cleanup, db, "DROP FUNCTION test_catalog_race_gate()")
	}()
	gate, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Rollback(context.Background())
	var holder int32
	if _, err := gate.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", key); err != nil {
		t.Fatal(err)
	}
	if err := gate.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&holder); err != nil {
		t.Fatal(err)
	}
	waiter := func(blocker int32) int32 {
		t.Helper()
		for {
			var pid int32
			err := db.QueryRow(ctx, `SELECT pid FROM pg_stat_activity
				WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) LIMIT 1`, blocker).Scan(&pid)
			if err == nil {
				return pid
			}
			if err != pgx.ErrNoRows {
				t.Fatalf("no se observó una transacción esperando a %d: %v", blocker, err)
			}
		}
	}
	a := make(chan *httptest.ResponseRecorder, 1)
	b := make(chan *httptest.ResponseRecorder, 1)
	go func() { a <- first() }()
	winner := waiter(holder)
	go func() { b <- second() }()
	waiter(winner)
	if err := gate.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wait := func(result <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
		select {
		case value := <-result:
			return value
		case <-ctx.Done():
			t.Fatal(ctx.Err())
			return nil
		}
	}
	return wait(a), wait(b)
}
