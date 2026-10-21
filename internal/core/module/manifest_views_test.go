package module_test

import (
	"testing"
	"time"

	"github.com/zedgama3/bms/internal/core/authz"
	"github.com/zedgama3/bms/internal/core/module"
	"github.com/zedgama3/bms/internal/core/privacy"
	"github.com/zedgama3/bms/internal/model"
)

// Response types for the Views cross-checks (invented, no real data).
type viewOK struct {
	ID    string  `json:"id" privacy:"public"`
	Phone *string `json:"phone,omitempty" privacy:"personal,group=contact"`
}

type viewUntagged struct {
	ID   string `json:"id" privacy:"public"`
	Name string `json:"name"`
}

type viewBadLevel struct {
	ID string `json:"id" privacy:"secret"`
}

type viewBadGroup struct {
	Phone *string `json:"phone,omitempty" privacy:"personal,group=nosuch"`
}

type viewUngrouped struct {
	Phone string `json:"phone" privacy:"personal"`
}

type viewPointerNoOmit struct {
	Phone *string `json:"phone" privacy:"personal,group=contact"`
}

type viewAuditName struct {
	Phone *string `json:"phoneNumber,omitempty" privacy:"personal,group=contact"`
}

type viewSensitive struct {
	Dx *string `json:"dx,omitempty" privacy:"sensitive,group=contact"`
}

// viewNested holds the group's sensitive field one type down, in a slice.
type viewNested struct {
	ID   string          `json:"id" privacy:"public"`
	Rows []viewSensitive `json:"rows,omitempty" privacy:"internal"`
}

type viewNestedPtr struct {
	Row *viewNested `json:"row,omitempty" privacy:"internal"`
}

func contactGroup(permission string, options ...privacy.Audience) privacy.Group {
	if options == nil {
		options = []privacy.Audience{privacy.AudienceNobody, privacy.AudienceSelf, privacy.AudienceHolders}
	}
	return privacy.Group{Name: "contact", Permission: authz.Permission(permission), Default: privacy.AudienceHolders, Options: options}
}

func withViews(views []any, groups []privacy.Group, mutate ...func(*module.Manifest)) module.Manifest {
	m := mf("alpha", func(m *module.Manifest) { m.Views, m.Groups = views, groups })
	for _, f := range mutate {
		f(&m)
	}
	return m
}

func appendVisibility(t *testing.T, groups []privacy.Group) func(*module.Manifest) {
	t.Helper()
	decls, err := privacy.Settings(groups)
	if err != nil {
		t.Fatalf("privacy.Settings: %v", err)
	}
	return func(m *module.Manifest) { m.Settings = append(m.Settings, decls...) }
}

func TestManifestValidate_ViewsWellFormed_Passes(t *testing.T) {
	g := []privacy.Group{contactGroup("people.contacts.read")}
	m := withViews([]any{viewOK{}, &viewOK{}}, g, appendVisibility(t, g))
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := withViews(nil, nil).Validate(); err != nil {
		t.Fatalf("nil Views and Groups: %v", err)
	}
}

func TestManifestValidate_UntaggedViewField_ReturnsInvalid(t *testing.T) { // F62
	g := []privacy.Group{contactGroup("people.contacts.read")}
	tests := []struct {
		name  string
		views []any
		field string
	}{
		{"untagged field", []any{viewOK{}, viewUntagged{}}, "views[1].Name"},
		{"bad level", []any{viewBadLevel{}}, "views[0].ID"},
		{"unknown group", []any{viewBadGroup{}}, "views[0].Phone"},
		{"personal without group", []any{viewUngrouped{}}, "views[0].Phone"},
		{"grouped pointer without omitempty (A1)", []any{viewPointerNoOmit{}}, "views[0].Phone"},
		{"auditable name outside audit grammar (A1)", []any{viewAuditName{}}, "views[0].Phone"},
		{"not a struct", []any{42}, "views[0]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := withViews(tc.views, g, appendVisibility(t, g))
			wantField(t, m.Validate(), tc.field)
		})
	}
}

func TestManifestValidate_ViewNotOmittable_ReturnsInvalid(t *testing.T) { // F62 (A2)
	g := []privacy.Group{contactGroup("people.contacts.read")}
	type internalPlain struct {
		N string `json:"n" privacy:"internal"`
	}
	type timeOmitempty struct {
		At time.Time `json:"at,omitempty" privacy:"personal,group=contact"`
	}
	for name, tc := range map[string]struct {
		view  any
		field string
	}{
		"ungrouped internal without the option": {internalPlain{}, "views[0].N"},
		"grouped time.Time with omitempty":      {timeOmitempty{}, "views[0].At"},
	} {
		t.Run(name, func(t *testing.T) {
			m := withViews([]any{tc.view}, g, appendVisibility(t, g))
			wantField(t, m.Validate(), tc.field)
		})
	}
}

