//go:build integration

package volunteers_test

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/model"
)

// people.md: Item.Label must be a key in the module's catalogue, and a section's title key is
// module.<module>.section.<ident>.title (personpage.Section). This calls the real provider for a
// person with a department and a booked shift and checks what it actually emits, so joined,
// formatted or subpackage-built keys are covered too.
func TestSections_EmittedKeysExistInWebCatalogue(t *testing.T) {
	cat := loadCatalogue(t)
	r := newReal(t)
	s := r.scenario()
	p := r.volunteer("Dandelion")
	r.member(s.dept, p)
	r.book(p, s.sh.ID)

	got, err := r.svc.Sections(r.as(p), []model.PersonID{p})
	wantNoErr(t, err)
	secs := got[p]
	if len(secs) == 0 {
		t.Fatal("no sections for a person with volunteering data")
	}
	labels := 0
	for _, sec := range secs {
		if sec.Failed {
			t.Errorf("section %q failed", sec.Key)
		}
		mod, ident, ok := strings.Cut(sec.Key, ".")
		if !ok {
			t.Errorf("section key %q is not <module>.<ident>", sec.Key)
			continue
		}
		title := "module." + mod + ".section." + ident + ".title"
		if _, ok := cat[title]; !ok {
			t.Errorf("section title key %q is missing from the web catalogue", title)
		}
		for _, it := range sec.Items {
			if !strings.HasPrefix(it.Label, "module.") {
				continue // not a message key
			}
			labels++
			if _, ok := cat[it.Label]; !ok {
				t.Errorf("item label %q in section %q is missing from the web catalogue", it.Label, sec.Key)
			}
		}
	}
	if labels == 0 {
		t.Error("no item label besides the title was emitted; the check proves nothing")
	}
}
