//go:build integration

package people_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// A person with a contact and a consent, ready for the PeopleSource.
func (r *irig) sourcePerson() model.PersonID {
	r.t.Helper()
	p := r.insertPerson(seed{Playa: mPlaya, Legal: mLegal, Phone: mPhone, ContactEmail: mContactEmail})
	r.insertContact(p, mECName, mECPhone, mRelationship, 1)
	r.exec(`INSERT INTO core.person_consent (person_id, purpose, granted, recorded_by, changed_at) VALUES ($1, 'volunteers.share-contact', true, $1, $2)`,
		[16]byte(p), r.clk.Now())
	return p
}

// B20: a module that forgot a table leaves a foreign key behind. people's rows cannot go,
// the whole transaction rolls back, and nothing is announced.
func TestDeletePerson_ModuleStillReferences_ReturnsConflict(t *testing.T) {
	r := newIRig(t, false)
	p := r.sourcePerson()
	r.exec("CREATE TABLE core.zz_forgotten (person_id uuid NOT NULL REFERENCES core.person(id) ON DELETE RESTRICT)")
	r.exec("INSERT INTO core.zz_forgotten (person_id) VALUES ($1)", [16]byte(p))

	err := inWrite(t, r.pool, func(ctx context.Context) error {
		_, derr := r.svc.DeletePerson(ctx, p)
		return keep(derr)
	})
	wantKind(t, err, errs.Conflict)
	if !r.rowExists(p) || r.contactRows(p) != 1 || r.consentRows(p) != 1 {
		t.Errorf("the failed deletion was not rolled back: row %v, contacts %d, consents %d", r.rowExists(p), r.contactRows(p), r.consentRows(p))
	}
	if got := r.published(sigDeleted); len(got) != 0 {
		t.Errorf("people.person-deleted published %d times for a deletion that failed", len(got))
	}
}

func TestDeletePerson_DeletesTheRowsAndAnnouncesOnce(t *testing.T) {
	r := newIRig(t, false)
	p := r.sourcePerson()
	var n int
	// No session in the ctx: the caller has authorized, and people checks nothing.
	wantNoErr(t, inWrite(t, r.pool, func(ctx context.Context) error {
		var derr error
		n, derr = r.svc.DeletePerson(ctx, p)
		return keep(derr)
	}))
	if n != 3 {
		t.Errorf("DeletePerson = %d rows, want 3 (the person, the contact, the consent)", n)
	}
	if r.rowExists(p) || r.contactRows(p) != 0 || r.consentRows(p) != 0 {
		t.Error("people's rows survive")
	}
	sigs := r.published(sigDeleted)
	if len(sigs) != 1 || payloadPerson(sigs[0]) != p {
		t.Errorf("published %v, want one people.person-deleted for the person", sigs)
	}
	if len(r.rec.Recorded()) != 0 {
		t.Errorf("entries %v: datarights records the deletion, people does not", actionsOf(r.rec.Recorded()))
	}
	r.noLeak(allMarkers()...)
}

func TestDeletePerson_UnknownPerson_ReturnsNotFound(t *testing.T) {
	r := newIRig(t, false)
	err := inWrite(t, r.pool, func(ctx context.Context) error {
		_, derr := r.svc.DeletePerson(ctx, newPerson())
		return keep(derr)
	})
	wantKind(t, err, errs.NotFound)
	if got := r.published(sigDeleted); len(got) != 0 {
		t.Error("people.person-deleted published for a person who was not there")
	}
}

func TestDeletePerson_RolledBackByTheCaller_PublishesNothing(t *testing.T) {
	r := newIRig(t, false)
	p := r.sourcePerson()
	boom := errs.E("test.later", errs.Unavailable, errors.New("a later source failed"))
	err := inWrite(t, r.pool, func(ctx context.Context) error {
		if _, derr := r.svc.DeletePerson(ctx, p); derr != nil {
			return derr //nolint:wrapcheck // a test closure
		}
		return boom
	})
	wantKind(t, err, errs.Unavailable)
	if !r.rowExists(p) || r.contactRows(p) != 1 || r.consentRows(p) != 1 {
		t.Error("the caller's rollback did not restore people's rows")
	}
	if got := r.published(sigDeleted); len(got) != 0 {
		t.Error("people.person-deleted published for a rolled-back deletion")
	}
}

