package identity_test

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
)

// The two methods identity adds for people (EmailOf, LinkAccount) are seams: they check
// no permission, so a Service whose checker denies everything still answers them.

func TestEmailOf_ZeroPerson_Invalid(t *testing.T) {
	fake := dbtest.NewFake()
	r := newRig(t, fake, withPerms(allowing()))
	_, err := r.svc.EmailOf(ctxT(t), model.PersonID{})
	wantKind(t, err, errs.Invalid)
	if n := len(fake.Calls()); n != 0 {
		t.Errorf("%d statements for a zero ID", n)
	}
}

// F64
func TestEmailOf_NoAccount_NotFound(t *testing.T) {
	fake := dbtest.NewFake()
	checker := allowing() // denies everything: EmailOf must not ask it
	r := newRig(t, fake, withPerms(checker))
	_, err := r.svc.EmailOf(ctxT(t), newPersonID())
	wantKind(t, err, errs.NotFound)
	if checker.askedCount() != 0 {
		t.Error("EmailOf asked the permission checker; it is people's seam and checks none")
	}
	if len(r.rec.Recorded()) != 0 {
		t.Error("EmailOf recorded an audit entry; people audits the use case")
	}
}

func TestLinkAccount_ZeroPerson_Invalid(t *testing.T) {
	fake := dbtest.NewFake()
	r := newRig(t, fake, withPerms(allowing()))
	_, _, err := r.svc.LinkAccount(ctxT(t), mail(t, "ada.quill@example.org"), model.PersonID{})
	wantKind(t, err, errs.Invalid)
	if n := len(fake.Calls()); n != 0 {
		t.Errorf("%d statements for a zero ID", n)
	}
}

// F65
func TestLinkAccount_UnknownEmail_NotFound(t *testing.T) {
	fake := dbtest.NewFake()
	checker := allowing()
	r := newRig(t, fake, withPerms(checker))
	_, _, err := r.svc.LinkAccount(ctxT(t), mail(t, "nobody.here@example.org"), newPersonID())
	wantKind(t, err, errs.NotFound)
	found := false
	for _, fe := range validate.Fields(err) {
		if fe.Field == "email" && fe.Code == "unknown" {
			found = true
		}
	}
	if !found {
		t.Errorf("no email/unknown field error in %v", validate.Fields(err))
	}
	if strings.Contains(err.Error(), "nobody.here") {
		t.Error("the error echoes the address")
	}
	if checker.askedCount() != 0 {
		t.Error("LinkAccount asked the permission checker; it is people's seam and checks none")
	}
	if len(r.rec.Recorded()) != 0 || len(r.bus.Published()) != 0 {
		t.Error("a refused link recorded or published something")
	}
}
