package intake_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

const (
	marker      = "ZQ9MARKER7"
	markerEmail = "zq9@marker.example"
)

var (
	epoch = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	addr1 = netip.MustParseAddr("203.0.113.7")
)

type rig struct {
	in  *intake.Intake
	cap *telemetrytest.Capture
	clk *clock.Fake
}

func newRig(t *testing.T) *rig {
	t.Helper()
	c := telemetrytest.New(t)
	clk := clock.NewFake(epoch)
	in, err := intake.New(intake.Options{Logger: c.Logger(), Meter: c.Meter("intake"), Clock: clk})
	if err != nil {
		t.Fatalf("intake.New: %v", err)
	}
	return &rig{in: in, cap: c, clk: clk}
}

func (r *rig) advance(t *testing.T, d time.Duration) {
	t.Helper()
	if err := r.clk.Advance(d); err != nil {
		t.Fatalf("Advance: %v", err)
	}
}

func (r *rig) report() intake.Report {
	return intake.Report{
		Kind:       intake.KindCrash,
		Message:    "TypeError: cannot read properties of undefined",
		Screen:     "/people/{id}",
		OccurredAt: r.clk.Now().Add(-time.Minute),
		AppVersion: "1.4.0",
	}
}

func (r *rig) reports(n int) []intake.Report {
	out := make([]intake.Report, n)
	for i := range out {
		out[i] = r.report()
	}
	return out
}

func (r *rig) submit(from netip.Addr, reports ...intake.Report) error {
	err := r.in.Accept(context.Background(), intake.Submission{
		From: from, UserAgent: "TestBrowser/1.0", Batch: intake.Batch{Reports: reports},
	})
	if err != nil {
		return fmt.Errorf("accept: %w", err)
	}
	return nil
}

func (r *rig) clientRecords() []telemetry.LogRecord {
	var out []telemetry.LogRecord
	for _, rec := range r.cap.Records() {
		if rec.Message == "client error" {
			out = append(out, rec)
		}
	}
	return out
}

func (r *rig) measurementSum(instrument, attr, val string) float64 {
	var sum float64
	for _, m := range r.cap.Measurements() {
		if m.Instrument == instrument && (attr == "" || m.Attrs[attr] == val) {
			sum += m.Value
		}
	}
	return sum
}

func hasField(err error, field string) bool {
	for _, f := range validate.Fields(err) {
		if f.Field == field {
			return true
		}
	}
	return false
}

func TestIntakeNew_NilDependency_ReturnsInvalid(t *testing.T) {
	c := telemetrytest.New(t)
	clk := clock.NewFake(epoch)
	cases := map[string]intake.Options{
		"nil logger": {Meter: c.Meter("intake"), Clock: clk},
		"nil meter":  {Logger: c.Logger(), Clock: clk},
		"nil clock":  {Logger: c.Logger(), Meter: c.Meter("intake")},
		"all nil":    {},
	}
	for name, o := range cases {
		t.Run(name, func(t *testing.T) {
			in, err := intake.New(o)
			if !errs.Is(err, errs.Invalid) || in != nil {
				t.Errorf("in=%v err=%v", in, err)
			}
		})
	}
}

