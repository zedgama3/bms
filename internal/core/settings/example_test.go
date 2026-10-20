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
