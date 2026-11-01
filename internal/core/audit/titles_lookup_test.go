package audit_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/core/authz/authztest"
)

// The authz.Lookup is the TitleSource, and authztest.Fake stands in for it (boundaries.md
// step 4). Titles is required; with the fake as the source a person actor is recorded with
// the titles the fake holds for them.
var _ audit.TitleSource = (*authztest.Fake)(nil)

func TestNew_RecordsTitlesFromLookup(t *testing.T) {
	f := newFx(t)
	fake := authztest.NewFake()
	fake.SetTitles(alice, "Gate lead", "Medic")
	f.opts.Titles = fake
	f.readOnly() // spool, so the recorded line can be read back without a database
	log := f.build(t)
	rec, err := log.Scoped("identity")
	wantNoErr(t, err)
	e := loginEntry()
	e.Actor = person(alice)
	wantNoErr(t, rec.Record(ctxT(t), e))
	lines := f.spooled(t)
	if len(lines) != 1 {
		t.Fatalf("%d spooled entries, want 1", len(lines))
	}
	got := list(obj(lines[0]["actor"])["titles"])
	if len(got) != 2 || got[0] != "Gate lead" || got[1] != "Medic" {
		t.Errorf("titles = %v, want the titles the source holds", got)
	}
}
