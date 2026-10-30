package intake_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/core/telemetry/intake"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// Tests for Amendment T1 (D54): intake.Usage and the event wire types. Spec: telemetry.md,
// "Amendment T1", failure modes F51 to F68 and F73.

const (
	uuidA = "3f2b8c1e-5d4a-4b6f-9a7e-1c2d3e4f5a6b"
	hexA  = "3f2b8c1e5d4a4b6f9a7e1c2d3e4f5a6b"
)

var (
	testScreens = []string{"/", "/people/{id}", "/volunteers/shifts", "/volunteers/shifts/{id}"}
	testActions = []string{"shift.filter", "shift.sort", "dialog.open"}
)

type usageRig struct {
	u   *intake.Usage
	cap *telemetrytest.Capture
	clk *clock.Fake
}

func newUsageRig(t *testing.T, mut ...func(*intake.UsageOptions)) *usageRig {
	t.Helper()
	c := telemetrytest.New(t)
	clk := clock.NewFake(epoch)
	o := intake.UsageOptions{
		Logger: c.Logger(), Meter: c.Meter("usage"), Clock: clk, Enabled: true,
		Screens: testScreens, Actions: testActions,
	}
	for _, m := range mut {
		m(&o)
	}
	u, err := intake.NewUsage(o)
	if err != nil {
		t.Fatalf("intake.NewUsage: %v", err)
	}
	return &usageRig{u: u, cap: c, clk: clk}
}

func (r *usageRig) advance(t *testing.T, d time.Duration) {
	t.Helper()
	if err := r.clk.Advance(d); err != nil {
		t.Fatalf("Advance: %v", err)
	}
}

func (r *usageRig) view(screen, from string) intake.Event {
	return intake.Event{Kind: intake.EventScreenView, Screen: screen, From: from, OccurredAt: r.clk.Now().Add(-time.Minute), AppVersion: "1.4.0"}
}

func (r *usageRig) action(screen, action string) intake.Event {
	return intake.Event{Kind: intake.EventAction, Screen: screen, Action: action, OccurredAt: r.clk.Now().Add(-time.Minute)}
}

func (r *usageRig) ready(screen string, ms float64) intake.Event {
	return intake.Event{Kind: intake.EventScreenReady, Screen: screen, Value: ms, OccurredAt: r.clk.Now().Add(-time.Minute)}
}

func (r *usageRig) vital(screen, name string, v float64) intake.Event {
	return intake.Event{Kind: intake.EventVital, Screen: screen, Name: name, Value: v, OccurredAt: r.clk.Now().Add(-time.Minute)}
}

func (r *usageRig) views(n int) []intake.Event {
	out := make([]intake.Event, n)
	for i := range out {
		out[i] = r.view("/", intake.EntryToken)
	}
	return out
}

func (r *usageRig) send(from netip.Addr, ua string, evs ...intake.Event) error {
	err := r.u.Accept(context.Background(), intake.EventSubmission{From: from, UserAgent: ua, Batch: intake.EventBatch{Events: evs}})
	if err != nil {
		return fmt.Errorf("accept: %w", err)
	}
	return nil
}

func (r *usageRig) submit(from netip.Addr, evs ...intake.Event) error {
	return r.send(from, "TestBrowser/1.0", evs...)
}

func (r *usageRig) measures(instrument string) []telemetry.Measurement {
	var out []telemetry.Measurement
	for _, m := range r.cap.Measurements() {
		if m.Instrument == instrument {
			out = append(out, m)
		}
	}
	return out
}

func (r *usageRig) sum(instrument, attr, val string) float64 {
	var s float64
	for _, m := range r.measures(instrument) {
		if attr == "" || m.Attrs[attr] == val {
			s += m.Value
		}
	}
	return s
}

func addrN(n int) netip.Addr {
	return netip.AddrFrom4([4]byte{198, 51, uint8(n / 250 % 256), uint8(n%250 + 1)}) //nolint:gosec // G115: n is a small test counter, both values fit a byte
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func sameKeys(got map[string]string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			return false
		}
	}
	return true
}

// ---- F51 ----

func TestNewUsage_NilDependencyOrBadAllowlist_ReturnsInvalid(t *testing.T) {
	c := telemetrytest.New(t)
	clk := clock.NewFake(epoch)
	ok := func() intake.UsageOptions {
		return intake.UsageOptions{Logger: c.Logger(), Meter: c.Meter("usage"), Clock: clk, Enabled: true}
	}
	cases := map[string]func(*intake.UsageOptions){
		"nil logger":           func(o *intake.UsageOptions) { o.Logger = nil },
		"nil meter":            func(o *intake.UsageOptions) { o.Meter = nil },
		"nil clock":            func(o *intake.UsageOptions) { o.Clock = nil },
		"screen with space":    func(o *intake.UsageOptions) { o.Screens = []string{"/ok", "/a b"} },
		"screen with query":    func(o *intake.UsageOptions) { o.Screens = []string{"/a?x=1"} },
		"screen empty":         func(o *intake.UsageOptions) { o.Screens = []string{""} },
		"screen is an e-mail":  func(o *intake.UsageOptions) { o.Screens = []string{"zq9@marker.example"} },
		"action with space":    func(o *intake.UsageOptions) { o.Actions = []string{"shift filter"} },
		"action empty":         func(o *intake.UsageOptions) { o.Actions = []string{""} },
		"action with newline":  func(o *intake.UsageOptions) { o.Actions = []string{"a.b\n"} },
		"disabled still check": func(o *intake.UsageOptions) { o.Enabled = false; o.Screens = []string{"/a b"} },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			o := ok()
			mut(&o)
			u, err := intake.NewUsage(o)
			if !errs.Is(err, errs.Invalid) || u != nil {
				t.Errorf("u=%v err=%v", u, err)
			}
		})
	}
	t.Run("valid with empty lists and disabled", func(t *testing.T) {
		o := ok()
		o.Enabled = false
		if u, err := intake.NewUsage(o); err != nil || u == nil {
			t.Errorf("u=%v err=%v", u, err)
		}
	})
}

