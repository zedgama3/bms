package greeting_test

import (
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/lib/validate"
	"github.com/zedgama3/bms/internal/model"
	"github.com/zedgama3/bms/internal/modules/hello/greeting"
)

func valid() greeting.Greeting {
	return greeting.Greeting{
		ID:        model.NewID[greeting.ID](),
		Person:    model.PersonID{0x01, 0x02, 0x03},
		Text:      "hello",
		CreatedAt: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC),
	}
}

func TestGreeting_Validate_Table(t *testing.T) {
	cases := []struct {
		name      string
		mut       func(*greeting.Greeting)
		wantField string // "" means valid
	}{
		{"valid", func(*greeting.Greeting) {}, ""},
		{"one byte", func(g *greeting.Greeting) { g.Text = "a" }, ""},
		{"max bytes", func(g *greeting.Greeting) { g.Text = strings.Repeat("a", greeting.MaxBytes) }, ""},
		{"zero id", func(g *greeting.Greeting) { g.ID = greeting.ID{} }, "id"},
		{"zero person", func(g *greeting.Greeting) { g.Person = model.PersonID{} }, "person"},
		{"empty text", func(g *greeting.Greeting) { g.Text = "" }, "greeting"},
		{"over-long text", func(g *greeting.Greeting) { g.Text = strings.Repeat("a", greeting.MaxBytes+1) }, "greeting"},
		{"zero time", func(g *greeting.Greeting) { g.CreatedAt = time.Time{} }, "created_at"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := valid()
			tc.mut(&g)
			err := g.Validate()
			if tc.wantField == "" {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			if !errs.Is(err, errs.Invalid) {
				t.Fatalf("Validate = %v, want Invalid", err)
			}
			found := false
			for _, fe := range validate.Fields(err) {
				if fe.Field == tc.wantField {
					found = true
				}
			}
			if !found {
				t.Errorf("no field error on %q: %v", tc.wantField, validate.Fields(err))
			}
		})
	}
}

func TestGreeting_Validate_AllProblemsAtOnce(t *testing.T) {
	err := greeting.Greeting{}.Validate()
	if !errs.Is(err, errs.Invalid) {
		t.Fatalf("Validate = %v, want Invalid", err)
	}
	got := map[string]bool{}
	for _, fe := range validate.Fields(err) {
		got[fe.Field] = true
	}
	for _, f := range []string{"id", "person", "greeting", "created_at"} {
		if !got[f] {
			t.Errorf("missing field error on %q", f)
		}
	}
}
