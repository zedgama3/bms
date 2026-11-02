//go:build integration

package people_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
)

// 5. Dropping the orphan on link records people.person.deleted as the system.
func TestLinkAccount_OrphanDrop_RecordsDeletedAsTheSystem(t *testing.T) {
	f := newLinkFixture(t)
	u := f.signUp("orphan.audit", true)
	res, err := f.svc.LinkAccount(f.ctx, f.target, u.email)
	wantNoErr(t, err)
	if !res.OrphanDeleted {
		t.Fatal("setup: the orphan was kept")
	}
	var got []audit.Entry
	for _, e := range f.entries("people.person.deleted") {
		if e.Subject == u.person {
			got = append(got, e)
		}
	}
	if len(got) != 1 {
		t.Fatalf("%d people.person.deleted entries about the orphan, want 1", len(got))
	}
	e := got[0]
	if e.Actor.Kind != audit.ActorSystem || !e.Actor.Person.IsZero() {
		t.Errorf("actor = %+v, want the system", e.Actor)
	}
	if e.Record.Kind != "people.person" || e.Record.ID != [16]byte(u.person) {
		t.Errorf("record = %+v", e.Record)
	}
}
