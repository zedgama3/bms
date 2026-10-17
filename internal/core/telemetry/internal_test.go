package telemetry

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/zedgama3/bms/internal/lib/clock"
)

func TestParseTraceparent_AcceptsOnlyVersion00WithNonZeroIDs(t *testing.T) {
	const good = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	cases := []struct {
		name, in string
		ok       bool
	}{
		{"valid", good, true},
		{"flags ignored", strings.TrimSuffix(good, "01") + "00", true},
		{"unknown version", "01" + good[2:], false},
		{"upper case hex", strings.ToUpper(good), false},
		{"too short", good[:40], false},
		{"zero trace id", "00-00000000000000000000000000000000-b7ad6b7169203331-01", false},
		{"zero parent id", "00-0af7651916cd43dd8448eb211c80319c-0000000000000000-01", false},
		{"trailing data", good + "-extra", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc, ok := parseTraceparent(tc.in)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && (sc.IsSampled() || !sc.IsRemote()) {
				t.Errorf("flags must be cleared and the parent remote: sampled=%v remote=%v", sc.IsSampled(), sc.IsRemote())
			}
		})
	}
}

func TestCapBytes_FitsLimitAndKeepsValidUTF8(t *testing.T) {
	for _, s := range []string{strings.Repeat("a", 600), strings.Repeat("é", 400), strings.Repeat("€", 300)} {
		got := capBytes(s, MaxTextBytes)
		if len(got) > MaxTextBytes || !utf8.ValidString(got) || !strings.HasSuffix(got, ellipsis) {
			t.Errorf("capBytes(%d bytes) = %d bytes, valid=%v", len(s), len(got), utf8.ValidString(got))
		}
	}
	if got := capBytes("short", MaxTextBytes); got != "short" {
		t.Errorf("short string changed: %q", got)
	}
}

func TestGate_AllowsOncePerKeyPerPeriod(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	clk := &stepClock{now: now}
	g := newGate(clk, time.Minute)
	if !g.allow("a") || g.allow("a") {
		t.Fatal("first call must pass and the second must not")
	}
	if !g.allow("b") {
		t.Error("another key is independent")
	}
	clk.now = now.Add(time.Minute)
	if !g.allow("a") {
		t.Error("a key passes again after the period")
	}
}

type stepClock struct{ now time.Time }

func (c *stepClock) Now() time.Time { return c.now }

func TestNest_WrapsTailInOpenGroupsInOrder(t *testing.T) {
	ops := []op{
		{attrs: []slog.Attr{slog.Int("a", 1)}},
		{group: "g"},
		{attrs: []slog.Attr{slog.Int("b", 2)}},
		{group: "empty"},
	}
	got := nest(ops, []slog.Attr{slog.Int("c", 3)})
	flat := map[string]string{}
	flatten("", got, flat)
	want := map[string]string{"a": "1", "g.empty.c": "3", "g.b": "2"}
	if len(flat) != len(want) {
		t.Fatalf("flat = %v, want %v", flat, want)
	}
	for k, v := range want {
		if flat[k] != v {
			t.Errorf("%s = %q, want %q", k, flat[k], v)
		}
	}
	if got := nest([]op{{group: "g"}}, nil); len(got) != 0 {
		t.Errorf("a group with nothing in it must vanish, got %v", got)
	}
}

func TestSpanNameOK_GrammarEdges(t *testing.T) {
	ok := []string{"db.SELECT", "signal:hello.greeted", "GET /api/people/{id}", "HTTP /x", "_OTHER", strings.Repeat("a", 128)}
	bad := []string{"", "get /x", "GET  /x", "GET x", "GE /x", "GETTINGSOMEX /x", "signal hello", ".x", strings.Repeat("a", 129), "GET /x y"}
	for _, n := range ok {
		if !spanNameOK(n) {
			t.Errorf("%q should be accepted", n)
		}
	}
	for _, n := range bad {
		if spanNameOK(n) {
			t.Errorf("%q should be rejected", n)
		}
	}
}

func TestKeyDenied_SuffixAfterUnderscoreOnly(t *testing.T) {
	cases := map[string]bool{
		"email": true, "person.email": true, "Http-Request-Body": true, "user_agent": false,
		"emailer": false, "db.name": false, "session_id": false, "my_token": true,
	}
	for k, want := range cases {
		if got := keyDenied(k); got != want {
			t.Errorf("keyDenied(%q) = %v, want %v", k, got, want)
		}
	}
}

func TestParseResourceAttrs_SkipsMalformedPairs(t *testing.T) {
	got := parseResourceAttrs("deployment.environment=prod, bad ,team=a%20b,=x")
	if got["deployment.environment"] != "prod" || got["team"] != "a b" || len(got) != 2 {
		t.Errorf("got %v", got)
	}
}

func TestCollect_RunsGaugeObserversOnlyWithAnObserver(t *testing.T) {
	var buf strings.Builder
	tel, err := New(context.Background(), Options{
		Level: slog.LevelInfo, Format: FormatJSON, Instance: "a", Version: "v1", Stdout: &buf,
		Clock: clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), Environ: func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tel.Collect(context.Background()); err != nil {
		t.Errorf("Collect without a reader must be a no-op, got %v", err)
	}
	if err := tel.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type sinkObserver struct {
	mu       sync.Mutex
	measures []Measurement
	records  []LogRecord
}

func (o *sinkObserver) Record(r LogRecord) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.records = append(o.records, r)
}
func (o *sinkObserver) Span(SpanData) {}
func (o *sinkObserver) Measurement(m Measurement) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.measures = append(o.measures, m)
}
func (o *sinkObserver) Violation(Violation) {}

func TestCollect_GaugeSamplesReachObserverAndPanicsAreRecovered(t *testing.T) {
	obs := &sinkObserver{}
	var buf strings.Builder
	tel, err := New(context.Background(), Options{
		Level: slog.LevelInfo, Format: FormatJSON, Instance: "a", Version: "v1", Stdout: &buf,
		Clock: clock.NewFake(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), Observer: obs,
		Environ: func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := tel.Close(context.Background()); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	m, err := tel.Meter("test")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Gauge("test.level", func(context.Context) []Sample {
		return []Sample{{Value: 2, Attrs: []Attr{Token("state", "idle")}}}
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.Gauge("test.boom", func(context.Context) []Sample { panic("observer bug") }); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := tel.Collect(context.Background()); err != nil {
			t.Fatalf("Collect: %v", err)
		}
	}
	obs.mu.Lock()
	defer obs.mu.Unlock()
	levels := 0
	for _, me := range obs.measures {
		if me.Instrument == "test.level" && me.Value == 2 && me.Attrs["state"] == "idle" {
			levels++
		}
	}
	if levels != 2 {
		t.Errorf("gauge samples seen = %d, want 2", levels)
	}
	panics := 0
	for _, r := range obs.records {
		if r.Level == slog.LevelError && r.Attrs["instrument"] == "test.boom" {
			panics++
		}
	}
	if panics != 1 {
		t.Errorf("panic log lines = %d, want 1 (once per instrument)", panics)
	}
}

func TestLogValueCap_OnlyClientStackGetsStackCap(t *testing.T) {
	for key, want := range map[string]int{
		"client.stack":   MaxStackLogBytes,
		"client.message": MaxLogValueBytes,
		"stack":          MaxLogValueBytes,
		"":               MaxLogValueBytes,
	} {
		if got := logValueCap(key); got != want {
			t.Errorf("logValueCap(%q) = %d, want %d", key, got, want)
		}
	}
}
