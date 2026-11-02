package people_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/people"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestConstants_MatchTheSpec(t *testing.T) {
	ints := []struct {
		name      string
		got, want int
	}{
		{"MaxPlayaNameBytes", people.MaxPlayaNameBytes, 100},
		{"MaxPronounsBytes", people.MaxPronounsBytes, 40},
		{"MaxLegalNameBytes", people.MaxLegalNameBytes, 200},
		{"MaxPhoneBytes", people.MaxPhoneBytes, 40},
		{"MaxRelationshipBytes", people.MaxRelationshipBytes, 60},
		{"MaxLanguageBytes", people.MaxLanguageBytes, 35},
		{"MaxNoteBytes", people.MaxNoteBytes, 500},
		{"MaxEmergencyContacts", people.MaxEmergencyContacts, 10},
		{"MinSearchBytes", people.MinSearchBytes, 2},
		{"MaxSearchBytes", people.MaxSearchBytes, 100},
		{"MaxSearchResults", people.MaxSearchResults, 50},
		{"DefaultSearchResults", people.DefaultSearchResults, 20},
		{"MaxNames", people.MaxNames, 200},
	}
	for _, tc := range ints {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
	if people.SectionTimeout != 2*time.Second {
		t.Errorf("SectionTimeout = %v, want 2s", people.SectionTimeout)
	}
}

func TestPurpose_Validate_Table(t *testing.T) {
	tests := []struct {
		name  string
		p     people.Purpose
		valid bool
		owner string
	}{
		{"module and ident", "volunteers.share-contact", true, "volunteers"},
		{"core owner", "people.newsletter", true, "people"},
		{"digits", "camps2.contact-sharing-2", true, "camps2"},
		{"empty", "", false, ""},
		{"no dot", "volunteers", false, ""},
		{"empty owner", ".share", false, ""},
		{"empty ident", "volunteers.", false, ""},
		{"upper case", "Volunteers.share", false, ""},
		{"space", "volunteers.share contact", false, ""},
		{"double hyphen", "volunteers.share--contact", false, ""},
		{"leading digit ident", "volunteers.1share", false, ""},
		{"underscore", "volunteers.share_contact", false, ""},
		{"64 bytes", people.Purpose("vol." + strings.Repeat("a", 60)), true, "vol"},
		{"65 bytes", people.Purpose("vol." + strings.Repeat("a", 61)), false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.p.Validate()
			if tc.valid && err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if !tc.valid {
				wantKind(t, err, errs.Invalid)
			}
			if got := tc.p.Owner(); got != tc.owner {
				t.Errorf("Owner = %q, want %q", got, tc.owner)
			}
		})
	}
}

func TestFieldMode_ParseAndString(t *testing.T) {
	for _, tc := range []struct {
		s    string
		mode people.FieldMode
	}{{"hidden", people.FieldHidden}, {"optional", people.FieldOptional}, {"required", people.FieldRequired}} {
		got, err := people.ParseFieldMode(tc.s)
		wantNoErr(t, err)
		if got != tc.mode {
			t.Errorf("ParseFieldMode(%q) = %v, want %v", tc.s, got, tc.mode)
		}
		if got.String() != tc.s {
			t.Errorf("String() = %q, want %q", got.String(), tc.s)
		}
		wantNoErr(t, got.Validate())
	}
	for _, bad := range []string{"", "maybe", "REQUIRED ", "0"} {
		if _, err := people.ParseFieldMode(bad); err == nil {
			t.Errorf("ParseFieldMode(%q) succeeded", bad)
		} else {
			wantKind(t, err, errs.Invalid)
		}
	}
	var zero people.FieldMode
	wantKind(t, zero.Validate(), errs.Invalid)
	wantKind(t, people.FieldMode(99).Validate(), errs.Invalid)
	if zero == people.FieldHidden || zero == people.FieldOptional || zero == people.FieldRequired {
		t.Error("the zero FieldMode is a valid mode")
	}
}

func TestDefaultPolicy_AllOptional(t *testing.T) {
	p := people.DefaultPolicy()
	want := people.Policy{LegalName: people.FieldOptional, Phone: people.FieldOptional, EmergencyContacts: people.FieldOptional}
	if p != want {
		t.Fatalf("DefaultPolicy = %+v, want %+v", p, want)
	}
	wantNoErr(t, p.Validate())
}

// F51
func TestPolicy_RequiredContacts_ReturnsInvalid(t *testing.T) {
	p := people.DefaultPolicy()
	p.EmergencyContacts = people.FieldRequired
	err := p.Validate()
	wantField(t, err, "people.emergency_contacts", "one_of")
}

func TestPolicy_Validate_Table(t *testing.T) {
	tests := []struct {
		name  string
		mod   func(*people.Policy)
		field string
	}{
		{"legal name required is allowed", func(p *people.Policy) { p.LegalName = people.FieldRequired }, ""},
		{"phone required is allowed", func(p *people.Policy) { p.Phone = people.FieldRequired }, ""},
		{"contacts hidden is allowed", func(p *people.Policy) { p.EmergencyContacts = people.FieldHidden }, ""},
		{"legal name zero", func(p *people.Policy) { p.LegalName = 0 }, "people.legal_name"},
		{"phone invalid", func(p *people.Policy) { p.Phone = 77 }, "people.phone"},
		{"contacts zero", func(p *people.Policy) { p.EmergencyContacts = 0 }, "people.emergency_contacts"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := people.DefaultPolicy()
			tc.mod(&p)
			err := p.Validate()
			if tc.field == "" {
				wantNoErr(t, err)
				return
			}
			wantKind(t, err, errs.Invalid)
			if !hasField(err, tc.field, "") {
				t.Errorf("no field error for %s in %v", tc.field, err)
			}
		})
	}
}

func TestPerson_Validate(t *testing.T) {
	wantKind(t, people.Person{}.Validate(), errs.Invalid)
	ok := people.Person{ID: newPerson(), PlayaName: "Dusty Ember", CreatedAt: epoch, UpdatedAt: epoch}
	wantNoErr(t, ok.Validate())
	long := ok
	long.PlayaName = strings.Repeat("x", people.MaxPlayaNameBytes+1)
	wantKind(t, long.Validate(), errs.Invalid)
}

func TestPerson_LogValue_IsTheIDOnly(t *testing.T) {
	p := people.Person{
		ID: newPerson(), PlayaName: mPlaya, Pronouns: "they/them", LegalName: mLegal, Phone: mPhone,
		ContactEmail: mail(t, mContactEmail), Email: mail(t, "marker.login@example.org"), HasAccount: true,
	}
	v := p.LogValue().String()
	if !strings.Contains(v, p.ID.String()) {
		t.Errorf("LogValue %q lacks the person ID", v)
	}
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("loaded", slog.Any("person", p))
	for _, m := range []string{mPlaya, "they/them", mLegal, mPhone, mContactEmail, "marker.login"} {
		if strings.Contains(v, m) || strings.Contains(buf.String(), m) {
			t.Errorf("logging a Person leaks %q", m)
		}
	}
}
