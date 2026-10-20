//go:build integration

package notify_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"        //nolint:depguard // faultDB must implement db.DB, whose Query returns pgx.Rows
	"github.com/jackc/pgx/v5/pgconn" //nolint:depguard // faultDB must implement db.DB, whose Exec returns pgconn.CommandTag

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/notify/notifytest"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// faultDB wraps a db.DB and fails on demand: readOnly behaves like a replica (writes refused
// with db.ErrReadOnly, ReadOnly() true); failAll fails every operation with err.
type faultDB struct {
	db.DB
	mu       sync.Mutex
	readOnly bool
	failAll  error
	onTx     func()
}

func (f *faultDB) set(ro bool, fail error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readOnly, f.failAll = ro, fail
}

func (f *faultDB) state() (readOnly bool, hook func(), fail error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readOnly, f.onTx, f.failAll
}

func (f *faultDB) Transact(ctx context.Context, mode db.Mode, fn func(context.Context) error) error {
	ro, hook, fail := f.state()
	if hook != nil {
		hook()
	}
	if fail != nil {
		return fail
	}
	if mode == db.Write && ro {
		return errs.E("test.faultDB", errs.Unavailable, db.ErrReadOnly)
	}
	return f.DB.Transact(ctx, mode, fn) //nolint:wrapcheck // test double
}

func (f *faultDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	ro, hook, fail := f.state()
	if hook != nil {
		hook()
	}
	if fail != nil {
		return pgconn.CommandTag{}, fail
	}
	if ro {
		return pgconn.CommandTag{}, errs.E("test.faultDB", errs.Unavailable, db.ErrReadOnly)
	}
	return f.DB.Exec(ctx, sql, args...) //nolint:wrapcheck // test double
}

// writes reports whether a statement changes data (the notify SQL files are plain
// INSERT/UPDATE/DELETE, some with RETURNING and so run through Query).
func writes(sql string) bool {
	u := strings.ToUpper(sql)
	return strings.Contains(u, "INSERT INTO") || strings.Contains(u, "UPDATE CORE.") ||
		strings.Contains(u, "DELETE FROM")
}

func (f *faultDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	ro, hook, fail := f.state()
	if hook != nil {
		hook()
	}
	if fail != nil {
		return nil, fail
	}
	if ro && writes(sql) {
		return nil, errs.E("test.faultDB", errs.Unavailable, db.ErrReadOnly)
	}
	return f.DB.Query(ctx, sql, args...) //nolint:wrapcheck // test double
}

type errRow struct{ err error }

func (e errRow) Scan(...any) error { return e.err }

func (f *faultDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	ro, hook, fail := f.state()
	if hook != nil {
		hook()
	}
	if fail != nil {
		return errRow{fail}
	}
	if ro && writes(sql) {
		return errRow{errs.E("test.faultDB", errs.Unavailable, db.ErrReadOnly)}
	}
	return f.DB.QueryRow(ctx, sql, args...)
}

func (f *faultDB) ReadOnly() bool {
	f.mu.Lock()
	ro := f.readOnly
	f.mu.Unlock()
	return ro || f.DB.ReadOnly()
}

// irig is a rig over a real, migrated, throw-away database.
type irig struct {
	*rig
	pool *db.Pool
	flt  *faultDB
}

func migrate(t testing.TB, pool *db.Pool) {
	t.Helper()
	mig, err := db.NewMigrator(pool, slog.New(slog.DiscardHandler))
	wantNoErr(t, err)
	_, err = mig.Migrate(ctxT(t), []db.Set{identity.Set, people.Set, notify.Set})
	wantNoErr(t, err)
}

func newIRig(t *testing.T, mods ...func(*notify.Options)) *irig {
	t.Helper()
	pool := dbtest.TempDB(t)
	migrate(t, pool)
	return newIRigOn(t, pool, mods...)
}

func newIRigOn(t *testing.T, pool *db.Pool, mods ...func(*notify.Options)) *irig {
	t.Helper()
	flt := &faultDB{DB: pool}
	r := newRig(t, flt, mods...)
	return &irig{rig: r, pool: pool, flt: flt}
}