func TestAccept_HappyPath_OneRecordPerReport(t *testing.T) {
	r := newRig(t)
	person := model.NewID[model.PersonID]()
	api := r.report()
	api.Kind = intake.KindAPI
	api.Message = "request failed"
	api.Stack = "at fetch (app.js:1:1)"
	api.Count = 3
	api.ErrorRef = "1A2B-3C4D"
	api.TraceID = "a1b2c3d4e5f60718293a4b5c6d7e8f90"
	api.API = &intake.APICall{Method: "POST", Route: "/api/people/{id}", Status: 503, Code: errs.Unavailable.String()}
	err := r.in.Accept(context.Background(), intake.Submission{
		From: addr1, Person: person, UserAgent: "TestBrowser/1.0",
		Batch: intake.Batch{Reports: []intake.Report{r.report(), api}},
	})
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	recs := r.clientRecords()
	if len(recs) != 2 {
		t.Fatalf("want one record per report, got %d", len(recs))
	}
	for _, rec := range recs {
		if rec.Level != slog.LevelWarn {
			t.Errorf("level = %v", rec.Level)
		}
		want := map[string]string{
			"origin": "client", "user_agent": "TestBrowser/1.0", "client.screen": "/people/{id}",
			"client.app_version": "1.4.0", "person_id": person.String(),
		}
		for k, v := range want {
			if rec.Attrs[k] != v {
				t.Errorf("%s = %q, want %q", k, rec.Attrs[k], v)
			}
		}
		if _, err := time.Parse(time.RFC3339, rec.Attrs["client.occurred_at"]); err != nil {
			t.Errorf("client.occurred_at = %q", rec.Attrs["client.occurred_at"])
		}
	}
	first, second := recs[0], recs[1]
	if first.Attrs["client.kind"] != "crash" || first.Attrs["client.count"] != "1" {
		t.Errorf("first = %v", first.Attrs)
	}
	if _, ok := first.Attrs["client.stack"]; ok {
		t.Error("empty stack must be omitted")
	}
	want := map[string]string{
		"client.kind": "api", "client.count": "3", "client.stack": api.Stack,
		"client.error_ref": "1A2B-3C4D", "client.trace_id": api.TraceID,
		"client.api.method": "POST", "client.api.route": "/api/people/{id}",
		"client.api.status": "503", "client.api.code": "unavailable",
	}
	for k, v := range want {
		if second.Attrs[k] != v {
			t.Errorf("%s = %q, want %q", k, second.Attrs[k], v)
		}
	}
	if r.cap.Contains(addr1.String()) {
		t.Error("the client address was logged")
	}
	for _, rec := range recs {
		if v, ok := rec.Attrs["source"]; ok {
			t.Errorf("source must not be set on client records, got %q", v)
		}
	}
}

func TestAccept_AnonymousSubmission(t *testing.T) {
	r := newRig(t)
	if err := r.submit(addr1, r.report()); err != nil {
		t.Fatal(err)
	}
	recs := r.clientRecords()
	if len(recs) != 1 {
		t.Fatalf("records = %d", len(recs))
	}
	if _, ok := recs[0].Attrs["person_id"]; ok {
		t.Errorf("person_id on an anonymous report: %v", recs[0].Attrs)
	}
}

func TestAccept_CountsAcceptedByCount(t *testing.T) {
	r := newRig(t)
	a, b := r.report(), r.report()
	a.Count = 5
	b.Kind = intake.KindScreen
	if err := r.submit(addr1, a, b); err != nil {
		t.Fatal(err)
	}
	if got := r.measurementSum("bms.client_errors.accepted", "kind", "crash"); got != 5 {
		t.Errorf("crash accepted = %v", got)
	}
	if got := r.measurementSum("bms.client_errors.accepted", "kind", "screen"); got != 1 {
		t.Errorf("screen accepted = %v (Count 0 means 1)", got)
	}
}

func TestAccept_InvalidAddress_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	err := r.submit(netip.Addr{}, r.report())
	if !errs.Is(err, errs.Invalid) {
		t.Errorf("err = %v", err)
	}
	if len(r.clientRecords()) != 0 {
		t.Error("a rejected submission logged a report")
	}
}

func TestAccept_EmptyOrTooManyReports_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	for _, n := range []int{0, intake.MaxReports + 1} {
		err := r.submit(addr1, r.reports(n)...)
		if !errs.Is(err, errs.Invalid) || !hasField(err, "reports") {
			t.Errorf("n=%d: err=%v", n, err)
		}
	}
	if err := r.submit(addr1, r.reports(intake.MaxReports)...); err != nil {
		t.Errorf("exactly MaxReports must pass: %v", err)
	}
	// Rejected sizes charged no tokens: the remaining 10 of this address's 30 are available.
	for range 5 {
		if err := r.submit(addr1, r.reports(intake.MaxReports+1)...); !errs.Is(err, errs.Invalid) {
			t.Fatalf("err = %v", err)
		}
	}
	if err := r.submit(addr1, r.reports(10)...); err != nil {
		t.Errorf("buckets were charged by invalid sizes: %v", err)
	}
}

