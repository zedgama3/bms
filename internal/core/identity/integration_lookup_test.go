//go:build integration

package identity_test

import (
	"context"
	"testing"

	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/telemetry/telemetrytest"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func (p *prig) lookup() *identity.Lookup {
	p.t.Helper()
	c := telemetrytest.New(p.t)
	l, err := identity.NewLookup(identity.LookupOptions{DB: p.fdb, Tracer: c.Tracer("identity")})
	wantNoErr(p.t, err)
	return l
}

// boundaries.md B15: no account for the person is NotFound (notify then tries the contact
// address).
func TestLookup_EmailOf_NoAccount_ReturnsNotFound(t *testing.T) {
	p := newPoolRig(t)
	p.signUp("ada.quill")
	_, err := p.lookup().EmailOf(ctxT(t), newPersonID())
	wantKind(t, err, errs.NotFound)
}

func TestLookup_EmailOf_Account_ReturnsTheLoginAddress_NoAuditNoActor(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	l := p.lookup()
	before := len(p.rec.Recorded())
	got, err := l.EmailOf(ctxT(t), u.sess.Person) // ctxT carries no session and no actor
	wantNoErr(t, err)
	if got.Canonical() != u.email.Canonical() {
		t.Errorf("EmailOf = %q, want the account's address", got)
	}
	if len(p.rec.Recorded()) != before {
		t.Error("the Lookup recorded an audit entry")
	}
}

func TestLookup_EmailOf_Works_OnAReadOnlyCopy(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	l := p.lookup()
	p.fdb.set(func(f *faultDB) { f.ro = true })
	if _, err := l.EmailOf(ctxT(t), u.sess.Person); err != nil {
		t.Fatalf("EmailOf on a read-only copy: %v", err)
	}
}

func TestLookup_EmailOf_CanceledContext_ReturnsCanceled(t *testing.T) {
	p := newPoolRig(t)
	u := p.signUp("ada.quill")
	l := p.lookup()
	ctx, cancel := context.WithCancel(ctxT(t))
	cancel()
	_, err := l.EmailOf(ctx, u.sess.Person)
	wantKind(t, err, errs.Canceled)
}