func TestManifestValidate_InvalidGroup_ReturnsInvalid(t *testing.T) { // F62 (groups[j])
	bad := privacy.Group{Name: "contact", Default: privacy.AudienceHolders,
		Options: []privacy.Audience{privacy.AudienceNobody, privacy.AudienceHolders}} // holders without permission
	m := withViews([]any{viewOK{}}, []privacy.Group{bad})
	wantFieldPrefix(t, m.Validate(), "groups[0]")
}

func TestManifestValidate_VisibilitySettingMissing_ReturnsInvalid(t *testing.T) { // F63
	g := []privacy.Group{contactGroup("people.contacts.read")}
	t.Run("missing", func(t *testing.T) {
		wantFieldPrefix(t, withViews([]any{viewOK{}}, g).Validate(), "settings[")
	})
	t.Run("different default", func(t *testing.T) {
		m := withViews([]any{viewOK{}}, g, func(m *module.Manifest) {
			m.Settings = append(m.Settings, module.SettingDecl{
				Key: "visibility_contact", Kind: module.SettingEnum,
				Options: []string{"nobody", "self", "holders"}, Default: "self",
			})
		})
		wantFieldPrefix(t, m.Validate(), "settings[")
	})
	t.Run("different options", func(t *testing.T) {
		m := withViews([]any{viewOK{}}, g, func(m *module.Manifest) {
			m.Settings = append(m.Settings, module.SettingDecl{
				Key: "visibility_contact", Kind: module.SettingEnum,
				Options: []string{"nobody", "holders"}, Default: "holders",
			})
		})
		wantFieldPrefix(t, m.Validate(), "settings[")
	})
	t.Run("present with equal options and default", func(t *testing.T) {
		m := withViews([]any{viewOK{}}, g, appendVisibility(t, g))
		if err := m.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})
}

func TestManifestValidate_HoldersPermissionReachesTooLow_ReturnsInvalid(t *testing.T) { // F64
	decl := func(reaches model.Sensitivity) func(*module.Manifest) {
		return func(m *module.Manifest) {
			m.Permissions = []authz.PermissionDecl{{Name: "alpha.contacts.read", Scope: authz.ScopeSite, Reaches: reaches}}
		}
	}
	g := []privacy.Group{contactGroup("alpha.contacts.read")}
	tests := []struct {
		name    string
		views   []any
		groups  []privacy.Group
		reaches model.Sensitivity
		wantErr bool
	}{
		{"personal field, internal reach", []any{viewOK{}}, g, model.SensitivityInternal, true},
		{"personal field, personal reach", []any{viewOK{}}, g, model.SensitivityPersonal, false},
		{"personal field, sensitive reach", []any{viewOK{}}, g, model.SensitivitySensitive, false},
		{"sensitive field, personal reach", []any{viewSensitive{}}, g, model.SensitivityPersonal, true},
		{"sensitive field, sensitive reach", []any{viewSensitive{}}, g, model.SensitivitySensitive, false},
		{"highest level among the group's fields counts", []any{viewOK{}, viewSensitive{}}, g, model.SensitivityPersonal, true},
		{"sensitive field in a nested type, personal reach (A2)", []any{viewNested{}}, g, model.SensitivityPersonal, true},
		{"sensitive field in a nested type, sensitive reach (A2)", []any{viewNested{}}, g, model.SensitivitySensitive, false},
		{"sensitive field two types down, personal reach (A2)", []any{viewNestedPtr{}}, g, model.SensitivityPersonal, true},
	}
	// The levels the check uses come from privacy.GroupLevels, which walks nested types.
	if got := privacy.GroupLevels([]any{viewNested{}})["contact"]; got != model.SensitivitySensitive {
		t.Fatalf("GroupLevels(nested) = %v, want sensitive", got)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := withViews(tc.views, tc.groups, appendVisibility(t, tc.groups), decl(tc.reaches))
			err := m.Validate()
			if tc.wantErr {
				wantField(t, err, "permissions[0].reaches")
				return
			}
			if err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
	t.Run("permission declared by another area is not cross-checked", func(t *testing.T) {
		og := []privacy.Group{contactGroup("people.contacts.read")}
		m := withViews([]any{viewOK{}}, og, appendVisibility(t, og), decl(model.SensitivityInternal))
		if err := m.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})
	t.Run("group not offering holders is not cross-checked", func(t *testing.T) {
		ng := []privacy.Group{{
			Name: "contact", Permission: "alpha.contacts.read", Default: privacy.AudienceSelf,
			Options: []privacy.Audience{privacy.AudienceNobody, privacy.AudienceSelf},
		}}
		m := withViews([]any{viewOK{}}, ng, appendVisibility(t, ng), decl(model.SensitivityInternal))
		if err := m.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
	})
}