func TestBatchValidate_BadKind_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	for _, k := range []intake.Kind{"", "CRASH", "warning"} {
		rep := r.report()
		rep.Kind = k
		b := intake.Batch{Reports: []intake.Report{r.report(), rep}}
		err := b.Validate()
		if !errs.Is(err, errs.Invalid) || !hasField(err, "reports[1].kind") {
			t.Errorf("kind %q: err=%v", k, err)
		}
	}
	for _, k := range []intake.Kind{intake.KindCrash, intake.KindRejection, intake.KindScreen} {
		rep := r.report()
		rep.Kind = k
		if err := rep.Validate(); err != nil {
			t.Errorf("kind %q: %v", k, err)
		}
	}
}

func TestBatchValidate_APICall_Table(t *testing.T) {
	r := newRig(t)
	ok := func() *intake.APICall {
		return &intake.APICall{Method: "GET", Route: "/api/x/{id}", Status: 500, Code: "internal"}
	}
	cases := []struct {
		name  string
		kind  intake.Kind
		api   *intake.APICall
		field string // "" means valid
	}{
		{"valid", intake.KindAPI, ok(), ""},
		{"network failure", intake.KindAPI, &intake.APICall{Method: "GET", Route: "/x", Status: 0}, ""},
		{"empty code", intake.KindAPI, &intake.APICall{Method: "PUT", Route: "/x", Status: 404, Code: ""}, ""},
		{"missing for api", intake.KindAPI, nil, "reports[0].api"},
		{"present for crash", intake.KindCrash, ok(), "reports[0].api"},
		{"bad method", intake.KindAPI, &intake.APICall{Method: "TRACE", Route: "/x", Status: 500}, "reports[0].api.method"},
		{"lower method", intake.KindAPI, &intake.APICall{Method: "get", Route: "/x", Status: 500}, "reports[0].api.method"},
		{"route space", intake.KindAPI, &intake.APICall{Method: "GET", Route: "/a b", Status: 500}, "reports[0].api.route"},
		{"route query", intake.KindAPI, &intake.APICall{Method: "GET", Route: "/a?x=1", Status: 500}, "reports[0].api.route"},
		{"route empty", intake.KindAPI, &intake.APICall{Method: "GET", Route: "", Status: 500}, "reports[0].api.route"},
		{"status 99", intake.KindAPI, &intake.APICall{Method: "GET", Route: "/x", Status: 99}, "reports[0].api.status"},
		{"status 600", intake.KindAPI, &intake.APICall{Method: "GET", Route: "/x", Status: 600}, "reports[0].api.status"},
		{"status negative", intake.KindAPI, &intake.APICall{Method: "GET", Route: "/x", Status: -1}, "reports[0].api.status"},
		{"code network", intake.KindAPI, &intake.APICall{Method: "GET", Route: "/x", Status: 0, Code: "network"}, ""},
		{"code malformed_response", intake.KindAPI, &intake.APICall{Method: "GET", Route: "/x", Status: 200, Code: "malformed_response"}, ""},
		{"code cross_origin", intake.KindAPI, &intake.APICall{Method: "GET", Route: "/x", Status: 0, Code: "cross_origin"}, ""},
		{"code misdirected", intake.KindAPI, &intake.APICall{Method: "GET", Route: "/x", Status: 403, Code: "misdirected"}, ""},
		{"code unknown but token", intake.KindAPI, &intake.APICall{Method: "GET", Route: "/x", Status: 500, Code: "kaboom"}, ""},
		{"code upper case is a token", intake.KindAPI, &intake.APICall{Method: "GET", Route: "/x", Status: 500, Code: "Kaboom"}, ""},
		{"code with space", intake.KindAPI, &intake.APICall{Method: "GET", Route: "/x", Status: 500, Code: "not ok"}, "reports[0].api.code"},
		{"code with punctuation", intake.KindAPI, &intake.APICall{Method: "GET", Route: "/x", Status: 500, Code: "a;b"}, "reports[0].api.code"},
		{"code with newline", intake.KindAPI, &intake.APICall{Method: "GET", Route: "/x", Status: 500, Code: "a\nb"}, "reports[0].api.code"},
		{"code too long", intake.KindAPI, &intake.APICall{Method: "GET", Route: "/x", Status: 500, Code: strings.Repeat("a", 500)}, "reports[0].api.code"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := r.report()
			rep.Kind, rep.API = tc.kind, tc.api
			err := intake.Batch{Reports: []intake.Report{rep}}.Validate()
			if tc.field == "" {
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if !errs.Is(err, errs.Invalid) || !hasField(err, tc.field) {
				t.Errorf("err=%v", err)
			}
			if tc.field == "reports[0].api.code" {
				for _, f := range validate.Fields(err) {
					if f.Field == tc.field && f.Code != "malformed" {
						t.Errorf("code field Code = %q, want malformed", f.Code)
					}
				}
			}
		})
	}
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		rep := r.report()
		rep.Kind, rep.API = intake.KindAPI, &intake.APICall{Method: m, Route: "/x", Status: 500}
		if err := rep.Validate(); err != nil {
			t.Errorf("method %s: %v", m, err)
		}
	}
}

