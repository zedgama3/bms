package hello_test

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/core/privacy/privacytest"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/modules/hello"
	"github.com/zedgama3/bms/internal/modules/hello/greeting"
)

// fieldErrorPaths lists the validate field paths carried by err.
func fieldErrorPaths(err error) map[string]bool {
	out := map[string]bool{}
	for _, fe := range validate.Fields(err) {
		out[fe.Field] = true
	}
	return out
}

// TestViews_Tagged: the four wire types are listed in the specified order, tagged, and
// hello declares no groups (every field is public or internal) and so no visibility_*
// setting (module-hello A1).
func TestViews_Tagged(t *testing.T) {
	m := hello.Manifest
	want := []reflect.Type{
		reflect.TypeOf(hello.PublicGreeting{}),
		reflect.TypeOf(hello.Banner{}),
		reflect.TypeOf(hello.Greeting{}),
		reflect.TypeOf(greeting.Greeting{}),
	}
	if len(m.Views) != len(want) {
		t.Fatalf("len(Views) = %d, want %d", len(m.Views), len(want))
	}
	for i, v := range m.Views {
		got := reflect.TypeOf(v)
		for got.Kind() == reflect.Pointer {
			got = got.Elem()
		}
		if got != want[i] {
			t.Errorf("Views[%d] = %v, want %v", i, got, want[i])
		}
	}
	if err := privacy.Check(m.Views, nil); err != nil {
		t.Fatalf("privacy.Check(Views, nil): %v", err)
	}
	if len(m.Groups) != 0 {
		t.Errorf("Groups = %v, want none", m.Groups)
	}
	for _, s := range m.Settings {
		if strings.HasPrefix(s.Key, "visibility_") {
			t.Errorf("setting %q: hello declares no groups, so no visibility setting", s.Key)
		}
	}
}

// TestViews_LevelsAsSpecified: PublicGreeting and Banner fields are public (the route has
// no actor); hello.Greeting and greeting.Greeting wire fields are internal; none is grouped.
func TestViews_LevelsAsSpecified(t *testing.T) {
	tests := []struct {
		typ   reflect.Type
		level string
	}{
		{reflect.TypeOf(hello.PublicGreeting{}), "public"},
		{reflect.TypeOf(hello.Banner{}), "public"},
		{reflect.TypeOf(hello.Greeting{}), "internal"},
		{reflect.TypeOf(greeting.Greeting{}), "internal"},
	}
	for _, tc := range tests {
		t.Run(tc.typ.Name(), func(t *testing.T) {
			wire := 0
			for i := 0; i < tc.typ.NumField(); i++ {
				f := tc.typ.Field(i)
				if !f.IsExported() || f.Tag.Get("json") == "-" {
					continue
				}
				wire++
				if got := f.Tag.Get("privacy"); got != tc.level {
					t.Errorf("%s privacy tag = %q, want %q", f.Name, got, tc.level)
				}
			}
			if wire == 0 {
				t.Error("no wire fields found")
			}
		})
	}
}

// TestViews_NonPublicFieldsOmittable (A2): every non-public wire field carries the option its
// kind needs: omitzero on the struct, array and time.Time fields (id, created_at),
// omitempty on the scalars (greeting, mine, seen); public fields need none.
func TestViews_NonPublicFieldsOmittable(t *testing.T) {
	want := map[string]string{
		"hello.Greeting.greeting":      "omitempty",
		"hello.Greeting.mine":          "omitempty",
		"hello.Greeting.seen":          "omitempty",
		"greeting.Greeting.id":         "omitzero",
		"greeting.Greeting.greeting":   "omitempty",
		"greeting.Greeting.created_at": "omitzero",
	}
	got := map[string]string{}
	for _, v := range hello.Manifest.Views {
		typ := reflect.TypeOf(v)
		for typ.Kind() == reflect.Pointer {
			typ = typ.Elem()
		}
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			if !f.IsExported() || f.Tag.Get("json") == "-" || f.Tag.Get("privacy") == "public" {
				continue
			}
			name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
			got[typ.PkgPath()[strings.LastIndex(typ.PkgPath(), "/")+1:]+"."+typ.Name()+"."+name] = opts
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("non-public field options = %v, want %v", got, want)
	}
}

// F37: a wire field added to a listed type without a tag fails Manifest.Validate with
// Invalid at views[i].<Field>, i being its place in Views.
func TestManifest_UntaggedViewField_ReturnsInvalid(t *testing.T) {
	type taggedPlus struct {
		ID        string `json:"id,omitempty" privacy:"internal"`
		Text      string `json:"greeting,omitempty" privacy:"internal"`
		CreatedAt string `json:"created_at,omitempty" privacy:"internal"`
		Extra     string `json:"extra"` // no tag
	}
	for i := range hello.Manifest.Views {
		t.Run(reflect.TypeOf(hello.Manifest.Views[i]).Name(), func(t *testing.T) {
			m := hello.Manifest
			m.Views = append([]any(nil), hello.Manifest.Views...)
			m.Views[i] = taggedPlus{}
			err := m.Validate()
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("kind = %v, want Invalid (%v)", errs.KindOf(err), err)
			}
			path := "views[" + strconv.Itoa(i) + "].Extra"
			if !fieldErrorPaths(err)[path] {
				t.Errorf("no field error at %s; got %v", path, fieldErrorPaths(err))
			}
		})
	}
	// The real manifest is fine.
	if err := hello.Manifest.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

// F38: the public route answers public fields only, with the keys the spec lists, and
// nothing that belongs to a signed-in or stored record.
func TestPublicGreeting_ZeroViewer_PublicFieldsOnly(t *testing.T) {
	f := newFx(t, cfg{seams: hello.Seams{Actor: noActor, Allow: hello.DenyAll{}}})
	f.start(t)
	rec := do(t, bareMux(t, f.mod), "GET", "/api/hello/public-greeting")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	body := rec.Body.Bytes()
	privacytest.AssertPresent(t, body, "greeting", "banners")
	var parsed struct {
		Banners []map[string]any `json:"banners"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.Banners) == 0 {
		t.Fatalf("no banners in %s", body)
	}
	for _, b := range parsed.Banners {
		for _, k := range []string{"module", "key"} {
			if _, ok := b[k]; !ok {
				t.Errorf("banner lacks %q: %v", k, b)
			}
		}
	}
	// Fields of the other wire types must never appear on the public route.
	privacytest.AssertHidden(t, body, "mine", "seen", "id", "created_at", "person")
}

// A signed-in reader keeps hello's internal fields, and the person ID stays off the wire.
func TestGreeting_SignedIn_KeepsInternalFieldsOnly(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	seedRows(t, f.store, alice, 2)
	rec := do(t, bareMux(t, f.mod), "GET", "/api/hello/greeting")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	privacytest.AssertPresent(t, rec.Body.Bytes(), "greeting", "mine")
	privacytest.AssertHidden(t, rec.Body.Bytes(), "person", "seen") // seen is 0: omitted (A2)
}

// A2: a signed-in reader with no rows of their own gets a body without `mine` (and without
// `seen`, nothing having been delivered); the screen reads absent as 0.
func TestGreeting_SignedIn_ZeroMineIsAbsent(t *testing.T) {
	f := newFx(t, cfg{})
	f.start(t)
	rec := do(t, bareMux(t, f.mod), "GET", "/api/hello/greeting")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	privacytest.AssertPresent(t, rec.Body.Bytes(), "greeting")
	privacytest.AssertHidden(t, rec.Body.Bytes(), "mine", "seen", "person")
}
