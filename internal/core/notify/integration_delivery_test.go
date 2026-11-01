//go:build integration

package notify_test

// The address rule moved in from app (boundaries step 8): the login address first, else the
// contact address, else the message is undeliverable (no_address, ending Skipped). Every
// address is invented.

import (
	"errors"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// TestAddressSource_Table (moved from app).
func TestAddressSource_Table(t *testing.T) {
	down := errs.E("test", errs.Unavailable, errors.New("down"))
	for _, tc := range []struct {
		name      string
		login     model.Email // "" = the person has no account
		contact   model.Email // "" = the person has no profile
		accErr    error
		contErr   error
		wantTo    string
		wantState string // "sent", "skipped", "queued" (retried)
	}{
		{name: "account first", login: "login@example.org", contact: "contact@example.org", wantTo: "login@example.org", wantState: "sent"},
		{name: "contact when no account", contact: "contact@example.org", wantTo: "contact@example.org", wantState: "sent"},
		{name: "neither is undeliverable", wantState: "skipped"},
		{name: "an identity failure is not hidden", contact: "contact@example.org", accErr: down, wantState: "queued"},
		{name: "a people failure is not hidden", contErr: down, wantState: "queued"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newIRig(t)
			p := r.queue("")
			if tc.login != "" {
				r.addrs.set(p, tc.login)
			}
			if tc.contact != "" {
				r.conts.set(p, tc.contact)
			}
			r.addrs.mu.Lock()
			r.addrs.err = tc.accErr
			r.addrs.mu.Unlock()
			r.conts.mu.Lock()
			r.conts.err = tc.contErr
			r.conts.mu.Unlock()
			r.sendOnce()
			x := r.one(p)
			if x.Status != tc.wantState {
				t.Fatalf("status = %q, want %q (row %+v)", x.Status, tc.wantState, x)
			}
			envs := r.ch.envelopes()
			switch tc.wantState {
			case "sent":
				if len(envs) != 1 || string(envs[0].To) != tc.wantTo {
					t.Fatalf("envelopes = %+v, want one to %s", envs, tc.wantTo)
				}
			case "skipped":
				if x.LastErr == nil || *x.LastErr != "no_address" {
					t.Errorf("last error = %v, want no_address", x.LastErr)
				}
				fallthrough
			default:
				if len(envs) != 0 {
					t.Errorf("sent %d envelopes, want none", len(envs))
				}
			}
		})
	}
}

// A login address wins even when the contact address is also set, and the contact is not
// even asked.
func TestAddressSource_AccountFirst_DoesNotAskTheProfile(t *testing.T) {
	r := newIRig(t)
	p := r.queue("login@example.org")
	r.conts.set(p, "contact@example.org")
	r.sendOnce()
	if n := r.conts.calls.Load(); n != 0 {
		t.Errorf("ContactEmailOf called %d times although the account had an address", n)
	}
}
