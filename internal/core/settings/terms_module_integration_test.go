//go:build integration

package settings_test

import (
	"slices"
	"testing"

	"github.com/zedgama3/bms/internal/core/settings"
)

func volunteersTerms() settings.Area {
	return termArea("volunteers", "shift", "position", "qualification")
}

func esArea() settings.Area {
	a := termArea("camps", "tent")
	a.Terms[0].Defaults["es"] = settings.Forms{Singular: "carpa", Plural: "carpas", SingularTitle: "Carpa", PluralTitle: "Carpas"}
	return a
}

// F29c
func TestTermsFor_ModuleTerm_DefaultsAndOverride(t *testing.T) {
	r := newIRig(t, withAreas(volunteersTerms(), esArea()))
	ctx := ctxT(t)

	en, err := r.svc.TermsFor(ctx, "en")
	wantNoErr(t, err)
	if en["shift"] != forms("shift") {
		t.Errorf("en shift = %+v", en["shift"])
	}
	if want := len(settings.CoreTerms) + 4; len(en) != want {
		t.Errorf("%d terms, want %d (core plus declared)", len(en), want)
	}
	if want := (settings.Forms{Singular: "lead", Plural: "leads", SingularTitle: "Lead", PluralTitle: "Leads"}); en["lead"] != want {
		t.Errorf("en lead = %+v", en["lead"])
	}

	// A language without an entry uses the en defaults; one with an entry uses its own.
	es := first(r.svc.TermsFor(ctx, "es"))
	if es["shift"] != en["shift"] {
		t.Errorf("es shift = %+v, want en's", es["shift"])
	}
	if es["tent"].Singular != "carpa" || en["tent"].Singular != "tent" {
		t.Errorf("tent: es %+v, en %+v", es["tent"], en["tent"])
	}

	// An override wins for its language only; Public flattens it.
	_, err = r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"shift": {Singular: str("duty"), PluralTitle: str("Duties")}}))
	wantNoErr(t, err)
	en = first(r.svc.TermsFor(ctx, "en"))
	if en["shift"].Singular != "duty" || en["shift"].PluralTitle != "Duties" || en["shift"].Plural != "shifts" {
		t.Errorf("en shift after override = %+v", en["shift"])
	}
	if first(r.svc.TermsFor(ctx, "es"))["shift"].Singular != "shift" {
		t.Error("an en override leaked into es")
	}
	// A reset restores the default.
	_, err = r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"shift": {Reset: []string{"singular", "plural_title"}}}))
	wantNoErr(t, err)
	if got := first(r.svc.TermsFor(ctx, "en"))["shift"]; got != forms("shift") {
		t.Errorf("after reset = %+v", got)
	}
}

func TestSetTerms_LeadIsACoreTerm(t *testing.T) {
	r := newIRig(t)
	_, err := r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"lead": {Singular: str("captain"), Plural: str("captains")}}))
	wantNoErr(t, err)
	got := first(r.svc.TermsFor(ctxT(t), "en"))["lead"]
	if got.Singular != "captain" || got.Plural != "captains" || got.SingularTitle != "Lead" {
		t.Errorf("lead = %+v", got)
	}
}

func TestSetTerms_ModuleTerm_AuditsTheChange(t *testing.T) {
	r := newIRig(t, withAreas(volunteersTerms()))
	_, err := r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"shift": {Singular: str("duty")}}))
	wantNoErr(t, err)
	if got := actionsOf(r.rec.Recorded()); !slices.Contains(got, "settings.term.changed") {
		t.Errorf("audit actions = %v", got)
	}
	// A no-op write adds no entry.
	n := len(r.rec.Recorded())
	_, err = r.svc.SetTerms(r.actx(), "en", termsOf(map[string]settings.FormChanges{"shift": {Singular: str("duty")}}))
	wantNoErr(t, err)
	if len(r.rec.Recorded()) != n {
		t.Error("an unchanged module term was audited again")
	}
}

