package intake_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/telemetry/intake"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/clock"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// Tests for the error-route half of Amendment T1 (D54): the chunk kind, duration_ms and the
// screen and route allowlists. Spec: telemetry.md T1.2, T1.5; F65, F69, F70, F71.

func newAllowRig(t *testing.T, screens, routes []string) *rig {
	t.Helper()
	c := telemetrytest.New(t)
	clk := clock.NewFake(epoch)
	in, err := intake.New(intake.Options{
		Logger: c.Logger(), Meter: c.Meter("intake"), Clock: clk, Screens: screens, Routes: routes,
	})
	if err != nil {
		t.Fatalf("intake.New: %v", err)
	}
	return &rig{in: in, cap: c, clk: clk}
}

func TestBatchValidate_ChunkAndDuration_Table(t *testing.T) {
	r := newRig(t)
	chunk := func() intake.Report {
		rep := r.report()
		rep.Kind = intake.KindChunk
		rep.Message = "chunk load failed assets/volunteers-3f9a1c.js"
		rep.Screen = "/volunteers/shifts"
		return rep
	}
	api := func(d int) intake.Report {
		rep := r.report()
		rep.Kind = intake.KindAPI
		rep.API = &intake.APICall{Method: "GET", Route: "/api/x", Status: 0, Code: "network", DurationMs: d}
		return rep
	}
	withAPI := func() intake.Report {
		rep := chunk()
		rep.API = &intake.APICall{Method: "GET", Route: "/api/x", Status: 500}
		return rep
	}
	cases := []struct {
		name  string
		rep   intake.Report
		field string // "" means valid
	}{
		{"chunk without api", chunk(), ""},
		{"chunk with api", withAPI(), "reports[0].api"},
		{"duration unknown", api(0), ""},
		{"duration one", api(1), ""},
		{"duration typical", api(35_000), ""},
		{"duration at max", api(intake.MaxDurationMs), ""},
		{"duration over max", api(intake.MaxDurationMs + 1), "reports[0].api.duration_ms"},
		{"duration negative", api(-1), "reports[0].api.duration_ms"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := intake.Batch{Reports: []intake.Report{tc.rep}}.Validate()
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

func TestAccept_ChunkReport_LoggedWithKindAndCountedByKind(t *testing.T) {
	r := newAllowRig(t, nil, nil)
	rep := r.report()
	rep.Kind = intake.KindChunk
	rep.Message = "chunk load failed assets/volunteers-3f9a1c.js"
	rep.Screen = "/volunteers/shifts"
	if err := r.submit(addr1, rep); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	recs := r.clientRecords()
	if len(recs) != 1 || recs[0].Attrs["client.kind"] != "chunk" || recs[0].Attrs["client.screen"] != "/volunteers/shifts" {
		t.Fatalf("records = %+v", recs)
	}
	if got := r.measurementSum("bms.client_errors.accepted", "kind", "chunk"); got != 1 {
		t.Errorf("accepted{chunk} = %v", got)
	}
}

func TestAccept_DurationMs_ReachesTheRecord(t *testing.T) {
	r := newAllowRig(t, nil, nil)
	rep := r.report()
	rep.Kind = intake.KindAPI
	rep.API = &intake.APICall{Method: "GET", Route: "/api/x", Status: 0, Code: "network", DurationMs: 35_012}
	if err := r.submit(addr1, rep); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	recs := r.clientRecords()
	if len(recs) != 1 {
		t.Fatalf("records = %d", len(recs))
	}
	found := false
	for k, v := range recs[0].Attrs {
		if v == "35012" {
			found = true
			t.Logf("duration is on attribute %q", k)
		}
	}
	if !found {
		t.Errorf("duration_ms 35012 is on no attribute: %v", recs[0].Attrs)
	}
}

func TestAccept_UnknownAPIRoute_BecomesUnmatched(t *testing.T) { // F70
	r := newAllowRig(t, nil, []string{"/api/people/{id}", "/api/modules"})
	mk := func(route string) intake.Report {
		rep := r.report()
		rep.Kind = intake.KindAPI
		rep.API = &intake.APICall{Method: "GET", Route: route, Status: 500, Code: "internal"}
		return rep
	}
	err := r.submit(addr1, mk("/api/people/{id}"), mk("/api/ZQ9MARKER7/thing"), mk("/api/people/"+uuidA))
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	recs := r.clientRecords()
	if len(recs) != 3 {
		t.Fatalf("records = %d", len(recs))
	}
	if recs[0].Attrs["client.api.route"] != "/api/people/{id}" {
		t.Errorf("known route = %q", recs[0].Attrs["client.api.route"])
	}
	if recs[1].Attrs["client.api.route"] != "/{unmatched}" {
		t.Errorf("unknown route = %q, want /{unmatched}", recs[1].Attrs["client.api.route"])
	}
	if recs[2].Attrs["client.api.route"] != "/api/people/{id}" {
		t.Errorf("ID-shaped segment is replaced before the allowlist check: %q", recs[2].Attrs["client.api.route"])
	}
	if got := r.measurementSum("bms.client_events.folded", "field", "api.route"); got != 1 {
		t.Errorf("folded{api.route} = %v, want 1", got)
	}
	if r.cap.Contains(marker) || r.cap.Contains(uuidA) {
		t.Error("an unknown route or an ID reached a log or a metric")
	}
}

func TestAccept_NoRoutesConfigured_KeepsRoute(t *testing.T) {
	r := newAllowRig(t, nil, nil)
	rep := r.report()
	rep.Kind = intake.KindAPI
	rep.API = &intake.APICall{Method: "GET", Route: "/api/anything/{id}", Status: 500}
	if err := r.submit(addr1, rep); err != nil {
		t.Fatal(err)
	}
	if got := r.clientRecords()[0].Attrs["client.api.route"]; got != "/api/anything/{id}" {
		t.Errorf("route = %q", got)
	}
}

func TestAccept_UnknownScreen_MetricFoldedLogKeepsValue(t *testing.T) { // F65
	r := newAllowRig(t, []string{"/people/{id}", "/"}, nil)
	known, unknown, none := r.report(), r.report(), r.report()
	known.Screen = "/people/{id}"
	unknown.Screen = "/ZQ9MARKER7/screen"
	none.Screen = ""
	idScreen := r.report()
	idScreen.Screen = "/people/" + uuidA
	if err := r.submit(addr1, known, unknown, none, idScreen); err != nil {
		t.Fatalf("Accept: %v", err)
	}
	var metricScreens []string
	for _, m := range r.cap.Measurements() {
		if m.Instrument == "bms.client_errors.accepted" {
			metricScreens = append(metricScreens, m.Attrs["screen"])
		}
	}
	want := []string{"/people/{id}", intake.OtherToken, "", "/people/{id}"}
	if len(metricScreens) != len(want) {
		t.Fatalf("accepted measurements = %v", metricScreens)
	}
	for i := range want {
		if metricScreens[i] != want[i] {
			t.Errorf("accepted[%d].screen = %q, want %q", i, metricScreens[i], want[i])
		}
	}
	if got := r.measurementSum("bms.client_events.folded", "field", "screen"); got != 1 {
		t.Errorf("folded{screen} = %v, want 1", got)
	}
	recs := r.clientRecords()
	if recs[1].Attrs["client.screen"] != "/ZQ9MARKER7/screen" {
		t.Errorf("the log record must keep the unfolded value, got %q", recs[1].Attrs["client.screen"])
	}
	if recs[3].Attrs["client.screen"] != "/people/{id}" {
		t.Errorf("the log record keeps the ID-replaced value, got %q", recs[3].Attrs["client.screen"])
	}
	for _, m := range r.cap.Measurements() {
		if m.Instrument == "bms.client_errors.accepted" {
			if m.Attrs["screen"] == "/ZQ9MARKER7/screen" {
				t.Error("an unknown screen reached a metric attribute")
			}
		}
	}
}
