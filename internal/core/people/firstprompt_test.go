package people_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
)

// F102 (P-W1): a nil Session is Invalid, field people.session, from Validate and from New.
func TestOptions_NilSession_ReturnsInvalid(t *testing.T) {
	o, _ := validOptions(t, dbtest.NewFake())
	o.Session = nil
	wantField(t, o.Validate(), "people.session", "")
	svc, err := people.New(o)
	wantField(t, err, "people.session", "")
	if svc != nil {
		t.Error("New returned a service with its error")
	}
	// Only the session is wrong: no other field is reported.
	if hasField(o.Validate(), "people.db", "") || hasField(o.Validate(), "people.actor", "") {
		t.Error("another field was reported")
	}
	// The wiring app uses is accepted.
	o.Session = identity.SessionFrom
	wantNoErr(t, o.Validate())
}