// dbPerson inserts a core.person row and gives the person an address.
func (r *irig) dbPerson(addr string) model.PersonID {
	r.t.Helper()
	p := newPerson()
	if _, err := r.pool.Exec(ctxT(r.t),
		"INSERT INTO core.person (id, created_at, updated_at) VALUES ($1, now(), now())", [16]byte(p)); err != nil {
		r.t.Fatalf("inserting a person: %v", err)
	}
	if addr != "" {
		r.addrs.set(p, mustEmail(r.t, addr))
	}
	return p
}

func (r *irig) exec(sql string, args ...any) {
	r.t.Helper()
	if _, err := r.pool.Exec(ctxT(r.t), sql, args...); err != nil {
		r.t.Fatalf("exec %q: %v", sql, err)
	}
}

type row struct {
	ID                                   [16]byte
	Owner, Template, Language, Status    string
	Attempts                             int
	Key, Address, Subject, Body, LastErr *string
	Person                               *[16]byte
	Next, Created, Expires               time.Time
	Leased, SentAt, FailedAt             *time.Time
}

const rowCols = `id, owner, template, language, status, attempts, key, address, subject, body, last_error,
	person_id, next_attempt_at, created_at, expires_at, leased_until, sent_at, failed_at`

func (r *irig) rows(where string, args ...any) []row {
	r.t.Helper()
	q := "SELECT " + rowCols + " FROM core.notify_message"
	if where != "" {
		q += " WHERE " + where
	}
	q += " ORDER BY created_at, id"
	rs, err := r.pool.Query(ctxT(r.t), q, args...)
	if err != nil {
		r.t.Fatalf("query rows: %v", err)
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		var x row
		if err := rs.Scan(&x.ID, &x.Owner, &x.Template, &x.Language, &x.Status, &x.Attempts, &x.Key, &x.Address,
			&x.Subject, &x.Body, &x.LastErr, &x.Person, &x.Next, &x.Created, &x.Expires, &x.Leased, &x.SentAt, &x.FailedAt); err != nil {
			r.t.Fatalf("scan: %v", err)
		}
		out = append(out, x)
	}
	if err := rs.Err(); err != nil {
		r.t.Fatalf("rows: %v", err)
	}
	return out
}

func (r *irig) rowsFor(p model.PersonID) []row { return r.rows("person_id = $1", [16]byte(p)) }

func (r *irig) one(p model.PersonID) row {
	r.t.Helper()
	rs := r.rowsFor(p)
	if len(rs) != 1 {
		r.t.Fatalf("person has %d rows, want 1", len(rs))
	}
	return rs[0]
}

func (r *irig) byID(id [16]byte) row {
	r.t.Helper()
	rs := r.rows("id = $1", id)
	if len(rs) != 1 {
		r.t.Fatalf("no row %x", id)
	}
	return rs[0]
}

func (r *irig) count() int {
	r.t.Helper()
	var n int
	if err := r.pool.QueryRow(ctxT(r.t), "SELECT count(*) FROM core.notify_message").Scan(&n); err != nil {
		r.t.Fatalf("count: %v", err)
	}
	return n
}

func (r *irig) advance(d time.Duration) {
	r.t.Helper()
	wantNoErr(r.t, r.clk.Advance(d))
}

// queue sends one greet to a new person with an address and returns the person.
func (r *irig) queue(addr string) model.PersonID {
	r.t.Helper()
	p := r.dbPerson(addr)
	wantNoErr(r.t, r.vol.Send(ctxT(r.t), greet(p)))
	return p
}

func (r *irig) sendOnce() notify.Sent {
	r.t.Helper()
	s, err := r.svc.SendOnce(ctxT(r.t))
	wantNoErr(r.t, err)
	return s
}

// metric sums a counter's measurements whose attribute key equals val ("" matches any).
func (r *rig) metric(instrument, key, val string) float64 {
	var sum float64
	for _, m := range r.cap.Measurements() {
		if m.Instrument == instrument && (key == "" || m.Attrs[key] == val) {
			sum += m.Value
		}
	}
	return sum
}

