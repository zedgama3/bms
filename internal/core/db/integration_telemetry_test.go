//go:build integration

package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func spansNamed(c *telemetrytest.Capture, name string) []telemetry.SpanData {
	var out []telemetry.SpanData
	for _, s := range c.Spans() {
		if s.Name == name {
			out = append(out, s)
		}
	}
	return out
}

func tracedPool(t *testing.T) (*db.Pool, *telemetrytest.Capture, string) {
	t.Helper()
	_, name := tempDB(t)
	c := telemetrytest.New(t)
	o := optionsFor(t, name)
	o.Tracer = c.Tracer("db")
	o.MaxConns = 3
	return openWith(t, o, nil), c, name
}

func TestOpen_WithTracer_OneSpanPerStatementUnderCallerSpan(t *testing.T) {
	p, c, _ := tracedPool(t)
	tr := c.Tracer("test")
	ctx, parent := tr.Start(ctxT(t), "test.parent")
	var n int
	if err := p.QueryRow(ctx, "SELECT $1::int", 7).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	parent.End()

	sel := spansNamed(c, "db.SELECT")
	if len(sel) == 0 {
		t.Fatalf("no db.SELECT span in %+v", c.Spans())
	}
	last := sel[len(sel)-1]
	parents := spansNamed(c, "test.parent")
	if len(parents) != 1 || last.TraceID != parents[0].TraceID || last.ParentID == "" {
		t.Errorf("the statement span must be a child of the caller's span: %+v", last)
	}
	if last.Kind != telemetry.SpanClient {
		t.Errorf("kind = %v, want SpanClient", last.Kind)
	}
	if last.Attrs["db.system"] != "postgresql" || last.Attrs["db.operation"] != "SELECT" {
		t.Errorf("attrs = %v", last.Attrs)
	}

	t.Run("transact yields BEGIN, statement, COMMIT", func(t *testing.T) {
		mustExec(t, p, "CREATE TABLE tr_t (n integer)")
		start := len(c.Spans())
		err := p.Transact(ctxT(t), db.Write, func(tx context.Context) error {
			_, e := p.Exec(tx, "INSERT INTO tr_t VALUES (1)")
			return e //nolint:wrapcheck // test closure
		})
		if err != nil {
			t.Fatalf("Transact: %v", err)
		}
		var names []string
		for _, s := range c.Spans()[start:] {
			names = append(names, s.Name)
		}
		want := []string{"db.BEGIN", "db.INSERT", "db.COMMIT"}
		if len(names) != len(want) {
			t.Fatalf("spans = %v, want %v", names, want)
		}
		for i := range want {
			if names[i] != want[i] {
				t.Fatalf("spans = %v, want %v", names, want)
			}
		}
	})
}

func TestQueryTracer_SpanNamedByOperation_Table(t *testing.T) {
	p, c, _ := tracedPool(t)
	tests := []struct{ sql, span string }{
		{"select 1", "db.SELECT"},
		{"  \n\tSELECT 1", "db.SELECT"},
		{"with x as (select 1) select * from x", "db.WITH"},
		{"show server_version", "db.SHOW"},
		{"(select 1)", "db.OTHER"},
	}
	for _, tc := range tests {
		start := len(c.Spans())
		var n int
		if err := p.QueryRow(ctxT(t), tc.sql).Scan(&n); err != nil && !errs.Is(err, errs.Internal) {
			t.Fatalf("%q: %v", tc.sql, err)
		}
		found := false
		for _, s := range c.Spans()[start:] {
			if s.Name == tc.span {
				found = true
				if s.Kind != telemetry.SpanClient || s.Attrs["db.system"] != "postgresql" {
					t.Errorf("%q: span %+v", tc.sql, s)
				}
			}
		}
		if !found {
			t.Errorf("%q: no span %s in %+v", tc.sql, tc.span, c.Spans()[start:])
		}
	}
}

func TestQueryTracer_NeverRecordsSQLOrArgs(t *testing.T) {
	p, c, _ := tracedPool(t)
	mustExec(t, p, "CREATE TABLE nm (v text)")
	if _, err := p.Exec(ctxT(t), "INSERT INTO nm VALUES ($1) /* "+marker+" */", marker); err != nil {
		t.Fatalf("insert: %v", err)
	}
	var v string
	if err := p.QueryRow(ctxT(t), "SELECT v FROM nm WHERE v = $1", marker).Scan(&v); err != nil {
		t.Fatalf("select: %v", err)
	}
	// And a failing statement carrying the marker.
	_, err := p.Exec(ctxT(t), "SELECT * FROM missing_"+marker)
	wantKind(t, err, errs.Internal)
	if c.Contains(marker) {
		t.Fatalf("statement text or an argument reached a span or a log")
	}
}

