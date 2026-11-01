package settings_test

import (
	"slices"
	"testing"

	"github.com/zedgama3/bms/internal/core/settings"
)

// Boundaries step 7: the public document, the stylesheet and the logo moved to regional, so
// settings serves exactly the four admin and terminology routes, none of them public.
func TestRoutes_AreExactlyTheFour(t *testing.T) {
	u := newUnit(t)
	var got []string
	for _, rt := range u.svc.Routes() {
		got = append(got, rt.Method+" "+normPattern(rt.Pattern))
		if rt.Public {
			t.Errorf("%s %s is public; settings has no public route any more", rt.Method, rt.Pattern)
		}
		if rt.Handler == nil {
			t.Errorf("%s %s has no handler", rt.Method, rt.Pattern)
		}
	}
	want := []string{"GET /", "GET /terms", "PUT /terms/{lang}", "PUT /values"}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("routes = %v, want exactly %v", got, want)
	}
}

// settings declares no area of its own and no longer adds regional: with no Areas it knows
// none, and the regional area is whatever app passes (regional.Settings).
func TestNew_NoAreas_DocumentIsEmpty(t *testing.T) {
	o := first(validOptions(t, newUnit(t).fake))
	o.Areas = nil
	svc, err := settings.New(o)
	wantNoErr(t, err)
	if _, err := svc.Scoped("regional"); err == nil {
		t.Error("Scoped(regional) succeeded with no regional area declared")
	}
}
