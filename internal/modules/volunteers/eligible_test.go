package volunteers_test

import (
	"net/http"
	"testing"
)

// F181: GET /shifts?eligible=<not a boolean> is Invalid "malformed" on "eligible".
func TestShiftsRoute_BadEligible_ReturnsInvalid(t *testing.T) {
	r := newRig(t, opts{})
	p := r.volunteer("Dandelion")
	for _, v := range []string{"maybe", "yes", "2", "on", "tru", "%20", "eligible"} {
		t.Run(v, func(t *testing.T) {
			wantField(t, wantProblem(t, r.call(p, "GET", "/shifts?eligible="+v, nil), http.StatusBadRequest, "invalid"), "eligible", "malformed")
		})
	}
}

// F181's other side: booleans parse like "canceled" and do not fail.
func TestShiftsRoute_GoodEligible_Accepted(t *testing.T) {
	r := newRig(t, opts{})
	p := r.volunteer("Dandelion")
	for _, v := range []string{"true", "false", "1", "0", "TRUE", "t", "f", ""} {
		t.Run("eligible="+v, func(t *testing.T) {
			rec := r.call(p, "GET", "/shifts?eligible="+v, nil)
			if rec.Code == http.StatusBadRequest {
				t.Fatalf("eligible=%q refused: %s", v, rec.Body.String())
			}
		})
	}
}