func TestDeletePerson_ReferencesToThePersonAsActor_DoNotBlock(t *testing.T) {
	r := newIRig(t, false)
	r.link()
	mgr, mctx := r.admin(permManage, permDirectory)
	created := r.create(mctx, "Walk On").Person.ID
	_, err := r.svc.SetConsent(mctx, created, "volunteers.share-contact", true)
	wantNoErr(t, err)
	r.deletePerson(mgr)
	if !r.rowExists(created) {
		t.Fatal("deleting the creator deleted the created person")
	}
	if n := r.scalarInt("SELECT count(*) FROM core.person WHERE id = $1 AND created_by IS NULL", [16]byte(created)); n != 1 {
		t.Error("created_by was not cleared")
	}
	ok, err := r.svc.ConsentOf(ctxT(t), created, "volunteers.share-contact")
	wantNoErr(t, err)
	if !ok {
		t.Error("deleting the manager who recorded a consent removed the consent")
	}
}

func TestExportPerson_ReturnsRowsContactsAndConsents(t *testing.T) {
	r := newIRig(t, false)
	p := r.sourcePerson()
	raw, err := r.svc.ExportPerson(ctxT(t), p) // no actor: the caller has authorized
	wantNoErr(t, err)
	var doc struct {
		Person struct {
			ID           string `json:"id"`
			PlayaName    string `json:"playa_name"`
			LegalName    string `json:"legal_name"`
			Phone        string `json:"phone"`
			ContactEmail string `json:"contact_email"`
		} `json:"person"`
		Contacts []struct {
			Name         string `json:"name"`
			Phone        string `json:"phone"`
			Relationship string `json:"relationship"`
			Position     int    `json:"position"`
		} `json:"emergency_contacts"`
		Consents []struct {
			Purpose string `json:"purpose"`
			Granted bool   `json:"granted"`
		} `json:"consents"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the export part is not JSON: %v\n%s", err, raw)
	}
	pp := doc.Person
	if pp.ID != p.String() || pp.PlayaName != mPlaya || pp.LegalName != mLegal || pp.Phone != mPhone || pp.ContactEmail != mContactEmail {
		t.Errorf("person = %+v", pp)
	}
	if len(doc.Contacts) != 1 || doc.Contacts[0].Name != mECName || doc.Contacts[0].Phone != mECPhone || doc.Contacts[0].Relationship != mRelationship || doc.Contacts[0].Position != 1 {
		t.Errorf("emergency_contacts = %+v", doc.Contacts)
	}
	if len(doc.Consents) != 1 || doc.Consents[0].Purpose != "volunteers.share-contact" || !doc.Consents[0].Granted {
		t.Errorf("consents = %+v", doc.Consents)
	}
	if len(r.rec.Recorded()) != 0 {
		t.Errorf("entries %v: datarights records the export, people does not", actionsOf(r.rec.Recorded()))
	}
}

func TestExportPerson_NoRow_ReturnsNotFound(t *testing.T) {
	r := newIRig(t, false)
	raw, err := r.svc.ExportPerson(ctxT(t), newPerson())
	wantKind(t, err, errs.NotFound)
	if len(raw) != 0 {
		t.Error("a document came back with the error")
	}
}

func TestExists_Table(t *testing.T) {
	r := newIRig(t, false)
	p := r.insertPerson(seed{Playa: mPlaya})
	tests := []struct {
		name string
		p    model.PersonID
		want errs.Kind // 0: nil
	}{
		{"a person with a row", p, 0},
		{"no row", newPerson(), errs.NotFound},
		{"zero person", model.PersonID{}, errs.Invalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := r.svc.Exists(ctxT(t), tc.p)
			if tc.want == 0 {
				wantNoErr(t, err)
				return
			}
			wantKind(t, err, tc.want)
		})
	}
	// Inside a Write transaction it sees the transaction's own deletion.
	wantNoErr(t, inWrite(t, r.pool, func(ctx context.Context) error {
		if _, err := r.svc.DeletePerson(ctx, p); err != nil {
			return err //nolint:wrapcheck // a test closure
		}
		if err := r.svc.Exists(ctx, p); !errs.Is(err, errs.NotFound) {
			t.Errorf("Exists inside the deleting transaction = %v, want NotFound", err)
		}
		return nil
	}))
}
