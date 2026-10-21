package privacy_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/lib/errs"
)

func TestAudienceValidate_Table(t *testing.T) {
	tests := []struct {
		a  privacy.Audience
		ok bool
	}{
		{privacy.AudienceNobody, true}, {privacy.AudienceSelf, true}, {privacy.AudienceHolders, true},
		{privacy.AudienceSignedIn, true}, {"booked", true},
		{privacy.Audience(strings.Repeat("a", 32)), true},
		{"", false}, {"Booked", false}, {"has space", false}, {"1abc", false}, {"a-b", false},
		{privacy.Audience(strings.Repeat("a", 33)), false},
	}
	for _, tc := range tests {
		t.Run(string(tc.a), func(t *testing.T) {
			err := tc.a.Validate()
			if tc.ok != (err == nil) {
				t.Fatalf("Validate(%q) = %v, ok want %v", tc.a, err, tc.ok)
			}
			if err != nil && !errs.Is(err, errs.Invalid) {
				t.Fatalf("kind = %v, want Invalid", errs.KindOf(err))
			}
		})
	}
	if privacy.AudienceNobody != "nobody" || privacy.AudienceSelf != "self" ||
		privacy.AudienceHolders != "holders" || privacy.AudienceSignedIn != "signed_in" {
		t.Error("built-in audience names changed")
	}
}

func TestGroupValidate_ValidGroups_Pass(t *testing.T) {
	for _, g := range allGroups {
		if err := g.Validate(); err != nil {
			t.Errorf("%s: %v", g.Name, err)
		}
	}
	// A permission is not needed when holders is not offered.
	g := privacy.Group{Name: "x", Default: privacy.AudienceSelf, Options: []privacy.Audience{privacy.AudienceNobody, privacy.AudienceSelf}}
	if err := g.Validate(); err != nil {
		t.Errorf("group without holders needs no permission: %v", err)
	}
}

func TestGroupValidate_Name_Table(t *testing.T) {
	base := gNick
	tests := []struct{ name, code string }{
		{"", "required"}, {"Bad Name", "malformed"}, {"1abc", "malformed"}, {"has-dash", "malformed"},
		{strings.Repeat("a", 65), "malformed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := base
			g.Name = tc.name
			wantField(t, g.Validate(), "name", tc.code)
		})
	}
}

func TestGroupValidate_HoldersWithoutPermission_ReturnsInvalid(t *testing.T) { // F11
	g := gContact
	g.Permission = ""
	wantField(t, g.Validate(), "permission", "required")
	g.Permission = "Not A Permission"
	wantField(t, g.Validate(), "permission", "malformed")
	// A malformed permission is wrong even when holders is not offered.
	n := gNick
	n.Permission = "Not A Permission"
	wantField(t, n.Validate(), "permission", "malformed")
}

func TestGroupValidate_Options_Table(t *testing.T) { // F12
	nobody, self, holders := privacy.AudienceNobody, privacy.AudienceSelf, privacy.AudienceHolders
	tests := []struct {
		name  string
		mod   func(*privacy.Group)
		field string
		code  string
	}{
		{"default not in options", func(g *privacy.Group) {
			g.Default = privacy.AudienceSignedIn
			g.Options = []privacy.Audience{nobody, holders}
		}, "default", "one_of"},
		{"default empty", func(g *privacy.Group) { g.Default = "" }, "default", "one_of"},
		{"one option", func(g *privacy.Group) { g.Options = []privacy.Audience{holders}; g.Default = holders }, "options", "too_few"},
		{"no options", func(g *privacy.Group) { g.Options = nil }, "options", "too_few"},
		{"duplicate option", func(g *privacy.Group) { g.Options = []privacy.Audience{nobody, holders, nobody}; g.Default = holders }, "options", "duplicate"},
		{"unknown audience", func(g *privacy.Group) { g.Options = []privacy.Audience{nobody, "booked", holders} }, "options", "unknown"},
		{"extra not offered is fine but unknown is not", func(g *privacy.Group) {
			g.Options = []privacy.Audience{nobody, holders, "other"}
			g.Extra = []privacy.Audience{"booked"}
		}, "options", "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := privacy.Group{Name: "g", Permission: permContacts, Default: self, Options: []privacy.Audience{nobody, self, holders}}
			tc.mod(&g)
			wantField(t, g.Validate(), tc.field, tc.code)
		})
	}
	t.Run("extra audience in options and default", func(t *testing.T) {
		if err := gRoster.Validate(); err != nil {
			t.Fatal(err)
		}
	})
}

func TestGroupValidate_ExtraCollidesBuiltin_ReturnsInvalid(t *testing.T) { // F13
	for _, a := range []privacy.Audience{privacy.AudienceNobody, privacy.AudienceSelf, privacy.AudienceHolders, privacy.AudienceSignedIn} {
		t.Run(string(a), func(t *testing.T) {
			g := gRoster
			g.Extra = []privacy.Audience{"booked", a}
			wantField(t, g.Validate(), "extra", "duplicate")
		})
	}
	t.Run("extra listed twice", func(t *testing.T) {
		g := gRoster
		g.Extra = []privacy.Audience{"booked", "booked"}
		wantField(t, g.Validate(), "extra", "duplicate")
	})
	t.Run("extra malformed", func(t *testing.T) {
		g := gRoster
		g.Extra = []privacy.Audience{"Not Valid"}
		wantField(t, g.Validate(), "extra", "malformed")
	})
}

func TestSettings_DuplicateGroup_ReturnsInvalid(t *testing.T) { // F14
	dup := gNick
	_, err := privacy.Settings([]privacy.Group{gNick, gContact, dup})
	wantField(t, err, "groups[2].name", "duplicate")
}

func TestSettings_InvalidGroup_ReturnsInvalid(t *testing.T) {
	bad := gContact
	bad.Permission = ""
	_, err := privacy.Settings([]privacy.Group{gNick, bad})
	wantField(t, err, "groups[1].permission", "required")
}

func TestSettings_ProducesEnumDecls(t *testing.T) { // F15
	decls, err := privacy.Settings(allGroups)
	if err != nil {
		t.Fatalf("Settings: %v", err)
	}
	if len(decls) != len(allGroups) {
		t.Fatalf("len = %d, want %d", len(decls), len(allGroups))
	}
	for i, g := range allGroups {
		d := decls[i]
		if d.Key != "visibility_"+g.Name {
			t.Errorf("Key = %q", d.Key)
		}
		if d.Kind != settings.Enum {
			t.Errorf("%s: Kind = %v, want Enum", d.Key, d.Kind)
		}
		if d.Default != string(g.Default) {
			t.Errorf("%s: Default = %q", d.Key, d.Default)
		}
		var want []string
		for _, o := range g.Options {
			want = append(want, string(o))
		}
		if !slices.Equal(d.Options, want) {
			t.Errorf("%s: Options = %v, want %v (display order kept)", d.Key, d.Options, want)
		}
		if err := d.Validate(); err != nil {
			t.Errorf("%s: Decl.Validate: %v", d.Key, err)
		}
	}
	// Options is a copy: mutating the decl must not change the group.
	decls[0].Options[0] = "zzz"
	if string(allGroups[0].Options[0]) == "zzz" {
		t.Error("Settings aliased the group's Options")
	}
}

func TestSettings_NoGroups_ReturnsNone(t *testing.T) {
	for _, in := range [][]privacy.Group{nil, {}} {
		decls, err := privacy.Settings(in)
		if err != nil || len(decls) != 0 {
			t.Errorf("Settings(%v) = %v, %v", in, decls, err)
		}
	}
}
