//go:build integration

package people_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"maps"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/contracts/personpage"
	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/core/people/peopletest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// The rule: no personal data in logs, spans or metrics, nor in the Reason or Fields of an
// audit entry. This test drives every use case that handles personal data with marker
// values and then looks for the markers everywhere the service wrote.
func TestNoPersonalDataInLogsSpansMetricsOrAuditEntries(t *testing.T) {
	r := newIRig(t, false)
	failing := providerFunc(func(context.Context, []model.PersonID) (map[model.PersonID][]personpage.Section, error) {
		return nil, errs.E("camps.Sections", errs.Unavailable, errors.New("camp service down"))
	})
	wantNoErr(t, r.svc.Link([]people.SectionProvider{{Module: "camps", Provider: failing}}, nil))

	_, actx := r.admin(permManage, permDirectory, permContacts)

	// Create with every personal field, then change and read it.
	prof, err := r.svc.Create(actx, people.ProfileInput{
		PlayaName: str(mPlaya), Pronouns: str("they/them"), LegalName: str(mLegal), Phone: str(mPhone), ContactEmail: str(mContactEmail),
	})
	wantNoErr(t, err)
	p := prof.Person.ID
	_, err = r.svc.AddContact(actx, p, people.ContactInput{Name: mECName, Phone: mECPhone, Relationship: mRelationship})
	wantNoErr(t, err)
	_, err = r.svc.Update(actx, p, people.ProfileInput{Phone: str("+1 555 0178")})
	wantNoErr(t, err)
	_, err = r.svc.Get(actx, p)
	wantNoErr(t, err)
	for _, q := range []string{"zephyrine", "0178", "marker.contact", "glimmer"} {
		_, err = r.svc.Search(actx, people.SearchQuery{Q: q})
		wantNoErr(t, err)
	}
	_, err = r.svc.SetConsent(actx, p, "volunteers.share-contact", true)
	wantNoErr(t, err)

	// The person's own side.
	_, err = r.svc.Me(asPerson(ctxT(t), p))
	wantNoErr(t, err)
	_, err = r.svc.UpdateMe(asPerson(ctxT(t), p), people.ProfileInput{Pronouns: str("she/her")})
	wantNoErr(t, err)

	// Failure paths whose errors might echo input.
	_, err = r.svc.UpdateMe(asPerson(ctxT(t), p), people.ProfileInput{LegalName: str(mLegal + strings.Repeat("x", 300)), Phone: str(mPhone + "zz")})
	wantKind(t, err, errs.Invalid)
	if strings.Contains(err.Error(), mLegal) || strings.Contains(err.Error(), mPhone) {
		t.Error("a validation error echoes personal input")
	}
	_, err = r.svc.LinkAccount(actx, p, model.Email("x"+mContactEmail))
	if err == nil {
		t.Error("linking an unknown address succeeded")
	} else if strings.Contains(err.Error(), "marker.contact") {
		t.Error("a link error echoes the address")
	}

	// Link and delete (the deletion is datarights' use case; people's share of it is its
	// PeopleSource.DeletePerson, run here in a transaction as datarights runs it).
	r.accts.addAccount(newPerson(), mail(t, "marker.link@example.org"), false)
	r.deletePerson(p)

	r.noLeak(allMarkers()...)
	r.noLeak("marker.link", "+1 555 0178")

	// Metrics take tokens only: no attribute is an ID.
	for _, m := range r.cap.Measurements() {
		if !strings.HasPrefix(m.Instrument, "bms.people.") {
			continue
		}
		for k, v := range m.Attrs {
			if _, err := model.ParseID[model.PersonID](v); err == nil {
				t.Errorf("metric %s attribute %s=%s is an ID", m.Instrument, k, v)
			}
			if k != "outcome" && k != "module" {
				t.Errorf("metric %s has unexpected attribute %q", m.Instrument, k)
			}
		}
	}
	// The failing provider is logged by module name, at warn or above.
	warned := false
	for _, rec := range r.cap.Records() {
		if rec.Level >= slog.LevelWarn && strings.Contains(rec.Message+attrsText(rec.Attrs), "camps") {
			warned = true
		}
	}
	if !warned {
		t.Error("no warn log line names the failing module")
	}
	// A span per use case, scoped to people.
	spans := 0
	for _, s := range r.cap.Spans() {
		if strings.HasPrefix(s.Name, "people.") {
			spans++
		}
	}
	if spans == 0 {
		t.Error("no span named people.<use-case>")
	}
	if v := r.cap.Violations(); len(v) != 0 {
		t.Errorf("telemetry policy violations: %v", v)
	}
}

