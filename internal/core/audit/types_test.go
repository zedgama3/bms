package audit_test

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/audit"
	"github.com/zedgama3/bms/internal/lib/errs"
	"github.com/zedgama3/bms/internal/model"
)

func TestConstants_MatchTheSpec(t *testing.T) {
	if audit.MaxBatch != 1000 || audit.MaxFields != 64 || audit.MaxFieldBytes != 64 ||
		audit.MaxReasonBytes != 256 || audit.MaxTitles != 16 || audit.MaxTitleBytes != 64 ||
		audit.MaxActionBytes != 96 || audit.MaxPageSize != 200 || audit.DefaultPageSize != 50 ||
		audit.SpoolVersion != 1 {
		t.Fatal("a limit constant differs from the spec")
	}
	if audit.SpoolDirName != "audit/spool" {
		t.Errorf("SpoolDirName = %q", audit.SpoolDirName)
	}
	if audit.DefaultImportInterval.Seconds() != 30 {
		t.Errorf("DefaultImportInterval = %v", audit.DefaultImportInterval)
	}
}

func TestAction_Validate(t *testing.T) {
	long96 := "identity." + strings.Repeat("a", 96-len("identity."))
	tests := []struct {
		name   string
		action string
		valid  bool
	}{
		{"two idents", "identity.login", true},
		{"three idents", "identity.account.disabled", true},
		{"hyphen", "identity.login-failed", true},
		{"digits", "identity.v2.read", true},
		{"exactly 96 bytes", long96, true},
		{"97 bytes", long96 + "a", false},
		{"empty", "", false},
		{"no dot", "identity", false},
		{"trailing dot", "identity.", false},
		{"double dot", "identity..login", false},
		{"upper case", "identity.Login", false},
		{"upper case area", "Identity.login", false},
		{"ident starts with digit", "identity.1login", false},
		{"ident starts with hyphen", "identity.-login", false},
		{"ident ends with hyphen", "identity.login-", false},
		{"double hyphen", "identity.log--in", false},
		{"underscore", "identity.log_in", false},
		{"space", "identity.log in", false},
		{"non ascii", "identity.lögin", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := audit.Action(tc.action).Validate()
			if tc.valid {
				wantNoErr(t, err)
				return
			}
			wantKind(t, err, errs.Invalid)
			wantField(t, err, "action")
		})
	}
}

func TestAction_Validate_NeverEchoesTheValue(t *testing.T) {
	err := audit.Action("Bad-" + marker + ".x").Validate()
	wantKind(t, err, errs.Invalid)
	noMarker(t, err)
}

