package regional_test

import (
	"testing"

	"github.com/zedgama3/bms/internal/core/regional"
	"github.com/zedgama3/bms/internal/lib/errs"
)

// B9: New fails Invalid for a nil Files or Settings (and any other dependency), and returns
// no service with the error. A valid set of options builds, and New does no I/O.
func TestNew_NilFiles_ReturnsInvalid(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*regional.Options)
	}{
		{"nil Files", func(o *regional.Options) { o.Files = nil }},
		{"nil Settings", func(o *regional.Options) { o.Settings = nil }},
		{"nil Audit", func(o *regional.Options) { o.Audit = nil }},
		{"nil Authz", func(o *regional.Options) { o.Authz = nil }},
		{"nil Actor", func(o *regional.Options) { o.Actor = nil }},
		{"nil Clock", func(o *regional.Options) { o.Clock = nil }},
		{"nil Logger", func(o *regional.Options) { o.Logger = nil }},
		{"nil Tracer", func(o *regional.Options) { o.Tracer = nil }},
		{"nil DB", func(o *regional.Options) { o.DB = nil }},
		{"nil Revision", func(o *regional.Options) { o.Revision = nil }},
		{"nil Terms", func(o *regional.Options) { o.Terms = nil }}, // required since A2 point 2
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o, _ := baseOptions(t)
			tc.mutate(&o)
			svc, err := regional.New(o)
			wantKind(t, err, errs.Invalid)
			if svc != nil {
				t.Error("New returned a service with its error")
			}
		})
	}
}

// Every missing dependency is named at once, not one per attempt.
func TestNew_FilesAndSettingsBothNil_ReturnsInvalid(t *testing.T) {
	o, _ := baseOptions(t)
	o.Files, o.Settings = nil, nil
	_, err := regional.New(o)
	wantKind(t, err, errs.Invalid)
}

func TestNew_ValidOptions_DoesNoIO(t *testing.T) {
	r := newRig(t)
	if r.svc == nil {
		t.Fatal("New returned nil without an error")
	}
	if n := len(r.fake.Calls()); n != 0 {
		t.Errorf("New ran %d statements; bms openapi and bms check build it against a DB that refuses them", n)
	}
	if n := len(r.rec.Recorded()); n != 0 {
		t.Errorf("New recorded %d audit entries", n)
	}
}

// Terms is required (boundaries A2, point 2): a public document with empty term maps is a
// different document, so a missing source is a wiring mistake.
func TestNew_NilTerms_ReturnsInvalid(t *testing.T) {
	o, _ := baseOptions(t)
	o.Terms = nil
	svc, err := regional.New(o)
	wantKind(t, err, errs.Invalid)
	if svc != nil {
		t.Error("New returned a service with its error")
	}
}
