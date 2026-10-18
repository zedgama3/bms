package personpage_test

import (
	"context"
	"go/build"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/contracts/personpage"
	"github.com/zedgama3/bms/internal/model"
)

func TestPoint_IsThePeoplePersonPage(t *testing.T) {
	if personpage.Point != "people.person-page" {
		t.Errorf("Point = %q", personpage.Point)
	}
}

type provider struct{ got []model.PersonID }

func (p *provider) Sections(_ context.Context, ids []model.PersonID) (map[model.PersonID][]personpage.Section, error) {
	p.got = ids
	out := map[model.PersonID][]personpage.Section{}
	for _, id := range ids[:1] {
		out[id] = []personpage.Section{{
			Key:   "hello.greeting",
			Items: []personpage.Item{{Label: "module.hello.item.count", Value: "3", Href: "/hello"}},
		}}
	}
	return out, nil
}

var _ personpage.Provider = (*provider)(nil)

func TestSection_ShapeAndZeroValues(t *testing.T) {
	var s personpage.Section
	if s.Failed || len(s.Items) != 0 || s.Key != "" {
		t.Errorf("zero Section = %+v", s)
	}
	p := &provider{}
	id := model.NewID[model.PersonID]()
	got, err := p.Sections(context.Background(), []model.PersonID{id})
	if err != nil {
		t.Fatal(err)
	}
	sec := got[id]
	if len(sec) != 1 || sec[0].Key != "hello.greeting" || sec[0].Items[0].Href != "/hello" || sec[0].Failed {
		t.Errorf("sections = %+v", sec)
	}
}

// The contract imports model only (and the standard library), so a module never imports
// two packages called people.
func TestImports_ModelOnly(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range pkg.Imports {
		if !strings.Contains(imp, ".") { // standard library
			continue
		}
		if imp != "github.com/zedgama3/bms/internal/model" {
			t.Errorf("personpage imports %s, want model only", imp)
		}
	}
}
