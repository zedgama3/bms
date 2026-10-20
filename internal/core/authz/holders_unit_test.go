package authz_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
)

// F67: an undeclared (or malformed) permission is the caller's bug, answered before any query.
func TestHoldersOf_Undeclared_ReturnsInvalid(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    string
	}{
		{"undeclared", string(pGhost)},
		{"malformed", "NotAPermission"},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, f := queryless(t)
			got, err := r.svc.HoldersOf(t.Context(), authzPermission(tc.p))
			wantKind(t, err, errs.Invalid)
			if got != nil {
				t.Errorf("HoldersOf returned %v with an error", got)
			}
			wantNoStatements(t, f)
		})
	}
}

// A system read: no actor is needed, and none is consulted.
func TestHoldersOf_NoActor_StillAnswers(t *testing.T) {
	r, _ := queryless(t)
	got, err := r.svc.HoldersOf(t.Context(), pPayApprove)
	wantNoErr(t, err)
	if got == nil || len(got) != 0 {
		t.Errorf("HoldersOf on an empty database = %#v, want an empty non-nil slice", got)
	}
}
