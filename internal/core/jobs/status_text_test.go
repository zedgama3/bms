package jobs_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/jobs"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// F50.
func TestStatus_MarshalText_Invalid_ReturnsInvalid(t *testing.T) {
	for _, s := range []jobs.Status{0, jobs.Canceled + 1, 200} {
		out, err := s.MarshalText()
		wantKind(t, err, errs.Invalid)
		if !hasField(err, "status", "one_of") {
			t.Errorf("Status(%d): fields %v, want status/one_of", uint8(s), describe(err))
		}
		if out != nil {
			t.Errorf("Status(%d): text %q returned with an error", uint8(s), out)
		}
	}
	for _, s := range []jobs.Status{jobs.Queued, jobs.Running, jobs.Done, jobs.Failed, jobs.Canceled} {
		out, err := s.MarshalText()
		wantNoErr(t, err)
		if string(out) != s.String() {
			t.Errorf("MarshalText = %q, want %q", out, s.String())
		}
	}
}

// F50.
func TestStatus_UnmarshalText_Unknown_ReturnsInvalid(t *testing.T) {
	for _, in := range []string{"", "Queued", "paused", "queued ", "0"} {
		s := jobs.Done
		err := s.UnmarshalText([]byte(in))
		wantKind(t, err, errs.Invalid)
		if !hasField(err, "status", "one_of") {
			t.Errorf("%q: fields %v, want status/one_of", in, describe(err))
		}
		if s != jobs.Done {
			t.Errorf("%q: receiver changed to %v on failure", in, s)
		}
	}
	var s jobs.Status
	wantNoErr(t, s.UnmarshalText([]byte("failed")))
	if s != jobs.Failed {
		t.Errorf("status = %v, want failed", s)
	}
}