func TestIntakeNew_BadAllowlist_ReturnsInvalid(t *testing.T) { // F71
	c := telemetrytest.New(t)
	clk := clock.NewFake(epoch)
	cases := map[string]intake.Options{
		"screen with space": {Screens: []string{"/a b"}},
		"screen with query": {Screens: []string{"/a?x=1"}},
		"route with space":  {Routes: []string{"/api/a b"}},
		"route with query":  {Routes: []string{"/api/a?x=1"}},
		"route empty":       {Routes: []string{""}},
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			o.Logger, o.Meter, o.Clock = c.Logger(), c.Meter("intake"), clk
			in, err := intake.New(o)
			if !errs.Is(err, errs.Invalid) || in != nil {
				t.Errorf("in=%v err=%v", in, err)
			}
		})
	}
	good := intake.Options{
		Logger: c.Logger(), Meter: c.Meter("intake2"), Clock: clk,
		Screens: []string{"/people/{id}"}, Routes: []string{"/api/people/{id}", "/"},
	}
	if _, err := intake.New(good); err != nil {
		t.Errorf("valid allowlists: %v", err)
	}
}

// ---- F52, F53 ----

func TestUsageAccept_Disabled_ReturnsUnsupportedRecordsNothing(t *testing.T) {
	r := newUsageRig(t, func(o *intake.UsageOptions) { o.Enabled = false })
	err := r.submit(addr1, r.views(3)...)
	if !errs.Is(err, errs.Unsupported) {
		t.Fatalf("err = %v", err)
	}
	if got := r.sum("bms.client_events.rejected", "reason", "disabled"); got < 1 {
		t.Errorf("rejected{disabled} = %v", got)
	}
	for _, m := range r.cap.Measurements() {
		if m.Instrument != "bms.client_events.rejected" {
			t.Errorf("disabled Usage recorded %s %v", m.Instrument, m.Attrs)
		}
	}
	if len(r.cap.Records()) != 0 {
		t.Error("disabled Usage logged")
	}
	// An invalid batch is still just "off": the switch is checked before the body.
	if err := r.submit(addr1); !errs.Is(err, errs.Unsupported) {
		t.Errorf("empty batch while off: %v", err)
	}
}

func TestUsageAccept_ContextCanceled_ReturnsCanceled(t *testing.T) {
	r := newUsageRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sub := intake.EventSubmission{From: addr1, Batch: intake.EventBatch{Events: r.views(1)}}
	if err := r.u.Accept(ctx, sub); !errs.Is(err, errs.Canceled) {
		t.Errorf("canceled: %v", err)
	}
	dctx, dcancel := context.WithDeadline(context.Background(), epoch.Add(-time.Hour))
	defer dcancel()
	if err := r.u.Accept(dctx, sub); !errs.Is(err, errs.Timeout) {
		t.Errorf("deadline: %v", err)
	}
	if len(r.measures("bms.client.screen_views")) != 0 {
		t.Error("measurements recorded for a done context")
	}
}

// ---- F54 to F62: validation ----

func TestUsageAccept_EmptyOrTooManyEvents_ReturnsInvalid(t *testing.T) {
	r := newUsageRig(t)
	for _, n := range []int{0, intake.MaxEvents + 1} {
		err := r.submit(addr1, r.views(n)...)
		if !errs.Is(err, errs.Invalid) || !hasField(err, "events") {
			t.Errorf("n=%d: err=%v", n, err)
		}
	}
	if err := (intake.EventBatch{}).Validate(); !errs.Is(err, errs.Invalid) || !hasField(err, "events") {
		t.Errorf("nil Events Validate: %v", err)
	}
	if err := r.submit(addr1, r.views(intake.MaxEvents)...); err != nil {
		t.Errorf("exactly MaxEvents must pass: %v", err)
	}
	// Rejected sizes charged nothing: 10 of this address's 60 events remain.
	for range 5 {
		if err := r.submit(addr1, r.views(intake.MaxEvents+1)...); !errs.Is(err, errs.Invalid) {
			t.Fatalf("err = %v", err)
		}
	}
	if err := r.submit(addr1, r.views(10)...); err != nil {
		t.Errorf("buckets were charged by invalid sizes: %v", err)
	}
}

func TestEventBatchValidate_BadKind_ReturnsInvalid(t *testing.T) {
	r := newUsageRig(t)
	for _, k := range []intake.EventKind{"", "SCREEN_VIEW", "click", "crash"} {
		ev := r.view("/", "")
		ev.Kind = k
		err := intake.EventBatch{Events: []intake.Event{r.view("/", ""), ev}}.Validate()
		if !errs.Is(err, errs.Invalid) || !hasField(err, "events[1].kind") {
			t.Errorf("kind %q: err=%v", k, err)
		}
	}
}

// tableCase is one event and the field it must be refused under ("" means valid).
type tableCase struct {
	name  string
	build func(r *usageRig) intake.Event
	field string
}

func runEventTable(t *testing.T, cases []tableCase) {
	t.Helper()
	r := newUsageRig(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := intake.EventBatch{Events: []intake.Event{tc.build(r)}}.Validate()
			if tc.field == "" {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if !errs.Is(err, errs.Invalid) || !hasField(err, tc.field) {
				t.Fatalf("err = %v, want Invalid on %s", err, tc.field)
			}
		})
	}
}

