package telemetry_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/zedgama3/bms/internal/core/telemetry"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestRefFrom_NoSpan_ReturnsFalse(t *testing.T) {
	ctx := context.Background()
	if r, ok := telemetry.RefFrom(ctx); ok || r != "" {
		t.Errorf("RefFrom = %q %v", r, ok)
	}
	if id, ok := telemetry.TraceIDFrom(ctx); ok || id != "" {
		t.Errorf("TraceIDFrom = %q %v", id, ok)
	}
}

func TestRefFrom_FirstFourBytesUpperHex(t *testing.T) {
	h := newHarness(t, nil)
	hdr := http.Header{}
	hdr.Set("Traceparent", "00-a1b2c3d4e5f60718293a4b5c6d7e8f90-1122334455667788-01")
	ctx, sp := h.tracer(t).Start(context.Background(), "GET /api/x", telemetry.WithParentFrom(hdr))
	defer sp.End()
	r, ok := telemetry.RefFrom(ctx)
	if !ok || r != "A1B2-C3D4" {
		t.Fatalf("RefFrom = %q %v", r, ok)
	}
	if err := r.Validate(); err != nil {
		t.Errorf("own ref invalid: %v", err)
	}
}

func TestTraceIDFrom_MatchesSpan(t *testing.T) {
	h := newHarness(t, nil)
	ctx, sp := h.tracer(t).Start(context.Background(), "ids")
	id, ok := telemetry.TraceIDFrom(ctx)
	sp.End()
	if !ok || len(id) != 32 || id != h.span(t, "ids").TraceID {
		t.Fatalf("TraceIDFrom = %q %v, span %q", id, ok, h.span(t, "ids").TraceID)
	}
	r, _ := telemetry.RefFrom(ctx)
	want := id[:4] + id[4:8]
	if string(r[:4])+string(r[5:]) != upper(want) || r[4] != '-' {
		t.Errorf("ref %q not derived from %q", r, id)
	}
}

func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'f' {
			b[i] = c - 'a' + 'A'
		}
	}
	return string(b)
}

func TestRefValidate_Table(t *testing.T) {
	cases := []struct {
		ref telemetry.Ref
		ok  bool
	}{
		{"1A2B-3C4D", true},
		{"0000-FFFF", true},
		{"1a2b-3c4d", false},
		{"1A2B3C4D", false},
		{"1A2B-3C4", false},
		{"1A2B-3C4DE", false},
		{"1A2B_3C4D", false},
		{"GGGG-HHHH", false},
		{"", false},
		{" 1A2B-3C4D", false},
		{"1A2B-3C4D\n", false},
	}
	for _, tc := range cases {
		err := tc.ref.Validate()
		if tc.ok && err != nil {
			t.Errorf("%q: %v", tc.ref, err)
		}
		if !tc.ok && !errs.Is(err, errs.Invalid) {
			t.Errorf("%q: err = %v, want Invalid", tc.ref, err)
		}
	}
}
