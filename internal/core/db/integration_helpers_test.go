//go:build integration

package db_test

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/secret"
)

//go:embed testdata/migrations
var migrationFixtures embed.FS

// fixtureSet returns a migration Set from testdata/migrations/<name>.
func fixtureSet(t testing.TB, name string) db.Set {
	t.Helper()
	sub, err := fs.Sub(migrationFixtures, "testdata/migrations/"+name)
	if err != nil {
		t.Fatalf("fs.Sub: %v", err)
	}
	return db.Set{Name: name, Schema: name, Files: sub}
}

func ctxT(t testing.TB) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func adminDSN(t testing.TB) *url.URL {
	t.Helper()
	raw := os.Getenv("BMS_TEST_DB_DSN")
	if raw == "" {
		t.Skip("BMS_TEST_DB_DSN not set")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("BMS_TEST_DB_DSN is not a URL")
	}
	return u
}

// tempDB returns a pool on a fresh throw-away database and that database's name.
func tempDB(t testing.TB) (*db.Pool, string) {
	t.Helper()
	p := dbtest.TempDB(t)
	var name string
	if err := p.QueryRow(ctxT(t), "SELECT current_database()").Scan(&name); err != nil {
		t.Fatalf("current_database: %v", err)
	}
	return p, name
}

// urlFor returns the test DSN pointed at database name, optionally as another role.
func urlFor(t testing.TB, name string) *url.URL {
	t.Helper()
	u := *adminDSN(t)
	u.Path = "/" + name
	return &u
}

func optionsFor(t testing.TB, name string) db.Options {
	t.Helper()
	return db.Options{
		URL:            secret.New(urlFor(t, name).String()),
		MaxConns:       4,
		ConnectTimeout: 10 * time.Second,
		QueryTimeout:   10 * time.Second,
	}
}

// openWith opens a second pool on the database called name and closes it at cleanup.
func openWith(t testing.TB, o db.Options, clk clock.Clock) *db.Pool {
	t.Helper()
	if clk == nil {
		clk = clock.Real()
	}
	p, err := db.Open(ctxT(t), o, clk, quietLogger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { closePool(t, p) })
	return p
}

func closePool(t testing.TB, p *db.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Close(ctx); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func mustExec(t testing.TB, q db.Querier, sql string, args ...any) {
	t.Helper()
	if _, err := q.Exec(ctxT(t), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func count(t testing.TB, q db.Querier, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := q.QueryRow(ctxT(t), sql, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

func migrator(t testing.TB, p *db.Pool) *db.Migrator {
	t.Helper()
	m, err := db.NewMigrator(p, quietLogger())
	if err != nil {
		t.Fatalf("NewMigrator: %v", err)
	}
	return m
}

// setReadOnly freezes new sessions on the database with the manual lever of decision 7.
func setReadOnly(t testing.TB, admin db.Querier, name string, on bool) {
	t.Helper()
	if on {
		mustExec(t, admin, `ALTER DATABASE "`+name+`" SET default_transaction_read_only = on`)
		return
	}
	mustExec(t, admin, `ALTER DATABASE "`+name+`" RESET default_transaction_read_only`)
}

// readOnlyPool opens a pool on a database whose new sessions are read-only.
func readOnlyPool(t testing.TB, admin db.Querier, name string) *db.Pool {
	t.Helper()
	setReadOnly(t, admin, name, true)
	return openWith(t, optionsFor(t, name), nil)
}

// withHeldConn runs fn while one connection of p is busy in a read transaction, so a new
// statement must open a fresh session (which sees ALTER DATABASE changes made inside fn).
func withHeldConn(t testing.TB, p *db.Pool, fn func()) {
	t.Helper()
	err := p.Transact(ctxT(t), db.Read, func(ctx context.Context) error {
		var one int
		if err := p.QueryRow(ctx, "SELECT 1").Scan(&one); err != nil {
			return err //nolint:wrapcheck // test closure
		}
		fn()
		return nil
	})
	if err != nil {
		t.Fatalf("held transaction: %v", err)
	}
}

// proxy is a TCP forwarder to the test database that can be killed to simulate an outage.
type proxy struct {
	l     net.Listener
	mu    sync.Mutex
	conns []net.Conn
	wg    sync.WaitGroup
}

func newProxy(t testing.TB) *proxy {
	t.Helper()
	target := adminDSN(t).Host
	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	var dialer net.Dialer
	px := &proxy{l: l}
	px.wg.Add(1)
	go func() {
		defer px.wg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			up, err := dialer.DialContext(context.Background(), "tcp", target)
			if err != nil {
				_ = c.Close() //nolint:errcheck // best effort in a test double
				continue
			}
			px.mu.Lock()
			px.conns = append(px.conns, c, up)
			px.mu.Unlock()
			go func() { _, _ = io.Copy(up, c); _ = up.Close() }() //nolint:errcheck // test double
			go func() { _, _ = io.Copy(c, up); _ = c.Close() }()  //nolint:errcheck // test double
		}
	}()
	t.Cleanup(px.Kill)
	return px
}

// Kill stops listening and drops every connection.
func (px *proxy) Kill() {
	_ = px.l.Close() //nolint:errcheck // idempotent teardown
	px.mu.Lock()
	for _, c := range px.conns {
		_ = c.Close() //nolint:errcheck // idempotent teardown
	}
	px.mu.Unlock()
	px.wg.Wait()
}

func (px *proxy) URL(t testing.TB, name string) string {
	t.Helper()
	u := urlFor(t, name)
	u.Host = px.l.Addr().String()
	return u.String()
}

func randHex(t testing.TB) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}

// fakeMeter is a telemetry.Meter whose Gauge registration always fails.
type refusingMeter struct{ err error }

func (m refusingMeter) Counter(string, ...telemetry.InstrumentOption) (telemetry.Counter, error) {
	return nil, m.err
}

func (m refusingMeter) Histogram(string, ...telemetry.InstrumentOption) (telemetry.Histogram, error) {
	return nil, m.err
}

func (m refusingMeter) Gauge(string, func(context.Context) []telemetry.Sample, ...telemetry.InstrumentOption) error {
	return m.err
}