func TestEventBatchValidate_Screen_Table(t *testing.T) {
	with := func(s string) func(*usageRig) intake.Event {
		return func(r *usageRig) intake.Event { return r.view(s, "") }
	}
	runEventTable(t, []tableCase{
		{"root", with("/"), ""},
		{"pattern", with("/volunteers/shifts/{id}"), ""},
		{"empty", with(""), "events[0].screen"},
		{"no leading slash", with("volunteers"), "events[0].screen"},
		{"space", with("/a b"), "events[0].screen"},
		{"query", with("/a?tab=x"), "events[0].screen"},
		{"e-mail", with("/zq9@marker.example"), "events[0].screen"},
		{"url", with("https://example.test/x"), "events[0].screen"},
		{"too long", with("/" + strings.Repeat("a", 300)), "events[0].screen"},
	})
}

func TestEventBatchValidate_From_Table(t *testing.T) {
	view := func(from string) func(*usageRig) intake.Event {
		return func(r *usageRig) intake.Event { return r.view("/", from) }
	}
	onKind := func(mk func(*usageRig) intake.Event) func(*usageRig) intake.Event {
		return func(r *usageRig) intake.Event { e := mk(r); e.From = "/people/{id}"; return e }
	}
	runEventTable(t, []tableCase{
		{"absent", view(""), ""},
		{"entry", view(intake.EntryToken), ""},
		{"pattern", view("/people/{id}"), ""},
		{"not a pattern", view("people"), "events[0].from"},
		{"space", view("/a b"), "events[0].from"},
		{"query", view("/a?x=1"), "events[0].from"},
		{"token without slash", view("loading"), "events[0].from"},
		{"on action", onKind(func(r *usageRig) intake.Event { return r.action("/", "shift.filter") }), "events[0].from"},
		{"on screen_ready", onKind(func(r *usageRig) intake.Event { return r.ready("/", 100) }), "events[0].from"},
		{"on vital", onKind(func(r *usageRig) intake.Event { return r.vital("/", "lcp", 100) }), "events[0].from"},
	})
}

func TestEventBatchValidate_Action_Table(t *testing.T) {
	act := func(a string) func(*usageRig) intake.Event {
		return func(r *usageRig) intake.Event { return r.action("/", a) }
	}
	onOther := func(mk func(*usageRig) intake.Event) func(*usageRig) intake.Event {
		return func(r *usageRig) intake.Event { e := mk(r); e.Action = "shift.filter"; return e }
	}
	sixtyFour := "a." + strings.Repeat("b", 62)
	runEventTable(t, []tableCase{
		{"two parts", act("shift.filter"), ""},
		{"three parts", act("shift.filter.clear"), ""},
		{"digits and underscore", act("shift_2.filter_1"), ""},
		{"exactly 64 bytes", act(sixtyFour), ""},
		{"65 bytes", act(sixtyFour + "b"), "events[0].action"},
		{"absent for action", act(""), "events[0].action"},
		{"one part", act("shift"), "events[0].action"},
		{"four parts", act("a.b.c.d"), "events[0].action"},
		{"upper case", act("Shift.filter"), "events[0].action"},
		{"leading digit", act("1shift.filter"), "events[0].action"},
		{"trailing dot", act("shift."), "events[0].action"},
		{"space", act("shift .filter"), "events[0].action"},
		{"hyphen", act("shift-x.filter"), "events[0].action"},
		{"on screen_view", onOther(func(r *usageRig) intake.Event { return r.view("/", "") }), "events[0].action"},
		{"on screen_ready", onOther(func(r *usageRig) intake.Event { return r.ready("/", 100) }), "events[0].action"},
		{"on vital", onOther(func(r *usageRig) intake.Event { return r.vital("/", "lcp", 100) }), "events[0].action"},
	})
}

func TestEventBatchValidate_Vital_Table(t *testing.T) {
	vital := func(name string, v float64) func(*usageRig) intake.Event {
		return func(r *usageRig) intake.Event { return r.vital("/", name, v) }
	}
	nameOn := func(mk func(*usageRig) intake.Event) func(*usageRig) intake.Event {
		return func(r *usageRig) intake.Event { e := mk(r); e.Name = "lcp"; return e }
	}
	runEventTable(t, []tableCase{
		{"lcp", vital("lcp", 2400), ""},
		{"inp", vital("inp", 180), ""},
		{"cls", vital("cls", 0.08), ""},
		{"cls zero is a measurement", vital("cls", 0), ""},
		{"lcp at max", vital("lcp", intake.MaxVitalMs), ""},
		{"inp at max", vital("inp", intake.MaxVitalMs), ""},
		{"cls at max", vital("cls", intake.MaxCLS), ""},
		{"fid is not allowed", vital("fid", 10), "events[0].name"},
		{"ttfb is not allowed", vital("ttfb", 10), "events[0].name"},
		{"upper case", vital("LCP", 10), "events[0].name"},
		{"name absent", vital("", 10), "events[0].name"},
		{"lcp negative", vital("lcp", -1), "events[0].value"},
		{"cls negative", vital("cls", -0.01), "events[0].value"},
		{"lcp over max", vital("lcp", intake.MaxVitalMs+1), "events[0].value"},
		{"inp over max", vital("inp", intake.MaxVitalMs+1), "events[0].value"},
		{"cls over max", vital("cls", intake.MaxCLS+0.5), "events[0].value"},
		{"name on screen_view", nameOn(func(r *usageRig) intake.Event { return r.view("/", "") }), "events[0].name"},
		{"name on action", nameOn(func(r *usageRig) intake.Event { return r.action("/", "shift.filter") }), "events[0].name"},
		{"name on screen_ready", nameOn(func(r *usageRig) intake.Event { return r.ready("/", 100) }), "events[0].name"},
	})
}