func TestBatchValidate_ScreenNotToken_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	for _, s := range []string{"two words", "/a?x=1", "a@b.example", repeat('s', 129)} {
		rep := r.report()
		rep.Screen = s
		err := intake.Batch{Reports: []intake.Report{rep}}.Validate()
		if !errs.Is(err, errs.Invalid) || !hasField(err, "reports[0].screen") {
			t.Errorf("screen %q: err=%v", s, err)
		}
	}
	rep := r.report()
	rep.Screen = ""
	if err := rep.Validate(); err != nil {
		t.Errorf("screen is optional: %v", err)
	}
}

func TestBatchValidate_RefAndTraceID_ReturnInvalid(t *testing.T) {
	r := newRig(t)
	for _, ref := range []string{"1a2b-3c4d", "1A2B3C4D", "1A2B-3C4", "nonsense"} {
		rep := r.report()
		rep.ErrorRef = ref
		err := intake.Batch{Reports: []intake.Report{rep}}.Validate()
		if !errs.Is(err, errs.Invalid) || !hasField(err, "reports[0].error_ref") {
			t.Errorf("error_ref %q: err=%v", ref, err)
		}
	}
	for _, id := range []string{
		"A1B2C3D4E5F60718293A4B5C6D7E8F90", "a1b2c3d4e5f60718293a4b5c6d7e8f9", "a1b2c3d4e5f60718293a4b5c6d7e8f900",
		"g1b2c3d4e5f60718293a4b5c6d7e8f90", "short",
	} {
		rep := r.report()
		rep.TraceID = id
		err := intake.Batch{Reports: []intake.Report{rep}}.Validate()
		if !errs.Is(err, errs.Invalid) || !hasField(err, "reports[0].trace_id") {
			t.Errorf("trace_id %q: err=%v", id, err)
		}
	}
	rep := r.report()
	rep.ErrorRef, rep.TraceID = "1A2B-3C4D", "a1b2c3d4e5f60718293a4b5c6d7e8f90"
	if err := rep.Validate(); err != nil {
		t.Errorf("valid ref and trace id: %v", err)
	}
}

func TestBatchValidate_Count_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	for _, n := range []int{-1, intake.MaxCount + 1} {
		rep := r.report()
		rep.Count = n
		err := intake.Batch{Reports: []intake.Report{rep}}.Validate()
		if !errs.Is(err, errs.Invalid) || !hasField(err, "reports[0].count") {
			t.Errorf("count %d: err=%v", n, err)
		}
	}
	for _, n := range []int{0, 1, intake.MaxCount} {
		rep := r.report()
		rep.Count = n
		if err := rep.Validate(); err != nil {
			t.Errorf("count %d: %v", n, err)
		}
	}
}

func TestBatchValidate_VersionAndMessage_ReturnInvalid(t *testing.T) {
	r := newRig(t)
	for _, v := range []string{"has space", "a@b", repeat('v', 65)} {
		rep := r.report()
		rep.AppVersion = v
		err := intake.Batch{Reports: []intake.Report{rep}}.Validate()
		if !errs.Is(err, errs.Invalid) || !hasField(err, "reports[0].app_version") {
			t.Errorf("app_version %q: err=%v", v, err)
		}
	}
	rep := r.report()
	rep.Message = ""
	err := intake.Batch{Reports: []intake.Report{rep}}.Validate()
	if !errs.Is(err, errs.Invalid) || !hasField(err, "reports[0].message") {
		t.Errorf("empty message: err=%v", err)
	}
	rep = r.report()
	rep.AppVersion = repeat('v', 64)
	if err := rep.Validate(); err != nil {
		t.Errorf("64-byte version: %v", err)
	}
}