// F29d
func TestTerms_ListsDeclaredModuleTermsWithArea(t *testing.T) {
	r := newIRig(t, withAreas(termArea("volunteers", "shift", "position")))
	doc, err := r.svc.Terms(r.actx())
	wantNoErr(t, err)
	for _, lang := range settings.Languages {
		terms := doc.Terms[lang]
		for _, core := range settings.CoreTerms {
			td, ok := terms[core]
			if !ok {
				t.Errorf("%s: no core term %s", lang, core)
			} else if td.Area != "core" {
				t.Errorf("%s/%s Area = %q, want core", lang, core, td.Area)
			}
		}
		for _, mod := range []string{"shift", "position"} {
			td, ok := terms[mod]
			if !ok {
				t.Errorf("%s: no module term %s", lang, mod)
				continue
			}
			if td.Area != "volunteers" {
				t.Errorf("%s/%s Area = %q, want volunteers", lang, mod, td.Area)
			}
			if td.Default != forms(mod) {
				t.Errorf("%s/%s default = %+v", lang, mod, td.Default)
			}
		}
		if _, ok := terms["qualification"]; ok {
			t.Errorf("%s: an undeclared term is listed", lang)
		}
		if want := len(settings.CoreTerms) + 2; len(terms) != want {
			t.Errorf("%s: %d terms, want %d", lang, len(terms), want)
		}
	}
}

func TestTerms_NoModuleTerms_OnlyCoreWithCoreArea(t *testing.T) {
	r := newIRig(t)
	doc := first(r.svc.Terms(r.actx()))
	if len(doc.Terms["en"]) != len(settings.CoreTerms) {
		t.Errorf("%d terms, want %d", len(doc.Terms["en"]), len(settings.CoreTerms))
	}
	if doc.Terms["en"]["lead"].Area != "core" {
		t.Errorf("lead Area = %q", doc.Terms["en"]["lead"].Area)
	}
}

// F29d: a row for a term no area declares (a disabled module) is ignored and kept.
func TestLoad_UndeclaredTermRow_IgnoredAndKept(t *testing.T) {
	with := newIRig(t, withAreas(volunteersTerms()))
	_, err := with.svc.SetTerms(with.actx(), "en", termsOf(map[string]settings.FormChanges{"shift": {Singular: str("duty")}}))
	wantNoErr(t, err)

	without := newIRigOn(t, with.pool, with.pool)
	ctx := ctxT(t)
	en, err := without.svc.TermsFor(ctx, "en")
	wantNoErr(t, err)
	if _, ok := en["shift"]; ok {
		t.Error("an undeclared term is returned by TermsFor")
	}
	doc := first(without.svc.Terms(without.actx()))
	if _, ok := doc.Terms["en"]["shift"]; ok {
		t.Error("an undeclared term is listed by Terms")
	}
	_, err = without.svc.SetTerms(without.actx(), "en", termsOf(map[string]settings.FormChanges{"shift": {Singular: str("x")}}))
	wantField(t, err, "terms.shift", "unknown")

	// A write elsewhere does not drop the stored row ...
	_, err = without.svc.SetTerms(without.actx(), "en", termsOf(map[string]settings.FormChanges{"department": teamForms}))
	wantNoErr(t, err)
	var n int
	if err := with.pool.QueryRow(ctx, "SELECT count(*) FROM core.terminology WHERE term = 'shift'").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("%d stored rows for the undeclared term, want 1 (kept)", n)
	}

	// ... and the override returns when the module is enabled again.
	again := newIRigOn(t, with.pool, with.pool, withAreas(volunteersTerms()))
	got := first(again.svc.TermsFor(ctxT(t), "en"))["shift"]
	if got.Singular != "duty" || got.Plural != "shifts" {
		t.Errorf("shift after re-enabling = %+v", got)
	}
}
