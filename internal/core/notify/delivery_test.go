package notify_test

// Step 8 of plan/design/boundaries.md: the delivery rules that moved in from app. The
// language fallback and the four-form flattening of terms (formerly TestSiteSource_*), the
// "NotFound is none" rule of the profile reads (formerly
// TestLanguageAndNameSources_NotFoundIsEmpty) and the address order (formerly
// TestAddressSource_Table, in integration_delivery_test.go because the address is resolved
// when a row is sent). Every person and address is invented.

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/zedgama3/bms/internal/core/db/dbtest"
	"github.com/zedgama3/bms/internal/core/notify"
	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

// insertedText returns every string argument of the INSERT statements d recorded.
func insertedText(d *dbtest.Fake) string {
	var b strings.Builder
	for _, c := range d.Calls() {
		if !strings.Contains(strings.ToLower(c.SQL), "insert") {
			continue
		}
		for _, a := range c.Args {
			if s, ok := a.(string); ok {
				b.WriteString(s)
				b.WriteString("\n")
			}
			if s, ok := a.(*string); ok && s != nil {
				b.WriteString(*s)
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}

func termsTemplate() *fstest.MapFS {
	return &fstest.MapFS{
		"words.en.tmpl": {Data: []byte("Subject: {{term \"department\"}}|{{term \"department_plural\"}}|" +
			"{{term \"department_title\"}}|{{term \"department_plural_title\"}}\n\nbody\n")},
	}
}

func departmentTerms() map[string]settings.Forms {
	return map[string]settings.Forms{"department": {
		Singular: "department", Plural: "departments", SingularTitle: "Department", PluralTitle: "Departments"}}
}

// TestSiteSource_Table (moved from app): the language the terms are asked in.
func TestSiteSource_Table(t *testing.T) {
	for _, tc := range []struct {
		name, regionalLang, person, wantLang string
	}{
		{"empty is the regional default", "en", "", "en"},
		{"empty is the regional default, whatever it is", "es", "", "es"},
		{"shipped language", "en", "es", "es"},
		{"regional tag uses its primary subtag", "en", "es-MX", "es"},
		{"unknown language falls back to the regional default", "en", "xx", "en"},
		{"unknown regional tag falls back to the regional default", "es", "xx-YY", "es"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := dbtest.NewFake()
			r := newRig(t, d)
			r.site.profile.Language = tc.regionalLang
			p := r.person("a@example.org")
			if tc.person != "" {
				r.langs.m[p] = tc.person
			}
			wantNoErr(t, r.vol.Send(ctxT(t), greet(p)))
			asked := r.site.languagesAsked()
			if len(asked) == 0 {
				t.Fatal("the terms were never asked for")
			}
			for _, got := range asked {
				if got != tc.wantLang {
					t.Errorf("terms asked in %q, want %q (all: %v)", got, tc.wantLang, asked)
				}
			}
		})
	}
}

func TestSiteSource_FlattensEveryTermIntoFourKeys(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	r.site.terms = departmentTerms()
	wantNoErr(t, r.svc.Register("words", termsTemplate()))
	n := r.scoped("words")
	p := r.person("a@example.org")
	wantNoErr(t, n.Send(ctxT(t), notify.Message{Template: "words.words", To: []model.PersonID{p}}))
	got := insertedText(d)
	if !strings.Contains(got, "department|departments|Department|Departments") {
		t.Errorf("subject lacks the four forms under key, key_plural, key_title, key_plural_title:\n%s", got)
	}
}

func TestSiteSource_RegionalFails_KeepsTheKind(t *testing.T) {
	for _, k := range []errs.Kind{errs.Unavailable, errs.Timeout, errs.Canceled} {
		t.Run(k.String(), func(t *testing.T) {
			d := dbtest.NewFake()
			r := newRig(t, d)
			r.site.fail(errs.E("test.regional", k, errors.New("down")))
			wantKind(t, r.vol.Send(ctxT(t), greet(r.person("a@example.org"))), k)
			if statements(d, "insert") != 0 {
				t.Fatal("a row was inserted although the regional could not be read")
			}
		})
	}
}

func TestSiteSource_RendersTheRegionalProfile(t *testing.T) {
	d := dbtest.NewFake()
	r := newRig(t, d)
	r.site.profile = regional.Profile{Name: "Quill Burn", PublicURL: "https://quill.example.org", Language: "en"}
	wantNoErr(t, r.vol.Send(ctxT(t), greet(r.person("a@example.org"))))
	got := insertedText(d)
	for _, w := range []string{"Quill Burn", "https://quill.example.org/volunteers/shifts/1"} {
		if !strings.Contains(got, w) {
			t.Errorf("rendered text lacks %q:\n%s", w, got)
		}
	}
}

// TestLanguageAndNameSources_NotFoundIsEmpty (moved from app): NotFound from Profiles is no
// name and no language; any other kind is not hidden.
func TestLanguageAndNameSources_NotFoundIsEmpty(t *testing.T) {
	notFound := errs.E("test", errs.NotFound, errors.New("no profile"))
	for _, seam := range []string{"language", "name"} {
		t.Run(seam+" NotFound sends with the defaults", func(t *testing.T) {
			d := dbtest.NewFake()
			r := newRig(t, d)
			p := r.person("a@example.org")
			if seam == "language" {
				r.langs.fail(notFound)
			} else {
				r.names.fail(notFound)
			}
			wantNoErr(t, r.vol.Send(ctxT(t), greet(p)))
			if statements(d, "insert") == 0 {
				t.Fatal("no row was inserted")
			}
			// English, the regional's default, and no playa name in the subject.
			if got := r.site.languagesAsked(); !slices.Contains(got, "en") || slices.ContainsFunc(got, func(l string) bool { return l != "en" }) {
				t.Errorf("terms asked in %v, want only the regional default", got)
			}
		})
	}
	t.Run("saved language and name are used", func(t *testing.T) {
		d := dbtest.NewFake()
		r := newRig(t, d)
		p := r.person("a@example.org")
		r.langs.m[p], r.names.m[p] = "es", "Wren"
		wantNoErr(t, r.vol.Send(ctxT(t), greet(p)))
		if got := insertedText(d); !strings.Contains(got, "Turno para Wren") {
			t.Errorf("row text lacks the Spanish subject for the playa name:\n%s", got)
		}
	})
	for _, k := range []errs.Kind{errs.Unavailable, errs.Timeout} {
		for _, seam := range []string{"language", "name"} {
			t.Run(seam+" "+k.String()+" is not hidden", func(t *testing.T) {
				d := dbtest.NewFake()
				r := newRig(t, d)
				p := r.person("a@example.org")
				e := errs.E("test", k, errors.New("down"))
				if seam == "language" {
					r.langs.fail(e)
				} else {
					r.names.fail(e)
				}
				wantKind(t, r.vol.Send(ctxT(t), greet(p)), k)
				if statements(d, "insert") != 0 {
					t.Fatal("a row was inserted although the profile read failed")
				}
			})
		}
	}
}

// New does not read any of the new seams (no I/O at build).
func TestNew_NewSeams_AreNotReadAtBuild(t *testing.T) {
	d := dbtest.NewFake()
	r, o := newRigOptions(t, d)
	_, err := notify.New(o)
	wantNoErr(t, err)
	if n := seamCalls(r) + r.conts.calls.Load(); n != 0 {
		t.Fatalf("New called a seam %d times", n)
	}
	if n := len(d.Calls()); n != 0 {
		t.Fatalf("New ran %d statements", n)
	}
}