func attrsText(m map[string]string) string {
	var b strings.Builder
	for k, v := range m {
		b.WriteString(k + "=" + v + " ")
	}
	return b.String()
}

func TestPersonLogValue_InsideServiceLogs_IsIDOnly(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.Info("x", slog.Any("person", people.Person{ID: newPerson(), PlayaName: mPlaya, LegalName: mLegal}))
	if strings.Contains(buf.String(), mPlaya) || strings.Contains(buf.String(), mLegal) {
		t.Errorf("log line leaks: %s", buf.String())
	}
}

// ---- peopletest.Fake against the real service -------------------------------------------

type dirResult struct {
	names map[model.PersonID]string
	ok    bool
	kind  errs.Kind
	err   bool
}

func TestFake_MatchesService(t *testing.T) {
	r := newIRig(t, false)
	known := r.insertPerson(seed{Playa: "Dusty Ember"})
	unnamed := r.insertPerson(seed{})
	unknown := newPerson()
	const grantedP people.Purpose = "volunteers.share-contact"
	const revokedP people.Purpose = "volunteers.photo-credit"
	ctx := asPerson(ctxT(t), known)
	_, err := r.svc.SetConsent(ctx, known, grantedP, true)
	wantNoErr(t, err)
	_, err = r.svc.SetConsent(ctx, known, revokedP, true)
	wantNoErr(t, err)
	_, err = r.svc.SetConsent(ctx, known, revokedP, false)
	wantNoErr(t, err)

	fake := peopletest.NewFake()
	fake.Add(known, "Dusty Ember")
	fake.Add(unnamed, "")
	fake.Consent(known, grantedP, true)
	fake.Consent(known, revokedP, false)

	tooMany := make([]model.PersonID, people.MaxNames+1)
	for i := range tooMany {
		tooMany[i] = newPerson()
	}
	names := func(ids ...model.PersonID) func(people.Directory) dirResult {
		return func(d people.Directory) dirResult {
			m, err := d.NamesOf(ctxT(t), ids)
			return dirResult{names: m, err: err != nil, kind: errs.KindOf(err)}
		}
	}
	consent := func(p model.PersonID, pur people.Purpose) func(people.Directory) dirResult {
		return func(d people.Directory) dirResult {
			ok, err := d.ConsentOf(ctxT(t), p, pur)
			return dirResult{ok: ok, err: err != nil, kind: errs.KindOf(err)}
		}
	}
	cases := map[string]func(people.Directory) dirResult{
		"known and unnamed":     names(known, unnamed),
		"unknown omitted":       names(known, unknown),
		"only unknown":          names(unknown),
		"empty":                 names(),
		"too many":              names(tooMany...),
		"exactly max":           names(tooMany[:people.MaxNames]...),
		"consent granted":       consent(known, grantedP),
		"consent revoked":       consent(known, revokedP),
		"consent never asked":   consent(known, "volunteers.something-else"),
		"consent unknown":       consent(unknown, grantedP),
		"consent bad purpose":   consent(known, "nodot"),
		"consent empty purpose": consent(known, ""),
		"consent zero person":   consent(model.PersonID{}, grantedP),
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			got, want := run(fake), run(r.svc)
			if got.err != want.err || got.kind != want.kind {
				t.Fatalf("fake: err=%v kind=%v; service: err=%v kind=%v", got.err, got.kind, want.err, want.kind)
			}
			if got.ok != want.ok {
				t.Errorf("fake says %v, service says %v", got.ok, want.ok)
			}
			if len(got.names) != len(want.names) || !maps.Equal(got.names, want.names) {
				t.Errorf("fake names %v, service names %v", got.names, want.names)
			}
		})
	}
}
