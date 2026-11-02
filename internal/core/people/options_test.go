package people_test

import (
	"context"
	"testing"

	"github.com/zedgama3/bms/internal/contracts/personpage"
	"github.com/zedgama3/bms/internal/core/db"
	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/identity"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

var (
	_ people.Directory    = (*people.Service)(nil)
	_ identity.NameSource = (*people.Service)(nil)
)

// F1
func TestNew_NilDependency_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		field string
		mod   func(*people.Options)
	}{
		{"people.db", func(o *people.Options) { o.DB = nil }},
		{"people.clock", func(o *people.Options) { o.Clock = nil }},
		{"people.logger", func(o *people.Options) { o.Logger = nil }},
		{"people.tracer", func(o *people.Options) { o.Tracer = nil }},
		{"people.audit", func(o *people.Options) { o.Audit = nil }},
		{"people.signals", func(o *people.Options) { o.Signals = nil }},
		{"people.authz", func(o *people.Options) { o.Authz = nil }},
		{"people.actor", func(o *people.Options) { o.Actor = nil }},
		{"people.session", func(o *people.Options) { o.Session = nil }},
		{"people.accounts", func(o *people.Options) { o.Accounts = nil }},
		{"people.settings", func(o *people.Options) { o.Settings = nil }},
		{"people.lookup", func(o *people.Options) { o.Lookup = nil }},
	}
	for _, tc := range tests {
		t.Run(tc.field, func(t *testing.T) {
			o, _ := validOptions(t, dbtest.NewFake())
			tc.mod(&o)
			wantField(t, o.Validate(), tc.field, "")
			svc, err := people.New(o)
			wantField(t, err, tc.field, "")
			if svc != nil {
				t.Error("New returned a service with its error")
			}
		})
	}
}

func TestOptions_Validate_ReportsEveryProblemAtOnce(t *testing.T) {
	err := people.Options{}.Validate()
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{
		"people.db", "people.clock", "people.logger", "people.tracer", "people.audit", "people.signals",
		"people.authz", "people.actor", "people.accounts", "people.session", "people.settings", "people.lookup",
	} {
		if !hasField(err, f, "") {
			t.Errorf("no field error for %s", f)
		}
	}
}

func TestNew_OptionalMeter_MayBeNil(t *testing.T) {
	o, _ := validOptions(t, dbtest.NewFake())
	o.Meter = nil
	wantNoErr(t, o.Validate())
	if _, err := people.New(o); err != nil {
		t.Fatalf("New without a Meter: %v", err)
	}
}

func TestNew_WorksAgainstNoDatabase_AndSubscribes(t *testing.T) {
	r := newRig(t, db.None())
	got := r.bus.Handlers()
	if len(got) != 1 || got[0] != identity.SignalAccountCreated {
		t.Fatalf("subscriptions = %v, want only %s", got, identity.SignalAccountCreated)
	}
}

// F2
func TestLink_Twice_ReturnsInternal(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	wantNoErr(t, r.svc.Link(nil, nil))
	wantKind(t, r.svc.Link(nil, nil), errs.Internal)
}

func TestLink_NilProvider_ReturnsInternal(t *testing.T) {
	tests := []struct {
		name     string
		sections []people.SectionProvider
	}{
		{"nil provider", []people.SectionProvider{{Module: "hello", Provider: nil}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, dbtest.NewFake())
			wantKind(t, r.svc.Link(tc.sections, nil), errs.Internal)
		})
	}
}

func TestLink_GoodProviders_Succeeds(t *testing.T) {
	r := newRig(t, dbtest.NewFake())
	prov := providerFunc(func(context.Context, []model.PersonID) (map[model.PersonID][]personpage.Section, error) {
		return nil, nil
	})
	wantNoErr(t, r.svc.Link([]people.SectionProvider{{Module: "hello", Provider: prov}}, nil))
}