func (r *irig) admin(perms ...string) context.Context {
	p := r.dbPerson("")
	for _, perm := range perms {
		r.az.Grant(p, permOf(perm))
	}
	return as(ctxT(r.t), p)
}

func newNotifytestChannel() *notifytest.Channel { return notifytest.NewChannel() }

func runtimeYield() { runtime.Gosched() }

func splitAddr(addr string) (host string, port int, err error) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, fmt.Errorf("split %q: %w", addr, err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return "", 0, fmt.Errorf("port %q: %w", p, err)
	}
	return h, n, nil
}

// mailpitExpectOne checks Mailpit holds exactly one message to addr, then deletes it.
func mailpitExpectOne(t *testing.T, api, addr string) {
	t.Helper()
	api = strings.TrimRight(api, "/")
	get := func() []string {
		req, err := http.NewRequestWithContext(ctxT(t), http.MethodGet, api+"/api/v1/search?query="+url.QueryEscape("to:"+addr), nil) //nolint:gosec // G704: the URL is the test environment's BMS_TEST_MAILPIT_URL, never input
		wantNoErr(t, err)
		resp, err := http.DefaultClient.Do(req) //nolint:gosec // G704: test environment URL
		if err != nil {
			t.Fatalf("Mailpit API unreachable: %v", err)
		}
		defer func() { ignore(resp.Body.Close()) }()
		var out struct {
			Messages []struct{ ID string } `json:"messages"`
		}
		wantNoErr(t, json.NewDecoder(resp.Body).Decode(&out))
		ids := []string{}
		for _, m := range out.Messages {
			ids = append(ids, m.ID)
		}
		return ids
	}
	ids := get()
	t.Cleanup(func() {
		body, merr := json.Marshal(map[string][]string{"IDs": ids})
		if merr != nil {
			return
		}
		req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, api+"/api/v1/messages", bytes.NewReader(body)) //nolint:gosec // G704: same URL as above
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		if resp, err := http.DefaultClient.Do(req); err == nil { //nolint:gosec // G704: test environment URL
			ignore(resp.Body.Close())
		}
	})
	if len(ids) != 1 {
		t.Fatalf("Mailpit holds %d messages for the recipient, want exactly 1", len(ids))
	}
}

// force sets a row to a status without going through the service, satisfying the table's
// checks (a lease for sending, sent_at and cleared text for sent).
func (r *irig) force(id [16]byte, status string) {
	r.t.Helper()
	switch status {
	case "sending":
		r.exec("UPDATE core.notify_message SET status='sending', leased_until=$1 WHERE id=$2", r.clk.Now().Add(time.Minute), id)
	case "sent":
		r.exec("UPDATE core.notify_message SET status='sent', sent_at=$1, subject=NULL, body=NULL, address=NULL WHERE id=$2", r.clk.Now(), id)
	default:
		r.t.Fatalf("force %q", status)
	}
}

func noTo(m notify.Message) notify.Message { m.To = nil; return m }

func toOne(m notify.Message, p model.PersonID) notify.Message {
	m.To, m.Key = []model.PersonID{p}, ""
	return m
}

func (c *chanFake) script(fn func(notify.Envelope) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fn = fn
}

func (c *chanFake) failWith(err error) {
	c.script(func(notify.Envelope) error { return err })
}

func actionsOf(es []audit.Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = string(e.Action)
	}
	return out
}

// statusOf reaches a Status through its text: the spec gives a Status constant and the Sent
// counter struct the same name, so tests never name the constant.
func statusOf(t testing.TB, name string) notify.Status {
	t.Helper()
	s, err := notify.ParseStatus(name)
	if err != nil {
		t.Fatalf("ParseStatus(%q): %v", name, err)
	}
	return s
}

func permOf(s string) authz.Permission { return authz.Permission(s) }

// first returns the first of two results; errOf the error of a call's results.
func first[T, U any](v T, _ U) T { return v }