func TestAction_Area(t *testing.T) {
	tests := map[string]string{
		"identity.login":            "identity",
		"identity.account.disabled": "identity",
		"identity":                  "",
		"":                          "",
		".x":                        "",
	}
	for in, want := range tests {
		if got := audit.Action(in).Area(); got != want {
			t.Errorf("Area(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestOutcome_StringParseValidate(t *testing.T) {
	names := map[audit.Outcome]string{audit.Succeeded: "succeeded", audit.Denied: "denied", audit.Failed: "failed"}
	for o, name := range names {
		if o.String() != name {
			t.Errorf("String(%d) = %q, want %q", o, o.String(), name)
		}
		wantNoErr(t, o.Validate())
		got, err := audit.ParseOutcome(name)
		wantNoErr(t, err)
		if got != o {
			t.Errorf("ParseOutcome(%q) = %v", name, got)
		}
	}
	if audit.Succeeded != 1 {
		t.Errorf("Succeeded = %d, want 1", audit.Succeeded)
	}
	if got := audit.Outcome(9).String(); got != "Outcome(9)" {
		t.Errorf("String(9) = %q", got)
	}
	for _, o := range []audit.Outcome{0, 4, 255} {
		wantKind(t, o.Validate(), errs.Invalid)
	}
	for _, s := range []string{"", "Succeeded", "SUCCEEDED", " denied", "ok", "Outcome(1)"} {
		_, err := audit.ParseOutcome(s)
		wantKind(t, err, errs.Invalid)
	}
}

func TestActor_Validate(t *testing.T) {
	longTitle := strings.Repeat("t", audit.MaxTitleBytes+1)
	many := make([]string, audit.MaxTitles+1)
	for i := range many {
		many[i] = "lead"
	}
	tests := []struct {
		name  string
		actor audit.Actor
		valid bool
	}{
		{"zero resolves", audit.Actor{}, true},
		{"person", person(alice), true},
		{"superuser person", superuser(alice), true},
		{"anonymous", audit.Actor{Kind: audit.ActorAnonymous}, true},
		{"operator", audit.Actor{Kind: audit.ActorOperator}, true},
		{"system", audit.Actor{Kind: audit.ActorSystem}, true},
		{"person with titles", audit.Actor{Kind: audit.ActorPerson, Person: alice, Titles: []string{"Gate lead"}}, true},
		{"unknown kind", audit.Actor{Kind: 9}, false},
		{"person kind without person", audit.Actor{Kind: audit.ActorPerson}, false},
		{"person set with zero kind", audit.Actor{Person: alice}, false},
		{"person set with system kind", audit.Actor{Kind: audit.ActorSystem, Person: alice}, false},
		{"person set with anonymous kind", audit.Actor{Kind: audit.ActorAnonymous, Person: alice}, false},
		{"superuser with operator", audit.Actor{Kind: audit.ActorOperator, Superuser: true}, false},
		{"superuser with zero kind", audit.Actor{Superuser: true}, false},
		{"too many titles", audit.Actor{Kind: audit.ActorPerson, Person: alice, Titles: many}, false},
		{"empty title", audit.Actor{Kind: audit.ActorPerson, Person: alice, Titles: []string{""}}, false},
		{"title over the limit", audit.Actor{Kind: audit.ActorPerson, Person: alice, Titles: []string{longTitle}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.actor.Validate()
			if tc.valid {
				wantNoErr(t, err)
				return
			}
			wantKind(t, err, errs.Invalid)
		})
	}
	t.Run("MaxTitles titles of MaxTitleBytes are valid", func(t *testing.T) {
		titles := make([]string, audit.MaxTitles)
		for i := range titles {
			titles[i] = strings.Repeat("t", audit.MaxTitleBytes)
		}
		wantNoErr(t, audit.Actor{Kind: audit.ActorPerson, Person: alice, Titles: titles}.Validate())
	})
}

func TestRef_RefOfIsZeroValidate(t *testing.T) {
	acc := model.AccountID{9, 9}
	r := audit.RefOf("identity.account", acc)
	if r.Kind != "identity.account" || r.ID != [16]byte(acc) {
		t.Fatalf("RefOf = %+v", r)
	}
	if r.IsZero() {
		t.Error("a set Ref is not zero")
	}
	wantNoErr(t, r.Validate())
	if !(audit.Ref{}).IsZero() {
		t.Error("the zero Ref must report IsZero")
	}
	wantNoErr(t, audit.Ref{}.Validate())

	bad := map[string]audit.Ref{
		"kind without id":     {Kind: "identity.account"},
		"id without kind":     {ID: [16]byte{1}},
		"kind without area":   {Kind: "account", ID: [16]byte{1}},
		"upper case kind":     {Kind: "Identity.account", ID: [16]byte{1}},
		"trailing dot":        {Kind: "identity.", ID: [16]byte{1}},
		"space in kind":       {Kind: "identity.my account", ID: [16]byte{1}},
		"underscore in ident": {Kind: "identity.my_account", ID: [16]byte{1}},
	}
	for name, ref := range bad {
		t.Run(name, func(t *testing.T) {
			wantKind(t, ref.Validate(), errs.Invalid)
		})
	}
}

func TestEntry_Validate_ReportsEachFieldProblem(t *testing.T) {
	e := audit.Entry{
		Action:  "Bad",
		Outcome: 0,
		Actor:   audit.Actor{Kind: audit.ActorPerson},
		Record:  audit.Ref{Kind: "identity.account"},
		Fields:  []string{"Bad Field"},
		Reason:  "a\x00b",
	}
	err := e.Validate()
	wantKind(t, err, errs.Invalid)
	for _, f := range []string{"action", "outcome", "actor", "record", "fields", "level", "reason"} {
		wantField(t, err, f)
	}
}

func TestEntry_Validate_Valid(t *testing.T) {
	wantNoErr(t, loginEntry().Validate())
	wantNoErr(t, readEntry(alice).Validate())
	e := loginEntry()
	e.Reason = "lost radio, needs the key"
	wantNoErr(t, e.Validate())
}

func TestEntry_Validate_Boundaries(t *testing.T) {
	maxFields := make([]string, audit.MaxFields)
	for i := range maxFields {
		maxFields[i] = "f" + strings.Repeat("a", i%5) + string(rune('a'+i/26)) + string(rune('a'+i%26))
	}
	withFields := func(fs []string) audit.Entry {
		e := loginEntry()
		e.Fields, e.Level = fs, model.SensitivityPersonal
		return e
	}
	withReason := func(r string) audit.Entry { e := loginEntry(); e.Reason = r; return e }
	tests := []struct {
		name  string
		entry audit.Entry
		valid bool
	}{
		{"MaxFields fields", withFields(maxFields), true},
		{"MaxFields+1 fields", withFields(append(append([]string(nil), maxFields...), "zzzz_extra")), false},
		{"field of MaxFieldBytes", withFields([]string{strings.Repeat("a", audit.MaxFieldBytes)}), true},
		{"field over MaxFieldBytes", withFields([]string{strings.Repeat("a", audit.MaxFieldBytes+1)}), false},
		{"field with digits and underscore", withFields([]string{"a1_b2"}), true},
		{"field starts with underscore", withFields([]string{"_a"}), false},
		{"field starts with digit", withFields([]string{"1a"}), false},
		{"field upper case", withFields([]string{"legalName"}), false},
		{"field with hyphen", withFields([]string{"legal-name"}), false},
		{"empty field name", withFields([]string{""}), false},
		{"duplicate field", withFields([]string{"email", "email"}), false},
		{"fields without level", func() audit.Entry { e := loginEntry(); e.Fields = []string{"email"}; return e }(), false},
		{"level without fields", func() audit.Entry { e := loginEntry(); e.Level = model.SensitivityInternal; return e }(), false},
		{"unknown level", func() audit.Entry { e := withFields([]string{"email"}); e.Level = 9; return e }(), false},
		{"reason of MaxReasonBytes", withReason(strings.Repeat("a", audit.MaxReasonBytes)), true},
		{"reason one byte over", withReason(strings.Repeat("a", audit.MaxReasonBytes+1)), false},
		{"reason multibyte at the limit", withReason(strings.Repeat("é", audit.MaxReasonBytes/2)), true},
		{"reason multibyte over by bytes", withReason(strings.Repeat("é", audit.MaxReasonBytes/2+1)), false},
		{"reason invalid utf-8", withReason("a\xffb"), false},
		{"reason newline", withReason("a\nb"), false},
		{"reason tab", withReason("a\tb"), false},
		{"reason NUL", withReason("a\x00b"), false},
		{"reason DEL", withReason("a\x7fb"), false},
		{"reason with unicode text", withReason("Zażółć radio — найти ключ"), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.entry.Validate()
			if tc.valid {
				wantNoErr(t, err)
				return
			}
			wantKind(t, err, errs.Invalid)
		})
	}
}

func TestEntry_Validate_NeverEchoesTheReason(t *testing.T) {
	e := loginEntry()
	e.Reason = marker + "\x00"
	err := e.Validate()
	wantKind(t, err, errs.Invalid)
	noMarker(t, err)
}