// F57
func TestQueryTracer_FailedStatement_SpanHasKindAndSQLSTATENoText(t *testing.T) {
	p, c, _ := tracedPool(t)
	start := len(c.Spans())
	_, err := p.Exec(ctxT(t), "SELECT * FROM no_such_table")
	wantKind(t, err, errs.Internal)
	var failed *telemetry.SpanData
	spans := c.Spans()[start:]
	for i := range spans {
		if spans[i].Failed {
			failed = &spans[i]
		}
	}
	if failed == nil {
		t.Fatalf("no failed span in %+v", spans)
	}
	if failed.ErrorKind == "" {
		t.Errorf("failed span has no error kind")
	}
	if failed.Attrs["db.response.status_code"] != "42P01" {
		t.Errorf("SQLSTATE attr = %q, want 42P01 (attrs %v)", failed.Attrs["db.response.status_code"], failed.Attrs)
	}
	for k, v := range failed.Attrs {
		if v == "SELECT * FROM no_such_table" {
			t.Errorf("attr %s carries the statement text", k)
		}
	}
}

func TestOpen_NilTracerAndMeter_NoSpansNoGauges(t *testing.T) {
	_, name := tempDB(t)
	c := telemetrytest.New(t)
	o := optionsFor(t, name)
	o.Tracer, o.Meter = nil, nil
	p, err := db.Open(ctxT(t), o, clock.Real(), c.Logger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer closePool(t, p)
	if got := count(t, p, "SELECT 1"); got != 1 {
		t.Fatalf("query failed")
	}
	if err := c.Telemetry().Collect(ctxT(t)); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if n := len(c.Spans()); n != 0 {
		t.Errorf("%d spans without a tracer", n)
	}
	if n := len(c.Measurements()); n != 0 {
		t.Errorf("%d measurements without a meter", n)
	}
}

func TestGauges_ObserveFromStats(t *testing.T) {
	_, name := tempDB(t)
	c := telemetrytest.New(t)
	o := optionsFor(t, name)
	o.Meter = c.Meter("db")
	o.MaxConns = 5
	p, err := db.Open(ctxT(t), o, clock.Real(), c.Logger())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	collect := func() []telemetry.Measurement {
		start := len(c.Measurements())
		if err := c.Telemetry().Collect(ctxT(t)); err != nil {
			t.Fatalf("Collect: %v", err)
		}
		return c.Measurements()[start:]
	}
	find := func(ms []telemetry.Measurement, inst string) []telemetry.Measurement {
		var out []telemetry.Measurement
		for _, m := range ms {
			if m.Instrument == inst {
				out = append(out, m)
			}
		}
		return out
	}

	var held []telemetry.Measurement
	if err := p.Transact(ctxT(t), db.Read, func(tx context.Context) error {
		var n int
		if err := p.QueryRow(tx, "SELECT 1").Scan(&n); err != nil {
			return err //nolint:wrapcheck // test closure
		}
		held = collect()
		return nil
	}); err != nil {
		t.Fatalf("Transact: %v", err)
	}
	if m := find(held, "db.client.connection.max"); len(m) != 1 || m[0].Value != 5 {
		t.Errorf("max = %+v", m)
	}
	if m := find(held, "db.client.connection.pending_requests"); len(m) != 1 || m[0].Value != 0 {
		t.Errorf("pending = %+v", m)
	}
	states := map[string]float64{}
	for _, m := range find(held, "db.client.connection.count") {
		states[m.Attrs["state"]] = m.Value
	}
	if len(states) != 2 || states["used"] != 1 {
		t.Errorf("count samples = %v, want idle and used with used=1", states)
	}

	closePool(t, p)
	if after := collect(); len(after) != 0 {
		t.Errorf("gauges must observe nothing after Close, got %+v", after)
	}
}

// F56
func TestOpen_GaugeRegistrationFails_ReturnsKindAndClosesPool(t *testing.T) {
	admin, name := tempDB(t)
	sessions := func() int {
		return count(t, admin, "SELECT count(*) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()", name)
	}
	baseline := sessions()
	o := optionsFor(t, name)
	o.Meter = refusingMeter{err: errs.E("test.meter", errs.Conflict, errors.New("duplicate name"))}
	p, err := db.Open(ctxT(t), o, clock.Real(), quietLogger())
	if p != nil {
		closePool(t, p)
		t.Fatalf("Open must fail")
	}
	wantKind(t, err, errs.Conflict)
	// The pgx pool was closed: no session of the new database remains.
	limit := 0
	for ; limit < 2000; limit++ {
		if sessions() <= baseline {
			return
		}
	}
	t.Errorf("sessions of the failed Open are still connected")
}