func TestEventBatchValidate_ScreenReady_Table(t *testing.T) {
	ready := func(v float64) func(*usageRig) intake.Event {
		return func(r *usageRig) intake.Event { return r.ready("/", v) }
	}
	valueOn := func(mk func(*usageRig) intake.Event) func(*usageRig) intake.Event {
		return func(r *usageRig) intake.Event { e := mk(r); e.Value = 5; return e }
	}
	runEventTable(t, []tableCase{
		{"typical", ready(840), ""},
		{"fractional", ready(0.5), ""},
		{"at max", ready(intake.MaxVitalMs), ""},
		{"zero", ready(0), "events[0].value"},
		{"negative", ready(-3), "events[0].value"},
		{"over max", ready(intake.MaxVitalMs + 1), "events[0].value"},
		{"value on screen_view", valueOn(func(r *usageRig) intake.Event { return r.view("/", "") }), "events[0].value"},
		{"value on action", valueOn(func(r *usageRig) intake.Event { return r.action("/", "shift.filter") }), "events[0].value"},
	})
}

func TestEventBatchValidate_CountAndVersion_ReturnInvalid(t *testing.T) {
	cnt := func(mk func(*usageRig) intake.Event, n int) func(*usageRig) intake.Event {
		return func(r *usageRig) intake.Event { e := mk(r); e.Count = n; return e }
	}
	ver := func(v string) func(*usageRig) intake.Event {
		return func(r *usageRig) intake.Event { e := r.view("/", ""); e.AppVersion = v; return e }
	}
	view := func(r *usageRig) intake.Event { return r.view("/", "") }
	act := func(r *usageRig) intake.Event { return r.action("/", "shift.filter") }
	ready := func(r *usageRig) intake.Event { return r.ready("/", 100) }
	vital := func(r *usageRig) intake.Event { return r.vital("/", "lcp", 100) }
	runEventTable(t, []tableCase{
		{"count zero means one", cnt(view, 0), ""},
		{"view count at max", cnt(view, intake.MaxCount), ""},
		{"action count 7", cnt(act, 7), ""},
		{"view count over max", cnt(view, intake.MaxCount+1), "events[0].count"},
		{"action count negative", cnt(act, -1), "events[0].count"},
		{"ready count 1 is fine", cnt(ready, 1), ""},
		{"ready count 2", cnt(ready, 2), "events[0].count"},
		{"vital count 2", cnt(vital, 2), "events[0].count"},
		{"version token", ver("1.4.0-rc.1"), ""},
		{"version absent", ver(""), ""},
		{"version 64 bytes", ver(strings.Repeat("v", 64)), ""},
		{"version 65 bytes", ver(strings.Repeat("v", 65)), "events[0].app_version"},
		{"version with space", ver("1.4 0"), "events[0].app_version"},
		{"version is an e-mail", ver(markerEmail), "events[0].app_version"},
		{"version has query", ver("1.4?x=1"), "events[0].app_version"},
	})
}

func TestEventBatchValidate_ReportsEveryIndex(t *testing.T) {
	r := newUsageRig(t)
	bad1, bad2 := r.view("nope", ""), r.action("/", "")
	err := intake.EventBatch{Events: []intake.Event{r.view("/", ""), bad1, r.view("/", ""), bad2}}.Validate()
	if !hasField(err, "events[1].screen") || !hasField(err, "events[3].action") {
		t.Errorf("err = %v", err)
	}
	if hasField(err, "events[0]") || hasField(err, "events[2]") {
		t.Errorf("valid events blamed: %v", err)
	}
}

// F61: the window needs the clock, so it is Accept's.
func TestUsageAccept_OccurredAtOutOfWindow_ReturnsInvalid(t *testing.T) {
	r := newUsageRig(t)
	now := r.clk.Now()
	cases := []struct {
		name string
		at   time.Time
		ok   bool
	}{
		{"zero", time.Time{}, false},
		{"just inside the past", now.Add(-intake.OccurredWithin + time.Second), true},
		{"just inside the future", now.Add(intake.OccurredWithin - time.Second), true},
		{"just outside the past", now.Add(-intake.OccurredWithin - time.Second), false},
		{"just outside the future", now.Add(intake.OccurredWithin + time.Second), false},
		{"year 1970", time.Unix(0, 0).UTC(), false},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev := r.view("/", "")
			ev.OccurredAt = tc.at
			err := r.submit(addrN(i), ev)
			if tc.ok {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if !errs.Is(err, errs.Invalid) || !hasField(err, "events[0].occurred_at") {
				t.Fatalf("err = %v", err)
			}
		})
	}
	// Nothing from a refused batch was recorded.
	r2 := newUsageRig(t)
	bad := r2.view("/", "")
	bad.OccurredAt = time.Time{}
	if err := r2.submit(addr1, r2.view("/", ""), bad); !errs.Is(err, errs.Invalid) {
		t.Fatalf("err = %v", err)
	}
	if len(r2.measures("bms.client.screen_views")) != 0 {
		t.Error("part of a refused batch was recorded")
	}
}

// ---- F63, F64: limits ----