func TestBatchValidate_ReportsEveryProblem(t *testing.T) {
	r := newRig(t)
	a, b := r.report(), r.report()
	a.Kind = "bad"
	b.Count = -5
	err := intake.Batch{Reports: []intake.Report{a, b}}.Validate()
	if !hasField(err, "reports[0].kind") || !hasField(err, "reports[1].count") {
		t.Errorf("fields = %v", validate.Fields(err))
	}
	if err := (intake.Batch{}).Validate(); !errs.Is(err, errs.Invalid) || !hasField(err, "reports") {
		t.Errorf("empty batch: %v", err)
	}
}

func TestAccept_OccurredAtOutOfWindow_ReturnsInvalid(t *testing.T) {
	r := newRig(t)
	now := r.clk.Now()
	bad := map[string]time.Time{
		"zero":           {},
		"past 25h":       now.Add(-25 * time.Hour),
		"future 25h":     now.Add(25 * time.Hour),
		"exactly -24h":   now.Add(-intake.OccurredWithin),
		"exactly +24h":   now.Add(intake.OccurredWithin),
		"year ago":       now.AddDate(-1, 0, 0),
		"far future now": now.AddDate(1, 0, 0),
	}
	for name, ts := range bad {
		t.Run(name, func(t *testing.T) {
			rep := r.report()
			rep.OccurredAt = ts
			err := r.submit(netip.MustParseAddr("198.51.100.1"), rep)
			if !errs.Is(err, errs.Invalid) || !hasField(err, "reports[0].occurred_at") {
				t.Errorf("err=%v", err)
			}
		})
	}
	good := map[string]time.Time{
		"now":     now,
		"-23h59m": now.Add(-23*time.Hour - 59*time.Minute),
		"+23h59m": now.Add(23*time.Hour + 59*time.Minute),
	}
	for name, ts := range good {
		t.Run(name, func(t *testing.T) {
			rep := r.report()
			rep.OccurredAt = ts
			if err := r.submit(netip.MustParseAddr("198.51.100.2"), rep); err != nil {
				t.Errorf("err=%v", err)
			}
		})
	}
	if got := r.measurementSum("bms.client_errors.rejected", "reason", "invalid"); got < 1 {
		t.Errorf("rejected{invalid} = %v", got)
	}
}

