package settings_test

import (
	"context"
	"fmt"

	"github.com/zedgama3/bms/internal/core/settings"
	"github.com/zedgama3/bms/internal/core/settings/settingstest"
)

// A module declares its settings in its manifest and reads them through the scoped Reader
// it gets as Deps.Settings. A test hands the module a Fake over the same declarations.
func ExampleReader() {
	decls := []settings.Decl{
		{Key: "greeting", Kind: settings.Text, Default: "Hello, burner"},
		{Key: "shift_limit", Kind: settings.Int, Default: "4"},
	}
	fake := settingstest.NewFake("hello", decls)
	ctx := context.Background()

	greeting, err := fake.Text(ctx, "greeting")
	fmt.Println(greeting, err)

	fake.Set("shift_limit", "6")
	limit, err := fake.Int(ctx, "shift_limit")
	fmt.Println(limit, err)

	_, err = fake.Bool(ctx, "greeting") // a Text read as a Bool
	fmt.Println(err != nil)
	// Output:
	// Hello, burner <nil>
	// 6 <nil>
	// true
}

// A module declares a word a regional may rename; Validate refuses a malformed key, a word
// the core already owns, and defaults without English.
func ExampleTermDecl_Validate() {
	shift := settings.TermDecl{Key: "shift", Defaults: map[string]settings.Forms{
		"en": {Singular: "shift", Plural: "shifts", SingularTitle: "Shift", PluralTitle: "Shifts"},
	}}
	fmt.Println(shift.Validate())

	lead := shift
	lead.Key = "lead" // a core term: declared once, in CoreTerms
	fmt.Println(lead.Validate() != nil)

	noEnglish := settings.TermDecl{Key: "gate", Defaults: map[string]settings.Forms{"es": shift.Defaults["en"]}}
	fmt.Println(noEnglish.Validate() != nil)
	// Output:
	// <nil>
	// true
	// true
}
