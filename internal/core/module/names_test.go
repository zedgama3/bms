package module_test

import (
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/signals"
	"github.com/zedgama3/bms/internal/lib/errs"
)

type validator interface{ Validate() error }

func checkTable(t *testing.T, mk func(string) validator, accept, reject []string) {
	t.Helper()
	for _, in := range accept {
		if err := mk(in).Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", in, err)
		}
	}
	for _, in := range reject {
		if err := mk(in).Validate(); !errs.Is(err, errs.Invalid) {
			t.Errorf("Validate(%q) = %v, want Invalid", in, err)
		}
	}
}

var (
	ident64  = strings.Repeat("a", 64)
	ident65  = strings.Repeat("a", 65)
	badIdent = []string{"", "Shift", "a_b", "-a", "a-", "a--b", "1a", "a b", "a.b", "é", ident65}
)

func TestIdentGrammar_Table(t *testing.T) {
	// A Capability is exactly one ident, so it exercises the grammar directly.
	accept := []string{"a1", "a", "shift-completed", "a-1", "a1-b2-c3", ident64}
	checkTable(t, func(s string) validator { return module.Capability(s) }, accept, badIdent)
}

func TestCapabilityValidate_Table(t *testing.T) {
	checkTable(t, func(s string) validator { return module.Capability(s) },
		[]string{"volunteer-credit", "greeter"},
		[]string{"volunteer.credit", "Greeter", ""})
}

func TestExtensionPointValidate_Table(t *testing.T) {
	checkTable(t, func(s string) validator { return module.ExtensionPoint(s) },
		[]string{"people.person-page", "a.b", "hello.banner", ident64 + "." + ident64},
		[]string{"", "people", "people.", ".page", "people.person.page", "People.page", "people.Page",
			"people.a_b", ident65 + ".x", "x." + ident65})
}

func TestExtensionPointDefiner_ReturnsPrefix(t *testing.T) {
	if got := module.ExtensionPoint("people.person-page").Definer(); got != "people" {
		t.Fatalf("Definer() = %q, want people", got)
	}
}

func TestExtensionPointDeclValidate_Table(t *testing.T) {
	tests := []struct {
		name string
		d    module.ExtensionPointDecl
		ok   bool
	}{
		{"valid", module.ExtensionPointDecl{Name: "alpha.page", Accepts: acceptsString}, true},
		{"nil accepts", module.ExtensionPointDecl{Name: "alpha.page"}, false},
		{"bad name", module.ExtensionPointDecl{Name: "page", Accepts: acceptsString}, false},
		{"both bad", module.ExtensionPointDecl{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.d.Validate()
			if tc.ok && err != nil {
				t.Fatalf("Validate() = %v", err)
			}
			if !tc.ok {
				wantKind(t, err, errs.Invalid)
			}
		})
	}
}

func TestSignalName_Validate(t *testing.T) {
	checkTable(t, func(s string) validator { return signals.Name(s) },
		[]string{"volunteers.shift-completed", "people.person-created", "a.b"},
		[]string{"", "volunteers", "a.b.c", ".b", "a.", "A.b", "a.B", "a_b.c"})
	if got := signals.Name("hello.greeted").Publisher(); got != "hello" {
		t.Fatalf("Publisher() = %q", got)
	}
}

func TestSettingKindString_Table(t *testing.T) {
	tests := map[module.SettingKind]string{
		module.SettingBool:     "bool",
		module.SettingInt:      "int",
		module.SettingDuration: "duration",
		module.SettingText:     "text",
		module.SettingEnum:     "enum",
		module.SettingKind(0):  "SettingKind(0)",
		module.SettingKind(42): "SettingKind(42)",
	}
	for k, want := range tests {
		if got := k.String(); got != want {
			t.Errorf("SettingKind(%d).String() = %q, want %q", uint8(k), got, want)
		}
	}
}

func TestEdgeKindString_Table(t *testing.T) {
	tests := map[module.EdgeKind]string{
		module.EdgeRequires:    "requires",
		module.EdgeUses:        "uses",
		module.EdgeContributes: "contributes",
		module.EdgeSubscribes:  "subscribes",
		module.EdgeKind(0):     "EdgeKind(0)",
		module.EdgeKind(9):     "EdgeKind(9)",
	}
	for k, want := range tests {
		if got := k.String(); got != want {
			t.Errorf("EdgeKind(%d).String() = %q, want %q", uint8(k), got, want)
		}
	}
}