func TestUsageAccept_PerAddressLimit_ReturnsRateLimitedWithoutLoggingAddress(t *testing.T) {
	r := newUsageRig(t)
	if err := r.submit(addr1, r.views(50)...); err != nil {
		t.Fatalf("within limit: %v", err)
	}
	if err := r.submit(addr1, r.views(10)...); err != nil {
		t.Fatalf("exactly the limit: %v", err)
	}
	before := len(r.measures("bms.client.screen_views"))
	for range 3 {
		err := r.submit(addr1, r.views(1)...)
		if !errors.Is(err, intake.ErrRateLimited) || !errs.Is(err, errs.Unavailable) {
			t.Fatalf("err = %v", err)
		}
	}
	if got := r.sum("bms.client_events.rejected", "reason", "rate_limited"); got < 3 {
		t.Errorf("rejected{rate_limited} = %v", got)
	}
	if len(r.measures("bms.client.screen_views")) != before {
		t.Error("a rejected batch was recorded")
	}
	if r.cap.Contains(addr1.String()) {
		t.Error("the address was logged or put on a metric")
	}
	if n := len(r.cap.Records()); n != 1 {
		t.Errorf("records after three rejections = %d, want exactly the one hourly line", n)
	}
	// Another address is unaffected; a whole batch is refused when it does not fit.
	if err := r.submit(addrN(2), r.views(50)...); err != nil {
		t.Fatalf("other address: %v", err)
	}
	if err := r.submit(addrN(2), r.views(11)...); !errors.Is(err, intake.ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	// One hour later the line may be written again, once.
	r.advance(t, time.Hour+time.Minute)
	if err := r.submit(addr1, r.views(50)...); err != nil {
		t.Fatalf("after refill: %v", err)
	}
	if err := r.submit(addr1, r.views(11)...); !errors.Is(err, intake.ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	if n := len(r.cap.Records()); n != 2 {
		t.Errorf("records after an hour = %d, want 2", n)
	}
	if r.cap.Contains(addr1.String()) {
		t.Error("the address was logged")
	}
}

func TestUsageAccept_GlobalLimit_ReturnsRateLimited(t *testing.T) {
	r := newUsageRig(t)
	n := intake.EventsGlobalPerMinute / intake.MaxEvents
	for i := range n {
		if err := r.submit(addrN(i), r.views(intake.MaxEvents)...); err != nil {
			t.Fatalf("batch %d: %v", i, err)
		}
	}
	err := r.submit(addrN(n), r.views(1)...)
	if !errors.Is(err, intake.ErrRateLimited) || !errs.Is(err, errs.Unavailable) {
		t.Fatalf("err = %v", err)
	}
	if got := r.sum("bms.client_events.rejected", "reason", "rate_limited"); got < 1 {
		t.Errorf("rejected{rate_limited} = %v", got)
	}
	// The refusal did not spend the address's own tokens; after a refill it is served.
	r.advance(t, time.Minute)
	if err := r.submit(addrN(n), r.views(intake.MaxEvents)...); err != nil {
		t.Errorf("after refill: %v", err)
	}
}

func TestUsageAccept_SharesBucketImplementationWithIntake(t *testing.T) {
	c := telemetrytest.New(t)
	clk := clock.NewFake(epoch)
	in, err := intake.New(intake.Options{Logger: c.Logger(), Meter: c.Meter("intake"), Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	u, err := intake.NewUsage(intake.UsageOptions{Logger: c.Logger(), Meter: c.Meter("usage"), Clock: clk, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	sendEvents := func(n int) error {
		evs := make([]intake.Event, n)
		for i := range evs {
			evs[i] = intake.Event{Kind: intake.EventScreenView, Screen: "/", OccurredAt: clk.Now().Add(-time.Minute)}
		}
		return u.Accept(context.Background(), intake.EventSubmission{From: addr1, Batch: intake.EventBatch{Events: evs}}) //nolint:wrapcheck // test helper
	}
	sendReports := func(n int) error {
		rs := make([]intake.Report, n)
		for i := range rs {
			rs[i] = intake.Report{Kind: intake.KindCrash, Message: "boom", OccurredAt: clk.Now().Add(-time.Minute)}
		}
		return in.Accept(context.Background(), intake.Submission{From: addr1, Batch: intake.Batch{Reports: rs}}) //nolint:wrapcheck // test helper
	}
	if err := sendEvents(50); err != nil {
		t.Fatal(err)
	}
	if err := sendEvents(10); err != nil {
		t.Fatal(err)
	}
	if err := sendEvents(1); !errors.Is(err, intake.ErrRateLimited) {
		t.Fatalf("events should be limited: %v", err)
	}
	if err := sendReports(20); err != nil {
		t.Errorf("an address limited for events must still report errors: %v", err)
	}
	// And the other way round.
	if err := sendReports(10); err != nil {
		t.Fatal(err)
	}
	if err := sendReports(1); !errors.Is(err, intake.ErrRateLimited) {
		t.Fatalf("reports should be limited: %v", err)
	}
	if err := sendEvents(1); !errors.Is(err, intake.ErrRateLimited) {
		t.Fatalf("events are still limited: %v", err)
	}
}

// ---- F65 to F68: folding ----

func TestUsageAccept_UnknownScreenOrAction_FoldedToOther(t *testing.T) {
	r := newUsageRig(t)
	evs := []intake.Event{
		r.view("/volunteers/shifts", "/"),            // all known
		r.view("/ZQ9MARKER7/screen", "/"),            // unknown screen
		r.view("/", "/ZQ9MARKER7/from"),              // unknown from
		r.view("/", intake.EntryToken),               // _entry is never folded
		r.action("/", "shift.filter"),                // known
		r.action("/", "zq9marker7.action"),           // unknown action
		r.action("/ZQ9MARKER7/screen", "shift.sort"), // unknown screen on an action
	}
	if err := r.submit(addr1, evs...); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if got := r.sum("bms.client_events.folded", "field", "screen"); got != 2 {
		t.Errorf("folded{screen} = %v, want 2", got)
	}
	if got := r.sum("bms.client_events.folded", "field", "from"); got != 1 {
		t.Errorf("folded{from} = %v, want 1", got)
	}
	if got := r.sum("bms.client_events.folded", "field", "action"); got != 1 {
		t.Errorf("folded{action} = %v, want 1", got)
	}
	views := r.measures("bms.client.screen_views")
	if len(views) != 4 {
		t.Fatalf("screen_views measurements = %d", len(views))
	}
	if views[1].Attrs["screen"] != intake.OtherToken || views[1].Attrs["from"] != "/" {
		t.Errorf("unknown screen: %v", views[1].Attrs)
	}
	if views[2].Attrs["from"] != intake.OtherToken || views[2].Attrs["screen"] != "/" {
		t.Errorf("unknown from: %v", views[2].Attrs)
	}
	if views[3].Attrs["from"] != intake.EntryToken {
		t.Errorf("_entry: %v", views[3].Attrs)
	}
	acts := r.measures("bms.client.actions")
	if len(acts) != 3 {
		t.Fatalf("actions measurements = %d", len(acts))
	}
	if acts[0].Attrs["action"] != "shift.filter" || acts[1].Attrs["action"] != intake.OtherToken ||
		acts[2].Attrs["screen"] != intake.OtherToken || acts[2].Attrs["action"] != "shift.sort" {
		t.Errorf("actions = %v %v %v", acts[0].Attrs, acts[1].Attrs, acts[2].Attrs)
	}
	if r.cap.Contains(marker) || r.cap.Contains("zq9marker7") {
		t.Error("an unknown value reached a metric attribute or a log")
	}
}

func TestUsageAccept_EmptyAllowlistCap_FoldsBeyondCap(t *testing.T) {
	r := newUsageRig(t, func(o *intake.UsageOptions) { o.Screens, o.Actions = nil, nil })
	n := 0
	fill := func(count int, mk func(i int) intake.Event) {
		for sent := 0; sent < count; {
			size := min(intake.MaxEvents, count-sent)
			evs := make([]intake.Event, size)
			for j := range evs {
				evs[j] = mk(sent + j)
			}
			if err := r.submit(addrN(n), evs...); err != nil {
				t.Fatalf("fill: %v", err)
			}
			n++
			sent += size
		}
	}
	fill(intake.MaxDistinctScreens, func(i int) intake.Event { return r.view(fmt.Sprintf("/s%d", i), "") })
	if got := r.sum("bms.client_events.folded", "field", "screen"); got != 0 {
		t.Fatalf("folded within the cap = %v", got)
	}
	fill(intake.MaxDistinctActions, func(i int) intake.Event { return r.action("/s0", fmt.Sprintf("a.n%d", i)) })
	if got := r.sum("bms.client_events.folded", "field", "action"); got != 0 {
		t.Fatalf("folded actions within the cap = %v", got)
	}
	// One more of each, then an admitted one again.
	if err := r.submit(addrN(n), r.view("/beyond", ""), r.view("/s0", ""), r.action("/s0", "a.beyond"), r.action("/s0", "a.n0")); err != nil {
		t.Fatal(err)
	}
	if got := r.sum("bms.client_events.folded", "field", "screen"); got != 1 {
		t.Errorf("folded{screen} beyond cap = %v, want 1", got)
	}
	if got := r.sum("bms.client_events.folded", "field", "action"); got != 1 {
		t.Errorf("folded{action} beyond cap = %v, want 1", got)
	}
	views := r.measures("bms.client.screen_views")
	if last := views[len(views)-2]; last.Attrs["screen"] != intake.OtherToken {
		t.Errorf("beyond-cap screen = %v", last.Attrs)
	}
	if last := views[len(views)-1]; last.Attrs["screen"] != "/s0" {
		t.Errorf("an already admitted screen was folded: %v", last.Attrs)
	}
	acts := r.measures("bms.client.actions")
	if last := acts[len(acts)-2]; last.Attrs["action"] != intake.OtherToken {
		t.Errorf("beyond-cap action = %v", last.Attrs)
	}
	if last := acts[len(acts)-1]; last.Attrs["action"] != "a.n0" {
		t.Errorf("an already admitted action was folded: %v", last.Attrs)
	}
}

func TestUsageAccept_IDSegments_ReplacedBeforeFolding(t *testing.T) {
	r := newUsageRig(t)
	evs := []intake.Event{
		r.view("/people/"+uuidA, "/volunteers/shifts/"+hexA),
		r.action("/volunteers/shifts/"+uuidA, "shift.filter"),
	}
	if err := r.submit(addr1, evs...); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	v := r.measures("bms.client.screen_views")
	if len(v) != 1 || v[0].Attrs["screen"] != "/people/{id}" || v[0].Attrs["from"] != "/volunteers/shifts/{id}" {
		t.Errorf("view = %+v", v)
	}
	a := r.measures("bms.client.actions")
	if len(a) != 1 || a[0].Attrs["screen"] != "/volunteers/shifts/{id}" {
		t.Errorf("action = %+v", a)
	}
	if got := r.sum("bms.client_events.folded", "", ""); got != 0 {
		t.Errorf("an ID-shaped segment must not count as folded: %v", got)
	}
	if r.cap.Contains(uuidA) || r.cap.Contains(hexA) {
		t.Error("an ID reached a metric or a log")
	}
	// With no allowlist the ID-replaced form is what is admitted, so the cap cannot be filled with IDs.
	r2 := newUsageRig(t, func(o *intake.UsageOptions) { o.Screens, o.Actions = nil, nil })
	if err := r2.submit(addr1, r2.view("/people/"+uuidA, ""), r2.view("/people/"+hexA, "")); err != nil {
		t.Fatal(err)
	}
	for _, m := range r2.measures("bms.client.screen_views") {
		if m.Attrs["screen"] != "/people/{id}" {
			t.Errorf("screen = %q", m.Attrs["screen"])
		}
	}
	if r2.cap.Contains(uuidA) || r2.cap.Contains(hexA) {
		t.Error("an ID reached a metric or a log")
	}
}

func TestUsageAccept_OtherScreen_OtherModule(t *testing.T) {
	r := newUsageRig(t)
	if err := r.submit(addr1, r.view("/nowhere", "/"), r.action("/nowhere", "shift.filter"), r.ready("/nowhere", 50), r.vital("/nowhere", "inp", 90), r.view("/", "")); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	for _, in := range []string{"bms.client.screen_views", "bms.client.actions", "bms.client.screen_ready", "bms.client.inp"} {
		ms := r.measures(in)
		if len(ms) == 0 {
			t.Fatalf("%s: no measurement", in)
		}
		m := ms[0]
		if m.Attrs["screen"] != intake.OtherToken || m.Attrs["module"] != intake.OtherToken {
			t.Errorf("%s: screen/module = %q/%q, want _other/_other", in, m.Attrs["screen"], m.Attrs["module"])
		}
	}
	// A known screen gets a real module, never _other and never empty.
	known := r.measures("bms.client.screen_views")
	last := known[len(known)-1]
	if m := last.Attrs["module"]; m == "" || m == intake.OtherToken {
		t.Errorf("known screen module = %q", m)
	}
}

// ---- behaviour that the spec's test plan names ----

func TestUsageAccept_HappyPath_RecordsInstrumentsOnly(t *testing.T) {
	r := newUsageRig(t)
	const ua = "Mozilla/5.0 (Linux; Android 14; ZQ9MARKER7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36"
	evs := []intake.Event{
		r.view("/volunteers/shifts/"+uuidA, "/volunteers/shifts"),
		r.action("/volunteers/shifts", "shift.filter"),
		r.ready("/volunteers/shifts", 1500),
		r.vital("/volunteers/shifts", "lcp", 2400),
		r.vital("/volunteers/shifts", "inp", 200),
		r.vital("/volunteers/shifts", "cls", 0.1),
	}
	if err := r.send(addr1, ua, evs...); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if recs := r.cap.Records(); len(recs) != 0 {
		t.Errorf("Usage wrote %d log records, want none", len(recs))
	}
	if spans := r.cap.Spans(); len(spans) != 0 {
		t.Errorf("Usage wrote %d spans, want none", len(spans))
	}
	for _, needle := range []string{addr1.String(), marker, markerEmail, uuidA, "Mobile", "Android", "Chrome/126"} {
		if r.cap.Contains(needle) {
			t.Errorf("%q reached telemetry", needle)
		}
	}
	sv := r.measures("bms.client.screen_views")
	if len(sv) != 1 || sv[0].Value != 1 || !sameKeys(sv[0].Attrs, "screen", "module", "from") {
		t.Fatalf("screen_views = %+v", sv)
	}
	if sv[0].Attrs["screen"] != "/volunteers/shifts/{id}" || sv[0].Attrs["from"] != "/volunteers/shifts" {
		t.Errorf("screen_views attrs = %v", sv[0].Attrs)
	}
	ac := r.measures("bms.client.actions")
	if len(ac) != 1 || ac[0].Value != 1 || !sameKeys(ac[0].Attrs, "action", "screen", "module") || ac[0].Attrs["action"] != "shift.filter" {
		t.Fatalf("actions = %+v", ac)
	}
	for _, c := range []struct {
		name  string
		value float64
	}{
		{"bms.client.screen_ready", 1.5}, {"bms.client.lcp", 2.4}, {"bms.client.inp", 0.2}, {"bms.client.cls", 0.1},
	} {
		ms := r.measures(c.name)
		if len(ms) != 1 {
			t.Fatalf("%s: %d measurements", c.name, len(ms))
		}
		m := ms[0]
		if !sameKeys(m.Attrs, "screen", "module", "browser", "form_factor") {
			t.Errorf("%s attrs = %v", c.name, keysOf(m.Attrs))
		}
		if d := m.Value - c.value; d > 1e-9 || d < -1e-9 {
			t.Errorf("%s value = %v, want %v (seconds for ms inputs; cls unitless)", c.name, m.Value, c.value)
		}
		if m.Attrs["browser"] != "chromium" || m.Attrs["form_factor"] != "phone" {
			t.Errorf("%s browser/form_factor = %q/%q", c.name, m.Attrs["browser"], m.Attrs["form_factor"])
		}
	}
	if sv[0].Attrs["module"] != ac[0].Attrs["module"] || sv[0].Attrs["module"] != r.measures("bms.client.lcp")[0].Attrs["module"] {
		t.Error("module differs between events of one screen family")
	}
	for kind, want := range map[string]float64{"screen_view": 1, "action": 1, "screen_ready": 1, "vital": 3} {
		if got := r.sum("bms.client_events.accepted", "kind", kind); got != want {
			t.Errorf("accepted{%s} = %v, want %v", kind, got, want)
		}
	}
	for _, m := range r.measures("bms.client_events.accepted") {
		if !sameKeys(m.Attrs, "kind") {
			t.Errorf("accepted attrs = %v", m.Attrs)
		}
	}
	for _, m := range r.cap.Measurements() {
		for k := range m.Attrs {
			switch k {
			case "person_id", "visit_id", "session", "user_agent", "address", "ip", "trace_id", "app_version", "url", "query":
				t.Errorf("%s carries the forbidden attribute %q", m.Instrument, k)
			}
		}
	}
}

func TestUsageAccept_CountMergedEvents_AddsCount(t *testing.T) {
	r := newUsageRig(t)
	a := r.view("/", "")
	a.Count = 7
	b := r.action("/", "shift.sort")
	b.Count = 3
	c := r.view("/", "")
	if err := r.submit(addr1, a, b, c); err != nil {
		t.Fatal(err)
	}
	if got := r.sum("bms.client.screen_views", "", ""); got != 8 {
		t.Errorf("screen_views = %v, want 7 + 1", got)
	}
	if got := r.sum("bms.client.actions", "", ""); got != 3 {
		t.Errorf("actions = %v, want 3", got)
	}
}

func TestUsageAccept_TransitionsCounted(t *testing.T) {
	r := newUsageRig(t)
	evs := []intake.Event{
		r.view("/", intake.EntryToken),
		r.view("/volunteers/shifts", "/"),
		r.view("/volunteers/shifts/"+uuidA, "/volunteers/shifts"),
		r.view("/volunteers/shifts", "/volunteers/shifts/"+uuidA),
	}
	if err := r.submit(addr1, evs...); err != nil {
		t.Fatal(err)
	}
	want := [][2]string{
		{"/", intake.EntryToken}, {"/volunteers/shifts", "/"},
		{"/volunteers/shifts/{id}", "/volunteers/shifts"}, {"/volunteers/shifts", "/volunteers/shifts/{id}"},
	}
	got := r.measures("bms.client.screen_views")
	if len(got) != len(want) {
		t.Fatalf("measurements = %d", len(got))
	}
	for i, w := range want {
		if got[i].Attrs["screen"] != w[0] || got[i].Attrs["from"] != w[1] || got[i].Value != 1 {
			t.Errorf("transition %d = %v, want screen=%s from=%s", i, got[i].Attrs, w[0], w[1])
		}
	}
}

func TestUserAgentClass_Table(t *testing.T) { // F73
	const (
		chromePhone  = "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36"
		chromeDesk   = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
		edgeDesk     = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0"
		firefoxDesk  = "Mozilla/5.0 (X11; Linux x86_64; rv:127.0) Gecko/20100101 Firefox/127.0"
		firefoxPhone = "Mozilla/5.0 (Android 14; Mobile; rv:127.0) Gecko/127.0 Firefox/127.0"
		safariPhone  = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"
		safariDesk   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Safari/605.1.15"
	)
	cases := []struct {
		name, ua, browser, form string
	}{
		{"chromium phone", chromePhone, "chromium", "phone"},
		{"chromium desktop", chromeDesk, "chromium", "desktop"},
		{"edge is chromium", edgeDesk, "chromium", "desktop"},
		{"firefox desktop", firefoxDesk, "firefox", "desktop"},
		{"firefox phone", firefoxPhone, "firefox", "phone"},
		{"webkit phone", safariPhone, "webkit", "phone"},
		{"webkit desktop", safariDesk, "webkit", "desktop"},
		{"empty", "", "other", "desktop"},
		{"curl", "curl/8.0.1", "other", "desktop"},
		{"10 KB of junk", strings.Repeat("ZQ9MARKER7 ", 1000), "other", "desktop"},
		{"junk with a marker", "ZQ9MARKER7/1.0 (" + markerEmail + ")", "other", "desktop"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newUsageRig(t)
			if err := r.send(addrN(i), tc.ua, r.ready("/", 100)); err != nil {
				t.Fatalf("Accept: %v", err)
			}
			ms := r.measures("bms.client.screen_ready")
			if len(ms) != 1 {
				t.Fatalf("measurements = %d", len(ms))
			}
			if ms[0].Attrs["browser"] != tc.browser || ms[0].Attrs["form_factor"] != tc.form {
				t.Errorf("browser/form_factor = %q/%q, want %s/%s", ms[0].Attrs["browser"], ms[0].Attrs["form_factor"], tc.browser, tc.form)
			}
			if r.cap.Contains(marker) || r.cap.Contains(markerEmail) || r.cap.Contains("Mozilla") || r.cap.Contains("curl") {
				t.Error("User-Agent text was recorded")
			}
			if len(r.cap.Records()) != 0 {
				t.Error("log records written")
			}
		})
	}
}

func TestEventBatch_JSONIgnoresUnknownFields(t *testing.T) {
	body := `{"events":[
		{"kind":"screen_view","screen":"/volunteers/shifts/{id}","from":"_entry","occurred_at":"2026-10-17T11:59:00Z","count":3,"app_version":"1.0.0","future":{"x":1}},
		{"kind":"action","screen":"/","action":"shift.filter","occurred_at":"2026-10-17T11:59:00Z"},
		{"kind":"vital","screen":"/","name":"lcp","value":2400.5,"occurred_at":"2026-10-17T11:59:00Z","person_id":"must-be-ignored"}
	],"extra":1}`
	var b intake.EventBatch
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(b.Events) != 3 {
		t.Fatalf("events = %d", len(b.Events))
	}
	e := b.Events[0]
	if e.Kind != intake.EventScreenView || e.Screen != "/volunteers/shifts/{id}" || e.From != intake.EntryToken ||
		e.Count != 3 || e.AppVersion != "1.0.0" || e.OccurredAt.Hour() != 11 {
		t.Errorf("event 0 = %+v", e)
	}
	if b.Events[1].Action != "shift.filter" || b.Events[2].Name != "lcp" || b.Events[2].Value != 2400.5 {
		t.Errorf("events = %+v", b.Events[1:])
	}
	if err := b.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
	out, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "person") || strings.Contains(string(out), "must-be-ignored") {
		t.Errorf("the wire form has a person field: %s", out)
	}
}

func TestUsageAccept_SafeForConcurrentUse(t *testing.T) {
	r := newUsageRig(t)
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 5 {
				if err := r.submit(addrN(i), r.view("/", ""), r.action("/", "shift.sort"), r.ready("/", 10)); err != nil {
					t.Errorf("goroutine %d: %v", i, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	if got := r.sum("bms.client.screen_views", "", ""); got != 80 {
		t.Errorf("screen_views = %v, want 80", got)
	}
}