func TestAccept_PerAddressLimit_ReturnsRateLimitedWithoutLoggingAddress(t *testing.T) {
	r := newRig(t)
	for range 3 {
		if err := r.submit(addr1, r.reports(10)...); err != nil {
			t.Fatalf("within limit: %v", err)
		}
	}
	before := len(r.clientRecords())
	err := r.submit(addr1, r.report())
	if !errors.Is(err, intake.ErrRateLimited) || !errs.Is(err, errs.Unavailable) {
		t.Fatalf("err = %v", err)
	}
	if len(r.clientRecords()) != before {
		t.Error("a rejected batch logged per-report records")
	}
	if got := r.measurementSum("bms.client_errors.rejected", "reason", "rate_limited"); got != 1 {
		t.Errorf("rejected{rate_limited} = %v", got)
	}
	if r.cap.Contains(addr1.String()) {
		t.Error("the address was logged")
	}
	// Another address is unaffected.
	if err := r.submit(netip.MustParseAddr("203.0.113.8"), r.report()); err != nil {
		t.Errorf("other address: %v", err)
	}
	// A whole batch is refused when it does not fit, nothing trickles through.
	err = r.submit(netip.MustParseAddr("203.0.113.9"), r.reports(20)...)
	if err != nil {
		t.Fatal(err)
	}
	n := len(r.clientRecords())
	if err := r.submit(netip.MustParseAddr("203.0.113.9"), r.reports(11)...); !errors.Is(err, intake.ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	if len(r.clientRecords()) != n {
		t.Error("part of a refused batch was logged")
	}
}

func TestAccept_GlobalLimit_ReturnsRateLimited(t *testing.T) {
	r := newRig(t)
	per := intake.PerAddressPerMinute
	for i := range intake.GlobalPerMinute / per {
		from := netip.AddrFrom4([4]byte{10, 0, byte(i), 1})
		if err := r.submit(from, r.reports(20)...); err != nil {
			t.Fatalf("address %d: %v", i, err)
		}
		if err := r.submit(from, r.reports(per-20)...); err != nil {
			t.Fatalf("address %d second batch: %v", i, err)
		}
	}
	err := r.submit(netip.MustParseAddr("192.0.2.200"), r.report())
	if !errors.Is(err, intake.ErrRateLimited) || !errs.Is(err, errs.Unavailable) {
		t.Fatalf("err = %v", err)
	}
	r.advance(t, time.Minute)
	if err := r.submit(netip.MustParseAddr("192.0.2.200"), r.report()); err != nil {
		t.Errorf("after a minute: %v", err)
	}
}

func TestBucket_RefillsWithFakeClock(t *testing.T) {
	r := newRig(t)
	if err := r.submit(addr1, r.reports(20)...); err != nil {
		t.Fatal(err)
	}
	if err := r.submit(addr1, r.reports(10)...); err != nil {
		t.Fatal(err)
	}
	if err := r.submit(addr1, r.report()); !errors.Is(err, intake.ErrRateLimited) {
		t.Fatalf("exhausted: %v", err)
	}
	r.advance(t, time.Minute)
	if err := r.submit(addr1, r.reports(20)...); err != nil {
		t.Errorf("after a minute: %v", err)
	}
	if err := r.submit(addr1, r.reports(10)...); err != nil {
		t.Errorf("after a minute, rest: %v", err)
	}
}

func TestBucket_IPv6KeyedBySlash64(t *testing.T) {
	r := newRig(t)
	a := netip.MustParseAddr("2001:db8:1:2::1")
	sameNet := netip.MustParseAddr("2001:db8:1:2:ffff::9")
	otherNet := netip.MustParseAddr("2001:db8:1:3::1")
	if err := r.submit(a, r.reports(20)...); err != nil {
		t.Fatal(err)
	}
	if err := r.submit(a, r.reports(10)...); err != nil {
		t.Fatal(err)
	}
	if err := r.submit(sameNet, r.report()); !errors.Is(err, intake.ErrRateLimited) {
		t.Errorf("same /64 must share the bucket: %v", err)
	}
	if err := r.submit(otherNet, r.report()); err != nil {
		t.Errorf("other /64: %v", err)
	}
}

func TestAccept_HourlyRateLimitLineHasNoAddress(t *testing.T) {
	r := newRig(t)
	if err := r.submit(addr1, r.reports(20)...); err != nil {
		t.Fatal(err)
	}
	if err := r.submit(addr1, r.reports(10)...); err != nil {
		t.Fatal(err)
	}
	lines := func() int {
		n := 0
		for _, rec := range r.cap.Records() {
			if rec.Message == "client error reports rate-limited" {
				n++
			}
		}
		return n
	}
	for range 5 {
		if err := r.submit(addr1, r.report()); !errors.Is(err, intake.ErrRateLimited) {
			t.Fatalf("err = %v", err)
		}
	}
	if got := lines(); got != 1 {
		t.Errorf("rate-limit lines within the hour = %d, want 1", got)
	}
	r.advance(t, time.Hour+time.Second)
	if err := r.submit(addr1, r.reports(20)...); err != nil {
		t.Fatal(err)
	}
	if err := r.submit(addr1, r.reports(10)...); err != nil {
		t.Fatal(err)
	}
	if err := r.submit(addr1, r.report()); !errors.Is(err, intake.ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	if got := lines(); got != 2 {
		t.Errorf("rate-limit lines after the hour = %d, want 2", got)
	}
	if r.cap.Contains(addr1.String()) {
		t.Error("the address was logged")
	}
}

func TestAccept_ContextCanceled_ReturnsCanceled(t *testing.T) {
	r := newRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sub := intake.Submission{From: addr1, Batch: intake.Batch{Reports: []intake.Report{r.report()}}}
	if err := r.in.Accept(ctx, sub); !errs.Is(err, errs.Canceled) {
		t.Errorf("canceled: %v", err)
	}
	dctx, dcancel := context.WithDeadline(context.Background(), epoch.Add(-time.Hour))
	defer dcancel()
	if err := r.in.Accept(dctx, sub); !errs.Is(err, errs.Timeout) {
		t.Errorf("deadline: %v", err)
	}
	if len(r.clientRecords()) != 0 {
		t.Error("records written for a done context")
	}
}

func TestAccept_TextFields_ScrubbedAndTruncated(t *testing.T) {
	r := newRig(t)
	rep := r.report()
	rep.Message = repeat('m', 600) + " " + markerEmail
	rep.Stack = "Error at /app?token=" + marker + " by " + markerEmail + "\n" + strings.Repeat("at frame (a.js:1:1)\n", 400)
	err := r.in.Accept(context.Background(), intake.Submission{
		From: addr1, UserAgent: "UA/" + repeat('u', 300), Batch: intake.Batch{Reports: []intake.Report{rep}},
	})
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	recs := r.clientRecords()
	if len(recs) != 1 {
		t.Fatalf("records = %d", len(recs))
	}
	a := recs[0].Attrs
	if len(a["client.message"]) > intake.MaxMessageBytes {
		t.Errorf("message len %d", len(a["client.message"]))
	}
	if len(a["client.stack"]) > intake.MaxStackBytes || !strings.HasPrefix(a["client.stack"], "Error at /app?[query]") {
		t.Errorf("stack len %d, head %q", len(a["client.stack"]), a["client.stack"][:30])
	}
	if len(a["user_agent"]) > intake.MaxUserAgentBytes {
		t.Errorf("user_agent len %d", len(a["user_agent"]))
	}
	if r.cap.Contains(marker) || r.cap.Contains("zq9@") {
		t.Error("marker leaked")
	}

	short := r.report()
	short.Message = "failed for " + markerEmail
	if err := r.submit(addr1, short); err != nil {
		t.Fatal(err)
	}
	if got := r.clientRecords()[1].Attrs["client.message"]; !strings.Contains(got, "[email]") {
		t.Errorf("message = %q", got)
	}
}

func TestAccept_IDSegments_ReplacedWithPlaceholder(t *testing.T) {
	r := newRig(t)
	rep := r.report()
	rep.Screen = "/people/123e4567-e89b-12d3-a456-426614174000/edit"
	api := r.report()
	api.Kind = intake.KindAPI
	api.API = &intake.APICall{Method: "GET", Route: "/api/files/0123456789abcdef0123456789abcdef", Status: 404, Code: "not_found"}
	if err := r.submit(addr1, rep, api); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	recs := r.clientRecords()
	if len(recs) != 2 {
		t.Fatalf("records = %d", len(recs))
	}
	if got := recs[0].Attrs["client.screen"]; got != "/people/{id}/edit" {
		t.Errorf("screen = %q", got)
	}
	if got := recs[1].Attrs["client.api.route"]; got != "/api/files/{id}" {
		t.Errorf("route = %q", got)
	}
	if r.cap.Contains("123e4567") {
		t.Error("UUID reached the log")
	}
}

func TestBatch_JSONIgnoresUnknownFields(t *testing.T) {
	body := `{"reports":[{"kind":"api","message":"boom","occurred_at":"2026-10-17T11:59:00Z","count":2,
		"app_version":"1.0.0","error_ref":"1A2B-3C4D","trace_id":"a1b2c3d4e5f60718293a4b5c6d7e8f90",
		"screen":"/people/{id}","future_field":{"x":1},
		"api":{"method":"GET","route":"/api/x","status":500,"code":"internal","extra":true}}],"extra":1}`
	var b intake.Batch
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(b.Reports) != 1 {
		t.Fatalf("reports = %d", len(b.Reports))
	}
	rep := b.Reports[0]
	if rep.Kind != intake.KindAPI || rep.Message != "boom" || rep.Count != 2 || rep.API == nil ||
		rep.API.Status != 500 || rep.API.Route != "/api/x" || rep.ErrorRef != "1A2B-3C4D" ||
		rep.OccurredAt.Hour() != 11 || rep.Screen != "/people/{id}" || rep.AppVersion != "1.0.0" {
		t.Errorf("report = %+v", rep)
	}
	if err := b.Validate(); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestAccept_SafeForConcurrentUse(t *testing.T) {
	r := newRig(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var ok int
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := r.submit(netip.AddrFrom4([4]byte{10, 1, 0, byte(i & 0xff)}), r.report())
			if err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 20 || len(r.clientRecords()) != 20 {
		t.Errorf("ok=%d records=%d", ok, len(r.clientRecords()))
	}
}

func repeat(c byte, n int) string { return strings.Repeat(string(c), n) }