func TestSettingDeclValidate_Table(t *testing.T) {
	notFive := func(v string) error {
		if v == "5" {
			return errs.E("test", errs.Invalid, errBoom)
		}
		return nil
	}
	tests := []struct {
		name string
		d    module.SettingDecl
		ok   bool
		// field is a suffix a reported field name must end with (case-insensitive) when !ok.
		field string
	}{
		{"bool true", module.SettingDecl{Key: "flag", Kind: module.SettingBool, Default: "true"}, true, ""},
		{"bool false", module.SettingDecl{Key: "flag", Kind: module.SettingBool, Default: "false"}, true, ""},
		{"bool bad default", module.SettingDecl{Key: "flag", Kind: module.SettingBool, Default: "maybe"}, false, "default"},
		{"int ok", module.SettingDecl{Key: "n", Kind: module.SettingInt, Default: "-12"}, true, ""},
		{"int bad", module.SettingDecl{Key: "n", Kind: module.SettingInt, Default: "1.5"}, false, "default"},
		{"int empty", module.SettingDecl{Key: "n", Kind: module.SettingInt, Default: ""}, false, "default"},
		{"duration ok", module.SettingDecl{Key: "ttl", Kind: module.SettingDuration, Default: "72h"}, true, ""},
		{"duration bad", module.SettingDecl{Key: "ttl", Kind: module.SettingDuration, Default: "3 days"}, false, "default"},
		{"text ok", module.SettingDecl{Key: "greeting", Kind: module.SettingText, Default: "hello"}, true, ""},
		{"text empty ok", module.SettingDecl{Key: "greeting", Kind: module.SettingText, Default: ""}, true, ""},
		{"text 4096 ok", module.SettingDecl{Key: "greeting", Kind: module.SettingText, Default: strings.Repeat("x", 4096)}, true, ""},
		{"text 4097 bad", module.SettingDecl{Key: "greeting", Kind: module.SettingText, Default: strings.Repeat("x", 4097)}, false, "default"},
		{"enum ok", module.SettingDecl{Key: "mode", Kind: module.SettingEnum, Options: []string{"a", "b-c"}, Default: "a"}, true, ""},
		{"enum default not option", module.SettingDecl{Key: "mode", Kind: module.SettingEnum, Options: []string{"a", "b"}, Default: "c"}, false, "default"},
		{"enum one option", module.SettingDecl{Key: "mode", Kind: module.SettingEnum, Options: []string{"a"}, Default: "a"}, false, "options"},
		{"enum no options", module.SettingDecl{Key: "mode", Kind: module.SettingEnum, Default: "a"}, false, "options"},
		{"enum duplicate options", module.SettingDecl{Key: "mode", Kind: module.SettingEnum, Options: []string{"a", "a"}, Default: "a"}, false, "options"},
		{"enum non-ident option", module.SettingDecl{Key: "mode", Kind: module.SettingEnum, Options: []string{"a", "B c"}, Default: "a"}, false, "options"},
		{"options on bool", module.SettingDecl{Key: "flag", Kind: module.SettingBool, Options: []string{"a", "b"}, Default: "true"}, false, "options"},
		{"options on text", module.SettingDecl{Key: "t", Kind: module.SettingText, Options: []string{"a", "b"}}, false, "options"},
		{"key not ident", module.SettingDecl{Key: "Bad_Key", Kind: module.SettingBool, Default: "true"}, false, "key"},
		{"key empty", module.SettingDecl{Kind: module.SettingBool, Default: "true"}, false, "key"},
		{"kind zero", module.SettingDecl{Key: "k", Default: "x"}, false, "kind"},
		{"kind out of range", module.SettingDecl{Key: "k", Kind: 99, Default: "x"}, false, "kind"},
		{"validate accepts", module.SettingDecl{Key: "n", Kind: module.SettingInt, Default: "7", Check: notFive}, true, ""},
		{"validate rejects default", module.SettingDecl{Key: "n", Kind: module.SettingInt, Default: "5", Check: notFive}, false, "default"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.d.Validate()
			if tc.ok {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			wantKind(t, err, errs.Invalid)
			for f := range fieldSet(err) {
				if strings.HasSuffix(strings.ToLower(f), tc.field) {
					return
				}
			}
			t.Fatalf("no field ending in %q; fields: %v", tc.field, fieldSet(err))
		})
	}
}

func TestSettingDeclValidate_ReportsEveryProblem(t *testing.T) {
	d := module.SettingDecl{Key: "Bad", Kind: module.SettingBool, Options: []string{"a"}, Default: "nope"}
	err := d.Validate()
	wantKind(t, err, errs.Invalid)
	if got := len(fieldSet(err)); got < 3 {
		t.Fatalf("reported %d distinct fields, want at least 3 (key, options, default)", got)
	}
}
