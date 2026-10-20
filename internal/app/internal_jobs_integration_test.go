//go:build integration

package app

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/lib/clock"
)

// itDBPool is itDB that also returns a pool on the throw-away database, for reading rows.
func itDBPool(t *testing.T) (*url.URL, *db.Pool) {
	t.Helper()
	pool := dbtest.TempDB(t)
	var name string
	if err := pool.QueryRow(t.Context(), "SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("current_database: %v", err)
	}
	u, err := url.Parse(os.Getenv("BMS_TEST_DB_DSN"))
	if err != nil {
		t.Fatal("BMS_TEST_DB_DSN is not a URL")
	}
	u.Path = "/" + name
	return u, pool
}

func itJobRows(ctx context.Context, t *testing.T, pool *db.Pool, name string) (total, done int) {
	t.Helper()
	err := pool.QueryRow(ctx,
		`SELECT count(*), count(*) FILTER (WHERE status = 'done') FROM core.job WHERE name = $1`, name).Scan(&total, &done)
	if err != nil {
		t.Fatalf("count %s rows: %v", name, err)
	}
	return total, done
}

// jobs.md test plan, end to end: serve on a temporary database runs jobs.purge and audit.purge
// once at start (the first slot of each hourly job), and not again within the hour.
func TestServe_StartsJobRunner_RunsPurgesOnce(t *testing.T) {
	u, pool := itDBPool(t)
	env, addr := itEnv(t, u.String())
	s := itServe(t, env, addr)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	for _, name := range []string{"jobs.purge", "audit.purge"} {
		for {
			if _, done := itJobRows(ctx, t, pool, name); done == 1 {
				break
			}
			if err := clock.Wait(ctx, clock.Real(), 50*time.Millisecond); err != nil {
				total, done := itJobRows(t.Context(), t, pool, name)
				t.Fatalf("%s never ran: %d rows, %d done: %v", name, total, done, err)
			}
		}
	}
	// Let any further tick pass; an hourly job has one slot in this hour.
	if err := clock.Wait(ctx, clock.Real(), 1500*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"jobs.purge", "audit.purge"} {
		if total, done := itJobRows(ctx, t, pool, name); total != 1 || done != 1 {
			t.Errorf("%s: %d rows, %d done, want exactly one run", name, total, done)
		}
	}
	if r := s.stop(t); r.code != ExitOK {
		t.Fatalf("status = %d, want ExitOK\n%s", r.code, r.errOut)
	}
}

// jobs.md "Lifecycle": bms check lists each registered job with its owner and schedule, and
// reads no job rows.
func TestCheck_ListsRegisteredJobs(t *testing.T) {
	env, _ := itEnv(t, itDB(t).String())
	if r := itMain(t.Context(), env, "migrate"); r.code != ExitOK {
		t.Fatalf("migrate: %d\n%s\n%s", r.code, r.out, r.errOut)
	}
	r := itMain(t.Context(), env, "check")
	if r.code != ExitOK {
		t.Fatalf("check = %d\n%s\n%s", r.code, r.out, r.errOut)
	}
	_, after, ok := strings.Cut(r.out, "jobs\n")
	if !ok {
		t.Fatalf("no jobs heading in:\n%s", r.out)
	}
	for _, want := range []string{"jobs.purge  owner jobs  every 1h0m0s", "audit.purge  owner audit  every 1h0m0s", "signals.redeliver"} {
		if !strings.Contains(after, want) {
			t.Errorf("jobs section lacks %q:\n%s", want, after)
		}
	}
}
