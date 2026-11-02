package people_test

import (
	"context"
	"testing"

	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// people answers as datarights' PeopleSource (boundaries.md decision 5): ExportPerson,
// DeletePerson (inside the caller's Write transaction) and Exists. datarights authorizes and
// audits; people knows its rows.

// inWrite runs fn in a Write transaction of d, as datarights' deletion does.
func inWrite(t *testing.T, d db.DB, fn func(ctx context.Context) error) error {
	t.Helper()
	return d.Transact(ctxT(t), db.Write, fn) //nolint:wrapcheck // a test helper passes the kind through
}

func TestPeopleSource_ZeroPerson_ReturnsInvalid(t *testing.T) {
	u := newUnit(t)
	_, err := u.svc.ExportPerson(ctxT(t), model.PersonID{})
	wantKind(t, err, errs.Invalid)
	wantKind(t, u.svc.Exists(ctxT(t), model.PersonID{}), errs.Invalid)
	err = inWrite(t, u.fake, func(ctx context.Context) error {
		_, derr := u.svc.DeletePerson(ctx, model.PersonID{})
		return keep(derr)
	})
	wantKind(t, err, errs.Invalid)
}

func TestDeletePerson_OutsideAWriteTransaction_ReturnsInternal(t *testing.T) {
	u := newUnit(t)
	_, err := u.svc.DeletePerson(ctxT(t), newPerson())
	wantKind(t, err, errs.Internal)
	err = u.fake.Transact(ctxT(t), db.Read, func(ctx context.Context) error {
		_, derr := u.svc.DeletePerson(ctx, newPerson())
		return keep(derr)
	})
	wantKind(t, err, errs.Internal)
	for _, c := range u.fake.Calls() {
		t.Errorf("a statement ran before the transaction check: %.40s", c.SQL)
	}
}

func TestDeletePerson_ReadOnlyCopy_ReturnsUnavailable(t *testing.T) {
	u := newUnit(t)
	u.fake.SetReadOnly(true)
	err := inWrite(t, u.fake, func(ctx context.Context) error {
		_, derr := u.svc.DeletePerson(ctx, newPerson())
		return keep(derr)
	})
	wantReadOnly(t, err)
	if len(u.bus.Published()) != 0 {
		t.Error("a signal was published on a read-only copy")
	}
}

func TestDeletePerson_RecordsNoAuditEntry_ChecksNoPermission(t *testing.T) {
	// Even a refused-looking call (no session, no grants) is not people's to refuse: the
	// caller has authorized. The fake store has no row, so the outcome is NotFound, not
	// Unauthenticated or Permission.
	u := newUnit(t)
	err := inWrite(t, u.fake, func(ctx context.Context) error {
		_, derr := u.svc.DeletePerson(ctx, newPerson())
		return keep(derr)
	})
	wantKind(t, err, errs.NotFound)
	if len(u.rec.Recorded()) != 0 {
		t.Errorf("entries %v from a source call", actionsOf(u.rec.Recorded()))
	}
}

func TestExportPerson_NeedsNoActor_RecordsNothing(t *testing.T) {
	u := newUnit(t)
	_, err := u.svc.ExportPerson(ctxT(t), newPerson())
	wantKind(t, err, errs.NotFound) // the fake store has no row; not Unauthenticated or Permission
	if len(u.rec.Recorded()) != 0 {
		t.Errorf("entries %v from a source call", actionsOf(u.rec.Recorded()))
	}
}
